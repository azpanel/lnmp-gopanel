//go:build linux

package control

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/azpanel/lnmp-gopanel/internal/backups"
)

func TestBackupPreparationReportsWithinLargeFileAndCancellation(t *testing.T) {
	e, _, _ := backupExecutionFixture(t)
	source := filepath.Join(e.store.state, "large-file")
	data := make([]byte, 256<<10)
	if err := os.WriteFile(source, data, 0600); err != nil {
		t.Fatal(err)
	}
	for _, cancelled := range []bool{false, true} {
		stage := filepath.Join(e.store.state, "work", fmt.Sprintf("stage-%t", cancelled))
		if err := os.MkdirAll(stage, 0700); err != nil {
			t.Fatal(err)
		}
		ctx, cancel := context.WithCancel(context.Background())
		var samples []backupProgressSample
		ctx = context.WithValue(ctx, backupProgressKey{}, backupProgressReporter(func(sample backupProgressSample) {
			samples = append(samples, sample)
			if cancelled && sample.processed > 0 {
				cancel()
			}
		}))
		err := e.stageBackupFiles(ctx, []backups.FileSource{{Path: source}}, stage, &backupManifest{})
		cancel()
		if (err != nil) != cancelled {
			t.Fatalf("cancelled=%t: %v", cancelled, err)
		}
		intermediate := false
		for _, sample := range samples {
			if sample.processed > 0 && sample.processed < int64(len(data)) {
				intermediate = true
			}
		}
		if !intermediate {
			t.Fatal("no progress within a large file", samples)
		}
		last := samples[len(samples)-1]
		if cancelled && (last.total >= 0 || last.percent >= 0) {
			t.Fatal("cancelled copy reported completion", last)
		}
	}
}

func TestBackupVerificationProgressIncludesDigest(t *testing.T) {
	tool := backupArchiveTestTool(t)
	dir := t.TempDir()
	stage := filepath.Join(dir, "stage")
	if err := os.Mkdir(stage, 0700); err != nil {
		t.Fatal(err)
	}
	data := make([]byte, 256<<10)
	if err := os.WriteFile(filepath.Join(stage, "data"), data, 0600); err != nil {
		t.Fatal(err)
	}
	archive := filepath.Join(dir, "archive.zip")
	if err := createBackupArchive(context.Background(), tool, stage, archive, backups.Compression{Format: "zip", Level: 0}, nil); err != nil {
		t.Fatal(err)
	}
	contents, err := os.ReadFile(archive)
	if err != nil {
		t.Fatal(err)
	}
	var samples []backupProgressSample
	size, digest, err := verifyBackupArchiveDigest(progressContext(&samples), tool, stage, archive, nil)
	wantDigest := sha256.Sum256(contents)
	if err != nil || size != int64(len(contents)) || digest != hex.EncodeToString(wantDigest[:]) {
		t.Fatal("verification changed digest", size, digest, err)
	}
	previous := int64(0)
	checked, hashed := false, false
	for _, sample := range samples {
		if sample.total != int64(len(data)+len(contents)) || sample.processed < previous || !sample.estimated {
			t.Fatal("incorrect combined verification progress", samples)
		}
		if sample.percent >= 0 && sample.processed > 0 && sample.processed <= int64(len(data)) {
			checked = true
		}
		if sample.processed > int64(len(data)) && sample.processed < sample.total {
			hashed = true
		}
		previous = sample.processed
	}
	if !checked || !hashed || samples[len(samples)-1].percent != 100 {
		t.Fatal("integrity or hashing progress missing", samples)
	}
	if err := os.WriteFile(archive, []byte("broken archive"), 0600); err != nil {
		t.Fatal(err)
	}
	samples = nil
	if _, _, err := verifyBackupArchiveDigest(progressContext(&samples), tool, stage, archive, nil); err == nil {
		t.Fatal("corrupt archive passed verification")
	}
	for _, sample := range samples {
		if sample.percent == 100 {
			t.Fatal("corrupt archive reported completion")
		}
	}
}

func progressContext(samples *[]backupProgressSample) context.Context {
	return context.WithValue(context.Background(), backupProgressKey{}, backupProgressReporter(func(s backupProgressSample) { *samples = append(*samples, s) }))
}

func TestBackupProgressParsersBoundAndSplitOutput(t *testing.T) {
	var samples []backupProgressSample
	ctx := progressContext(&samples)
	parser := backupCompressionProgress(ctx, 1000)
	for _, part := range []string{"secret\n  1", "2% 3 file\r", "\b 50%\b", "200%\r", strings.Repeat("x", 65537) + "70%\n", " 80%"} {
		_, _ = parser.Write([]byte(part))
	}
	parser.Flush()
	if len(samples) != 3 || samples[0].processed != 120 || samples[1].percent != 50 || samples[2].processed != 800 || !samples[2].estimated {
		t.Fatalf("unexpected samples: %+v", samples)
	}
	samples = nil
	parser = backupRcloneProgress(ctx, 1000)
	for _, line := range []string{`{"stats":{"bytes":9000,"transferring":[{"bytes":250}]}}`, `{"stats":{"transferring":[{"bytes":-1}]}}`, `{"stats":{"transferring":[{"bytes":null}]}}`, `{"msg":"secret"}`, `{"stats":{"transferring":[{"bytes":1},{"bytes":2}]}}`, `malformed`} {
		_, _ = parser.Write([]byte(line + "\n"))
	}
	if len(samples) != 1 || samples[0].processed != 250 {
		t.Fatalf("retry totals or malformed stats accepted: %+v", samples)
	}
}

func progressRunFixture(t *testing.T) (backupExecutor, *time.Time, storedBackupRun) {
	t.Helper()
	e, now, _ := backupExecutionFixture(t)
	p := saveExecutionPolicy(t, e, "进度测试")
	run, err := e.start(context.Background(), backups.PolicyIdentity{ID: p.ID, Revision: p.Revision})
	if err != nil {
		t.Fatal(err)
	}
	j, err := e.loadRun(run.ID)
	if err != nil {
		t.Fatal(err)
	}
	return e, now, j
}

func TestBackupStepProgressThrottleETAAndTerminalGuard(t *testing.T) {
	e, now, j := progressRunFixture(t)
	id := "databases-compress"
	err := e.runStep(context.Background(), &j, id, func(ctx context.Context) error {
		indexBefore, err := os.ReadFile(filepath.Join(e.store.state, "run-index.json"))
		if err != nil {
			t.Fatal(err)
		}
		reportBackupProgress(ctx, backupByteSample(100, 0))
		reportBackupProgress(ctx, backupByteSample(100, 10))
		disk, err := e.loadRun(j.ID)
		if err != nil {
			t.Fatal(err)
		}
		if *backupStep(&disk, id).Progress.ProcessedBytes != 0 {
			t.Fatal("progress was not throttled")
		}
		*now = now.Add(time.Second)
		reportBackupProgress(ctx, backupByteSample(100, 20))
		step := backupStep(&j, id)
		if step.Progress.ETASeconds == nil {
			t.Fatal("ETA missing after progress")
		}
		reportBackupProgress(ctx, backupByteSample(100, 5))
		if *step.Progress.ProcessedBytes != 20 {
			t.Fatal("retry regressed progress")
		}
		*now = now.Add(16 * time.Second)
		reportBackupProgress(ctx, backupByteSample(100, 20))
		if step.Progress.ETASeconds != nil {
			t.Fatal("stalled ETA retained")
		}
		indexAfter, _ := os.ReadFile(filepath.Join(e.store.state, "run-index.json"))
		if string(indexAfter) != string(indexBefore) {
			t.Fatal("progress rewrote history index")
		}
		reportBackupProgress(ctx, backupByteSample(100, 99))
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	disk, err := e.loadRun(j.ID)
	if err != nil {
		t.Fatal(err)
	}
	step := backupStep(&disk, id)
	if step.Status != "succeeded" || *step.Progress.Percent != 100 || *step.Progress.ProcessedBytes != 100 || step.Progress.ETASeconds != nil {
		t.Fatalf("final sample not flushed: %+v", step)
	}
	list, err := e.list(backups.RunQuery{ID: j.PolicyID})
	if err != nil || len(list.Items) != 1 || len(list.Items[0].Steps) != 0 {
		t.Fatal("history exposes full steps", err)
	}
	disk.Status = "failed"
	disk.Error = "interrupted"
	// Simulate interruption between committing the run and its index.
	if err := e.saveRun(&disk); err != nil {
		t.Fatal(err)
	}
	if err := e.updateProgress(&j); !errors.Is(err, errBackupRunFinished) {
		t.Fatal("late sample overwrote terminal run", err)
	}
	if err := e.update(&j); !errors.Is(err, errBackupRunFinished) {
		t.Fatal("late phase overwrote terminal run", err)
	}
}

func TestBackupStepFailuresZeroBytesAndCancellation(t *testing.T) {
	for _, mode := range []string{"failed", "cancelled", "zero"} {
		t.Run(mode, func(t *testing.T) {
			e, _, j := progressRunFixture(t)
			id := "databases-compress"
			err := e.runStep(context.Background(), &j, id, func(ctx context.Context) error {
				if mode == "zero" {
					reportBackupProgress(ctx, backupByteSample(0, 0))
					return nil
				}
				reportBackupProgress(ctx, backupByteSample(100, 25))
				if mode == "cancelled" {
					return context.Canceled
				}
				return errors.New("fixture failure")
			})
			step := backupStep(&j, id)
			if mode == "zero" {
				if err != nil || *step.Progress.Percent != 100 || *step.Progress.ProcessedBytes != 0 {
					t.Fatal("empty operation failed", err)
				}
			} else if err == nil || step.Status != "failed" || *step.Progress.Percent != 25 {
				t.Fatal("failure fabricated completion")
			}
			j.Status = mode
			if mode == "zero" {
				j.Status = "succeeded"
			}
			// Simulate a separate operation interrupted before it could finalize.
			backupStep(&j, "databases-verify").Status = "running"
			if err := e.update(&j); err != nil {
				t.Fatal(err)
			}
			for _, s := range j.Steps {
				if s.Status == "running" || s.Status == "pending" {
					t.Fatal("unfinished step survived terminal state", s)
				}
			}
		})
	}
}

func TestBackupActualCompressionAndRcloneProgress(t *testing.T) {
	tool := backupArchiveTestTool(t)
	dir := t.TempDir()
	stage := filepath.Join(dir, "stage")
	if err := os.Mkdir(stage, 0700); err != nil {
		t.Fatal(err)
	}
	data := make([]byte, 4<<20)
	// Incompressible enough to exercise real 7-Zip output without a large fixture.
	for i := range data {
		data[i] = byte((i*31 + i/257) % 251)
	}
	source := filepath.Join(stage, "source")
	if err := os.WriteFile(source, data, 0600); err != nil {
		t.Fatal(err)
	}
	var samples []backupProgressSample
	if err := compressBackupArchive(progressContext(&samples), tool, stage, filepath.Join(dir, "archive.zip"), backups.Compression{Format: "zip", Level: 5}, nil); err != nil {
		t.Fatal(err)
	}
	found := false
	for _, s := range samples {
		if s.percent >= 0 && s.total == int64(len(data)) {
			found = true
		}
	}
	if !found {
		t.Fatal("real 7-Zip did not produce parsable progress", samples)
	}
	rclone := backupTool("rclone")
	if rclone == "" {
		t.Skip("rclone unavailable")
	}
	samples = nil
	ctx, cancel := context.WithTimeout(progressContext(&samples), 10*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, rclone, "copyto", "--config", filepath.Join(dir, "empty.conf"), "--use-json-log", "--stats", "100ms", "--stats-log-level", "NOTICE", "--bwlimit", "2M", source, filepath.Join(dir, "copied"))
	parser := backupRcloneProgress(ctx, int64(len(data)))
	cmd.Stderr = parser
	if err := cmd.Run(); err != nil {
		t.Fatal(err)
	}
	parser.Flush()
	found = false
	for _, s := range samples {
		if s.processed > 0 && s.processed < int64(len(data)) {
			found = true
		}
	}
	if !found {
		t.Fatal("real rclone did not produce intermediate progress", samples)
	}
}

func TestBackupGdriveChunkProgressKeepsFileID(t *testing.T) {
	e, _, _ := backupExecutionFixture(t)
	base := filepath.Join(e.accountHome(), ".config/gdrive3", "fixture")
	if err := os.MkdirAll(base, 0700); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"secret.json", "tokens.json"} {
		if err := os.WriteFile(filepath.Join(base, name), []byte(`{}`), 0600); err != nil {
			t.Fatal(err)
		}
	}
	tool := filepath.Join(e.store.state, "gdrive")
	script := "#!/bin/sh\nprintf 'Info: Uploading 32 B chunk (0-31 of 100)\\nInfo: Retrying 32 B chunk (0-31 of 100)\\nInfo: Uploading 32 B chunk (32-63 of 100)\\nInfo: Uploading 36 B chunk (64-99 of 100)\\nfixture_object_id'\n"
	if err := os.WriteFile(tool, []byte(script), 0700); err != nil {
		t.Fatal(err)
	}
	e.tools = func(string) string { return tool }
	archive := filepath.Join(e.store.state, "archive.zip")
	if err := os.WriteFile(archive, make([]byte, 100), 0600); err != nil {
		t.Fatal(err)
	}
	var samples []backupProgressSample
	id, err := e.deliver(progressContext(&samples), strings.Repeat("a", 32), backups.Destination{Type: "gdrive", Account: "fixture", FolderID: "root"}, archive, "archive.zip")
	if err != nil || id != "fixture_object_id" {
		t.Fatal(id, err)
	}
	if len(samples) != 5 || samples[len(samples)-1].processed != 64 {
		t.Fatal("chunk end counted before acknowledgement", samples)
	}
}
