//go:build linux

package control

import (
	"context"
	"encoding/json"
	"fmt"
	"io/fs"
	"math"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/azpanel/lnmp-gopanel/internal/backups"
)

func backupSteps(p storedBackupPolicy, now time.Time) []backups.RunStep {
	steps := []backups.RunStep{{ID: "queued", Kind: "queued", Status: "running", StartedAt: &now}, {ID: "preflight", Kind: "preflight", Status: "pending"}}
	for _, category := range []string{"databases", "files"} {
		if category == "databases" && !p.Databases.Enabled || category == "files" && len(p.Files) == 0 {
			continue
		}
		for _, kind := range []string{"prepare", "compress", "verify"} {
			steps = append(steps, backups.RunStep{ID: category + "-" + kind, Kind: kind, Category: category, Status: "pending"})
		}
		for i, d := range p.Destinations {
			for _, kind := range []string{"deliver", "cleanup"} {
				destination := d
				steps = append(steps, backups.RunStep{ID: fmt.Sprintf("%s-%s-%d", category, kind, i), Kind: kind, Category: category, Destination: &destination, Status: "pending"})
			}
		}
	}
	return append(steps, backups.RunStep{ID: "finished", Kind: "finished", Status: "pending"})
}

func backupStep(j *storedBackupRun, id string) *backups.RunStep {
	for i := range j.Steps {
		if j.Steps[i].ID == id {
			return &j.Steps[i]
		}
	}
	return nil
}

func finishBackupSteps(j *storedBackupRun, now time.Time) {
	for i := range j.Steps {
		s := &j.Steps[i]
		switch {
		case s.Kind == "finished":
			s.Status = j.Status
			s.FinishedAt = &now
			s.Error = j.Error
		case s.Status == "running":
			s.Status = "failed"
			if j.Status == "cancelled" {
				s.Status = "cancelled"
			}
			s.Error = j.Error
			s.FinishedAt = &now
		case s.Status == "pending":
			s.Status = "skipped"
			if j.Status == "cancelled" {
				s.Status = "cancelled"
			}
			s.FinishedAt = &now
		}
		if s.Progress != nil && s.Status != "running" {
			s.Progress.ETASeconds = nil
		}
	}
}

// Progress does not change list order or rewrite the history index. The store
// lock and current on-disk status prevent late samples resurrecting a run.
func (e backupExecutor) updateProgress(j *storedBackupRun) error {
	release, err := e.store.lock()
	if err != nil {
		return err
	}
	defer release()
	current, err := e.loadRun(j.ID)
	if err != nil {
		return err
	}
	if !backups.ActiveRun(current.Status) {
		return errBackupRunFinished
	}
	return e.saveRun(j)
}

type backupProgressKey struct{}
type backupProgressSample struct {
	total, processed int64
	percent          float64
	estimated        bool
}
type backupProgressReporter func(backupProgressSample)

func reportBackupProgress(ctx context.Context, s backupProgressSample) {
	if report, ok := ctx.Value(backupProgressKey{}).(backupProgressReporter); ok {
		report(s)
	}
}

// Only the command's output reader mutates progress while the operation runs.
// The mutex also permits writers on different streams; Wait joins them before
// finalizing the step. Child output is never persisted as a diagnostic.
func (e backupExecutor) runStep(ctx context.Context, j *storedBackupRun, id string, operation func(context.Context) error) error {
	s := backupStep(j, id)
	if s == nil {
		return operation(ctx)
	} // Compatibility with pre-upgrade reservations.
	now := e.now().UTC()
	s.Status = "running"
	s.StartedAt = &now
	if err := e.update(j); err != nil {
		return err
	}
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	var mu sync.Mutex
	var writeErr error
	var lastSave, lastAdvance time.Time
	var samples []struct {
		at    time.Time
		bytes int64
	}
	report := backupProgressReporter(func(sample backupProgressSample) {
		mu.Lock()
		defer mu.Unlock()
		if writeErr != nil {
			return
		}
		now := e.now().UTC()
		p := &backups.RunProgress{UpdatedAt: now, Estimated: sample.estimated}
		if sample.total >= 0 {
			total := sample.total
			p.TotalBytes = &total
		}
		if sample.processed >= 0 {
			processed := sample.processed
			if sample.total >= 0 && processed > sample.total {
				processed = sample.total
			}
			if s.Progress != nil && s.Progress.ProcessedBytes != nil && processed < *s.Progress.ProcessedBytes {
				return
			}
			p.ProcessedBytes = &processed
			if len(samples) == 0 || processed > samples[len(samples)-1].bytes {
				lastAdvance = now
				sample := struct {
					at    time.Time
					bytes int64
				}{now, processed}
				// Keep at most one sample per second, even for fast local copies.
				if len(samples) > 0 && samples[len(samples)-1].at.Truncate(time.Second).Equal(now.Truncate(time.Second)) {
					samples[len(samples)-1] = sample
				} else {
					samples = append(samples, sample)
				}
				for len(samples) > 2 && now.Sub(samples[1].at) > 15*time.Second {
					samples = samples[1:]
				}
			}
			if len(samples) > 1 && now.Sub(lastAdvance) <= 15*time.Second && sample.total > processed {
				first, last := samples[0], samples[len(samples)-1]
				elapsed := last.at.Sub(first.at).Seconds()
				if elapsed > 0 && last.bytes > first.bytes {
					eta := math.Ceil(float64(sample.total-processed) * elapsed / float64(last.bytes-first.bytes))
					if eta >= 0 && eta < float64(math.MaxInt64) {
						seconds := int64(eta)
						p.ETASeconds = &seconds
					}
				}
			}
		}
		if !math.IsNaN(sample.percent) && !math.IsInf(sample.percent, 0) && sample.percent >= 0 {
			percent := math.Min(100, sample.percent)
			p.Percent = &percent
		}
		s.Progress = p
		if lastSave.IsZero() || now.Sub(lastSave) >= time.Second {
			writeErr = e.updateProgress(j)
			lastSave = now
			if writeErr != nil {
				cancel()
			}
		}
	})
	err := operation(context.WithValue(ctx, backupProgressKey{}, report))
	mu.Lock()
	defer mu.Unlock()
	if writeErr != nil {
		return writeErr
	}
	now = e.now().UTC()
	s.FinishedAt = &now
	s.Status = "succeeded"
	if err != nil {
		s.Status = "failed"
		s.Error = backupDiagnostic(err.Error())
	}
	if s.Progress != nil {
		s.Progress.ETASeconds = nil
		if err == nil {
			percent := 100.0
			s.Progress.Percent = &percent
			s.Progress.ProcessedBytes = s.Progress.TotalBytes
		}
		s.Progress.UpdatedAt = now
	}
	if saveErr := e.update(j); saveErr != nil {
		return saveErr
	}
	return err
}

func backupByteSample(total, processed int64) backupProgressSample {
	percent := -1.0
	if total > 0 && processed >= 0 {
		percent = float64(processed) / float64(total) * 100
	}
	return backupProgressSample{total: total, processed: processed, percent: percent}
}

type backupCountingWriter struct {
	ctx              context.Context
	total, processed int64
	write            func([]byte) (int, error)
}

func (w *backupCountingWriter) Write(p []byte) (int, error) {
	n, err := w.write(p)
	w.processed += int64(n)
	reportBackupProgress(w.ctx, backupByteSample(w.total, w.processed))
	return n, err
}

// Bound incomplete lines, accept CLI CR/backspace progress, and discard all
// non-numeric output. An overlong line is ignored until the next delimiter.
type backupProgressLines struct {
	line     []byte
	overflow bool
	accept   func(string)
}

func (w *backupProgressLines) Write(p []byte) (int, error) {
	for _, b := range p {
		if b == '\r' || b == '\n' || b == '\b' {
			w.Flush()
		} else if !w.overflow {
			if len(w.line) >= 64*1024 {
				w.line = nil
				w.overflow = true
			} else {
				w.line = append(w.line, b)
			}
		}
	}
	return len(p), nil
}
func (w *backupProgressLines) Flush() {
	if !w.overflow && len(w.line) > 0 {
		w.accept(strings.TrimSpace(string(w.line)))
	}
	w.line = nil
	w.overflow = false
}

var backupPercentLine = regexp.MustCompile(`^([0-9]{1,3})%(?:\s|$)`)
var backupChunkLine = regexp.MustCompile(`^Info: (?:Uploading|Retrying) .+ chunk \(([0-9]+)-([0-9]+) of ([0-9]+)\)$`)

func backupCompressionProgress(ctx context.Context, total int64) *backupProgressLines {
	return &backupProgressLines{accept: func(line string) {
		m := backupPercentLine.FindStringSubmatch(line)
		if m == nil {
			return
		}
		percent, _ := strconv.ParseFloat(m[1], 64)
		if percent > 100 {
			return
		}
		processed := int64(float64(total) * percent / 100)
		reportBackupProgress(ctx, backupProgressSample{total: total, processed: processed, percent: percent, estimated: true})
	}}
}
func backupStagedBytes(stage string) (int64, error) {
	var total int64
	err := filepath.WalkDir(stage, func(_ string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.Type().IsRegular() {
			info, err := d.Info()
			if err != nil {
				return err
			}
			total += info.Size()
		}
		return nil
	})
	return total, err
}
func backupRcloneProgress(ctx context.Context, total int64) *backupProgressLines {
	return &backupProgressLines{accept: func(line string) {
		var record struct {
			Stats *struct {
				Transferring []struct {
					Bytes *int64 `json:"bytes"`
				} `json:"transferring"`
			} `json:"stats"`
		}
		if json.Unmarshal([]byte(line), &record) != nil || record.Stats == nil {
			return
		}
		if len(record.Stats.Transferring) == 1 && record.Stats.Transferring[0].Bytes != nil {
			n := *record.Stats.Transferring[0].Bytes
			if n >= 0 {
				reportBackupProgress(ctx, backupByteSample(total, n))
			}
		}
	}}
}
