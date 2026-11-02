//go:build linux

package control

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/azpanel/lnmp-gopanel/internal/backups"
	"golang.org/x/sys/unix"
)

func backupExecutionFixture(t *testing.T) (backupExecutor, *time.Time, *[]string) {
	t.Helper()
	p := backupTestInstaller(t)
	now := time.Date(2026, 11, 2, 2, 0, 0, 0, time.FixedZone("CST", 8*3600))
	launched := []string{}
	e := backupExecutor{store: backupPolicyStore{state: p.state, accountHome: p.accountHome}, helper: p.helper, now: func() time.Time { return now }}
	e.run = func(_ context.Context, program string, args ...string) (string, error) {
		if program == "/usr/bin/systemd-run" {
			launched = append(launched, args[len(args)-1])
			return "", nil
		}
		return "ActiveState=active\nLoadState=loaded", nil
	}
	return e, &now, &launched
}
func saveExecutionPolicy(t *testing.T, e backupExecutor, name string) backups.Policy {
	t.Helper()
	r := sampleBackupPolicy()
	r.Name = name
	r.Schedule = "* * * * *"
	p, err := e.store.save(r)
	if err != nil {
		t.Fatal(err)
	}
	return p
}
func finishExecution(t *testing.T, e backupExecutor, id string) {
	t.Helper()
	j, err := e.loadRun(id)
	if err != nil {
		t.Fatal(err)
	}
	j.Status = "succeeded"
	now := e.now().UTC()
	j.FinishedAt = &now
	if err = e.update(&j); err != nil {
		t.Fatal(err)
	}
}

func TestBackupExecutionQueueConcurrencyTickDeduplicationAndSnapshots(t *testing.T) {
	e, now, launched := backupExecutionFixture(t)
	ctx := context.Background()
	policies := []backups.Policy{saveExecutionPolicy(t, e, "一"), saveExecutionPolicy(t, e, "二"), saveExecutionPolicy(t, e, "三")}
	if err := e.dispatch(ctx, true); err != nil {
		t.Fatal(err)
	}
	if len(*launched) != 2 {
		t.Fatal("concurrency limit", *launched)
	}
	index, err := e.index()
	if err != nil || len(index.Runs) != 3 || index.Runs[2].Status != "queued" {
		t.Fatal(index, err)
	}
	if err = e.dispatch(ctx, true); err != nil {
		t.Fatal(err)
	}
	again, _ := e.index()
	if len(again.Runs) != 3 {
		t.Fatal("duplicate tick", again)
	}
	r := backups.SavePolicyRequest{Policy: policies[2], PasswordAction: "replace", Password: "private-new-password"}
	r.Name = "新版"
	updated, err := e.store.save(r)
	if err != nil {
		t.Fatal(err)
	}
	queued, err := e.loadRun(index.Runs[2].ID)
	if err != nil || queued.Snapshot.Name != "三" || queued.Revision == updated.Revision {
		t.Fatal("snapshot changed", queued, err)
	}
	if _, err = e.start(ctx, backups.PolicyIdentity{ID: policies[0].ID, Revision: policies[0].Revision}); !errors.Is(err, backups.ErrRunBusy) {
		t.Fatal("duplicate start", err)
	}
	finishExecution(t, e, index.Runs[0].ID)
	if err = e.dispatch(ctx, false); err != nil {
		t.Fatal(err)
	}
	if len(*launched) != 3 || (*launched)[2] != index.Runs[2].ID {
		t.Fatal("FIFO not resumed", *launched)
	}
	*now = now.Add(time.Minute)
	if err = e.dispatch(ctx, true); err != nil {
		t.Fatal(err)
	}
	index, _ = e.index()
	skipped := 0
	for _, ref := range index.Runs {
		if ref.Status == "skipped" {
			skipped++
		}
	}
	if skipped != 2 {
		t.Fatal("busy scheduled runs not recorded", skipped, index)
	}
	if _, err = e.start(ctx, backups.PolicyIdentity{ID: updated.ID, Revision: updated.Revision - 1}); !errors.Is(err, backups.ErrConflict) {
		t.Fatal(err)
	}
	response, err := e.store.list()
	if err != nil {
		t.Fatal(err)
	}
	for _, p := range response {
		if p.ID == policies[1].ID && (p.LastRun == nil || p.LastRun.Status != "running") {
			t.Fatal("active run hidden by skipped tick", p)
		}
	}
	raw, _ := json.Marshal(response)
	if strings.Contains(string(raw), "private-new-password") || strings.Contains(string(raw), "snapshot") || strings.Contains(string(raw), "ciphertext") {
		t.Fatal("public secret leak", string(raw))
	}
}

func TestBackupExecutionDisableDeleteAndManualDisabledPolicy(t *testing.T) {
	e, _, _ := backupExecutionFixture(t)
	ctx := context.Background()
	first := saveExecutionPolicy(t, e, "first")
	_ = saveExecutionPolicy(t, e, "second")
	third := saveExecutionPolicy(t, e, "third")
	if err := e.dispatch(ctx, true); err != nil {
		t.Fatal(err)
	}
	if err := e.store.delete(backups.PolicyIdentity{ID: first.ID, Revision: first.Revision}); !errors.Is(err, backups.ErrRunBusy) {
		t.Fatal("deleted running policy", err)
	}
	r := backups.SavePolicyRequest{Policy: third, PasswordAction: "keep"}
	r.Enabled = false
	third, err := e.store.save(r)
	if err != nil {
		t.Fatal(err)
	}
	history, err := e.list(backups.RunQuery{ID: third.ID})
	if err != nil || history.Total != 1 || history.Items[0].Status != "cancelled" {
		t.Fatal(history, err)
	}
	j, err := e.start(ctx, backups.PolicyIdentity{ID: third.ID, Revision: third.Revision})
	if err != nil || j.Status != "queued" {
		t.Fatal(j, err)
	}
	if err = e.store.delete(backups.PolicyIdentity{ID: third.ID, Revision: third.Revision}); err != nil {
		t.Fatal(err)
	}
	history, err = e.list(backups.RunQuery{ID: third.ID})
	if err != nil || history.Items[0].Status != "cancelled" {
		t.Fatal(history, err)
	}
}

func TestBackupExecutionInterruptionAndHistoryRetention(t *testing.T) {
	e, now, _ := backupExecutionFixture(t)
	ctx := context.Background()
	p := saveExecutionPolicy(t, e, "test")
	j, err := e.start(ctx, backups.PolicyIdentity{ID: p.ID, Revision: p.Revision})
	if err != nil {
		t.Fatal(err)
	}
	if err = os.MkdirAll(e.workPath(j.ID), 0700); err != nil {
		t.Fatal(err)
	}
	*now = now.Add(2 * time.Minute)
	e.run = func(context.Context, string, ...string) (string, error) {
		return "ActiveState=inactive\nLoadState=not-found", nil
	}
	if err = e.dispatch(ctx, false); err != nil {
		t.Fatal(err)
	}
	recovered, err := e.loadRun(j.ID)
	if err != nil || recovered.Status != "failed" || recovered.Snapshot != nil {
		t.Fatal(recovered, err)
	}
	if _, err = os.Stat(e.workPath(j.ID)); !os.IsNotExist(err) {
		t.Fatal("interrupted workspace leaked", err)
	}
	index, _ := e.index()
	records := []string{}
	for n := 0; n < 105; n++ {
		*now = now.Add(time.Minute)
		run, enqueueErr := e.enqueue(&index, storedBackupPolicy{Policy: p}, "manual", nil)
		if enqueueErr != nil {
			t.Fatal(enqueueErr)
		}
		run.Status = "succeeded"
		if err = e.saveRun(&run); err != nil {
			t.Fatal(err)
		}
		index.Runs[len(index.Runs)-1].Status = "succeeded"
		records = append(records, run.ID)
	}
	if err = e.saveIndex(index); err != nil {
		t.Fatal(err)
	}
	if err = e.dispatch(ctx, false); err != nil {
		t.Fatal(err)
	}
	history, err := e.list(backups.RunQuery{ID: p.ID})
	if err != nil || history.Total != 100 || len(history.Items) != 20 {
		t.Fatal(history, err)
	}
	if _, err = e.loadRun(records[0]); !errors.Is(err, backups.ErrNotFound) {
		t.Fatal("old history retained", err)
	}
	last, err := e.list(backups.RunQuery{ID: p.ID, Page: 5})
	if err != nil || len(last.Items) != 20 {
		t.Fatal(last, err)
	}
}

func TestBackupExecutionWorkerLifetimeLockAndCancellation(t *testing.T) {
	e, _, _ := backupExecutionFixture(t)
	ctx := context.Background()
	p := saveExecutionPolicy(t, e, "worker")
	j, err := e.start(ctx, backups.PolicyIdentity{ID: p.ID, Revision: p.Revision})
	if err != nil {
		t.Fatal(err)
	}
	entered := make(chan struct{})
	resume := make(chan struct{})
	e.execute = func(ctx context.Context, j *storedBackupRun) error {
		close(entered)
		select {
		case <-resume:
			j.Status = "succeeded"
			return nil
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	done := make(chan error, 1)
	go func() { done <- e.worker(ctx, j.ID) }()
	<-entered
	if err = e.worker(ctx, j.ID); err == nil {
		t.Fatal("duplicate worker accepted")
	}
	close(resume)
	if err = <-done; err != nil {
		t.Fatal(err)
	}
	finished, err := e.loadRun(j.ID)
	if err != nil || finished.Status != "succeeded" || finished.Snapshot != nil {
		t.Fatal(finished, err)
	}
	j, err = e.start(ctx, backups.PolicyIdentity{ID: p.ID, Revision: p.Revision})
	if err != nil {
		t.Fatal(err)
	}
	cancelled, cancel := context.WithCancel(ctx)
	cancel()
	e.execute = func(ctx context.Context, _ *storedBackupRun) error { return ctx.Err() }
	if err = e.worker(cancelled, j.ID); err != nil {
		t.Fatal(err)
	}
	finished, _ = e.loadRun(j.ID)
	if finished.Status != "failed" || finished.FinishedAt == nil {
		t.Fatal(finished)
	}
}

func TestBackupExecutionSimultaneousStartsAndInstallExclusion(t *testing.T) {
	e, _, _ := backupExecutionFixture(t)
	p := saveExecutionPolicy(t, e, "concurrent")
	var group sync.WaitGroup
	results := make(chan error, 8)
	for range 8 {
		group.Add(1)
		go func() {
			defer group.Done()
			_, err := e.start(context.Background(), backups.PolicyIdentity{ID: p.ID, Revision: p.Revision})
			results <- err
		}()
	}
	group.Wait()
	close(results)
	ok := 0
	for err := range results {
		if err == nil {
			ok++
		} else if !errors.Is(err, backups.ErrRunBusy) {
			t.Fatal(err)
		}
	}
	if ok != 1 {
		t.Fatal("duplicate reservations", ok)
	}
	e.installLock = filepath.Join(e.store.state, "install.lock")
	f, err := os.OpenFile(e.installLock, os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if err = unix.Flock(int(f.Fd()), unix.LOCK_EX|unix.LOCK_NB); err != nil {
		t.Fatal(err)
	}
	defer unix.Flock(int(f.Fd()), unix.LOCK_UN)
	if _, err = e.start(context.Background(), backups.PolicyIdentity{ID: p.ID, Revision: p.Revision}); !errors.Is(err, backups.ErrRunBusy) {
		t.Fatal("installer not excluded", err)
	}
	if err = e.dispatch(context.Background(), true); err != nil {
		t.Fatal(err)
	}
}

func TestBackupExecutionScheduleTimezoneAndNoCatchup(t *testing.T) {
	e, now, _ := backupExecutionFixture(t)
	p := saveExecutionPolicy(t, e, "timezone")
	r := backups.SavePolicyRequest{Policy: p, PasswordAction: "keep"}
	r.Schedule = "30 2 * * 1"
	if _, err := e.store.save(r); err != nil {
		t.Fatal(err)
	}
	*now = time.Date(2026, 11, 2, 2, 31, 0, 0, now.Location())
	if err := e.dispatch(context.Background(), true); err != nil {
		t.Fatal(err)
	}
	i, _ := e.index()
	if len(i.Runs) != 0 {
		t.Fatal("missed minute caught up", i)
	}
	*now = now.Add(-time.Minute)
	if err := e.dispatch(context.Background(), true); err != nil {
		t.Fatal(err)
	}
	i, _ = e.index()
	if len(i.Runs) != 1 {
		t.Fatal("server timezone not honored", i)
	}
}

func TestBackupExecutionLongRunResultSurvivesSkippedHistory(t *testing.T) {
	e, now, _ := backupExecutionFixture(t)
	p := saveExecutionPolicy(t, e, "long-running")
	j, err := e.start(context.Background(), backups.PolicyIdentity{ID: p.ID, Revision: p.Revision})
	if err != nil {
		t.Fatal(err)
	}
	for range 105 {
		*now = now.Add(time.Minute)
		if err = e.dispatch(context.Background(), true); err != nil {
			t.Fatal(err)
		}
	}
	history, err := e.list(backups.RunQuery{ID: p.ID})
	if err != nil || history.Total != 100 || history.Items[0].ID != j.ID {
		t.Fatal("active result lost in skipped ticks", history, err)
	}
	*now = now.Add(10 * time.Second)
	finishExecution(t, e, j.ID)
	if err = e.dispatch(context.Background(), false); err != nil {
		t.Fatal(err)
	}
	history, err = e.list(backups.RunQuery{ID: p.ID})
	if err != nil || history.Total != 100 || history.Items[0].ID != j.ID || history.Items[0].Status != "succeeded" {
		t.Fatal("completed result immediately pruned", history, err)
	}
	summaries, err := e.store.list()
	if err != nil || summaries[0].LastRun == nil || summaries[0].LastRun.ID != j.ID {
		t.Fatal("latest result hidden", summaries, err)
	}
}

func TestBackupExecutionUncertainSystemdStartRetainsReservation(t *testing.T) {
	e, _, _ := backupExecutionFixture(t)
	p := saveExecutionPolicy(t, e, "uncertain-start")
	e.run = func(_ context.Context, program string, _ ...string) (string, error) {
		if program == "/usr/bin/systemd-run" {
			return "", context.DeadlineExceeded
		}
		return "ActiveState=active\nLoadState=loaded", nil
	}
	j, err := e.start(context.Background(), backups.PolicyIdentity{ID: p.ID, Revision: p.Revision})
	if err != nil || j.Status != "running" {
		t.Fatal("live unit lost its reservation", j, err)
	}
	if _, err = e.start(context.Background(), backups.PolicyIdentity{ID: p.ID, Revision: p.Revision}); !errors.Is(err, backups.ErrRunBusy) {
		t.Fatal("uncertain unit allowed duplicate", err)
	}
	finishExecution(t, e, j.ID)
	e.run = func(_ context.Context, program string, _ ...string) (string, error) {
		if program == "/usr/bin/systemd-run" {
			return "", errors.New("start failed")
		}
		return "LoadState=not-found\nActiveState=inactive", nil
	}
	j, err = e.start(context.Background(), backups.PolicyIdentity{ID: p.ID, Revision: p.Revision})
	if err != nil || j.Status != "failed" {
		t.Fatal("definitive failure not recorded", j, err)
	}
}

func TestBackupExecutionEncryptedSnapshotNeverInventsReplacementKey(t *testing.T) {
	e, _, _ := backupExecutionFixture(t)
	r := sampleBackupPolicy()
	r.PasswordAction = "replace"
	r.Password = "old-password"
	p, err := e.store.save(r)
	if err != nil {
		t.Fatal(err)
	}
	j, err := e.start(context.Background(), backups.PolicyIdentity{ID: p.ID, Revision: p.Revision})
	if err != nil {
		t.Fatal(err)
	}
	snapshot, err := e.loadRun(j.ID)
	if err != nil {
		t.Fatal(err)
	}
	r.Policy = p
	r.PasswordAction = "clear"
	r.Password = ""
	p, err = e.store.save(r)
	if err != nil {
		t.Fatal(err)
	}
	key := filepath.Join(e.store.state, "policies.key")
	if err = os.Remove(key); err != nil {
		t.Fatal(err)
	}
	if password, err := e.store.decryptBackupPassword(*snapshot.Snapshot); err == nil {
		clear(password)
		t.Fatal("missing snapshot key accepted")
	}
	if _, err = os.Stat(key); !os.IsNotExist(err) {
		t.Fatal("worker invented a key", err)
	}
	r.Policy = p
	r.PasswordAction = "replace"
	r.Password = "new-password"
	if _, err = e.store.save(r); err == nil {
		t.Fatal("replacement key created while encrypted snapshot exists")
	}
	if _, err = os.Stat(key); !os.IsNotExist(err) {
		t.Fatal("policy save invented a key", err)
	}
}

func TestBackupExecutionRecoversCrashBetweenRunAndIndexCommit(t *testing.T) {
	e, _, _ := backupExecutionFixture(t)
	p := saveExecutionPolicy(t, e, "commit-gap")
	run, err := e.start(context.Background(), backups.PolicyIdentity{ID: p.ID, Revision: p.Revision})
	if err != nil {
		t.Fatal(err)
	}
	if err = os.MkdirAll(e.workPath(run.ID), 0700); err != nil {
		t.Fatal(err)
	}
	j, err := e.loadRun(run.ID)
	if err != nil {
		t.Fatal(err)
	}
	j.Status = "succeeded"
	if err = e.saveRun(&j); err != nil {
		t.Fatal(err)
	}
	if err = e.dispatch(context.Background(), false); err != nil {
		t.Fatal(err)
	}
	if _, err = os.Stat(e.workPath(run.ID)); !os.IsNotExist(err) {
		t.Fatal("committed result retained private work files", err)
	}
	index, err := e.index()
	if err != nil || index.Runs[0].Status != "succeeded" {
		t.Fatal(index, err)
	}
}
