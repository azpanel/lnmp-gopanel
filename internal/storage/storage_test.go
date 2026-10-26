package storage

import (
	"context"
	"errors"
	"fmt"
	"sync/atomic"
	"testing"
	"time"
)

func TestTopFilesBoundedAndSorted(t *testing.T) {
	h := topFiles{}
	for i := 0; i < 1000; i++ {
		h.Add(File{Path: fmt.Sprintf("/file %04d", i), Size: int64(i)})
	}
	if len(h) != Limit {
		t.Fatal("inventory must retain only thirty files")
	}
	files := h.Sorted()
	for i, f := range files {
		if f.Size != int64(999-i) {
			t.Fatalf("rank %d: %+v", i, f)
		}
	}
	h.Add(File{Path: "/large file\nwith newline", Size: 99999})
	if h.Sorted()[0].Path != "/large file\nwith newline" {
		t.Fatal("path changed")
	}
}
func TestTopFilesStableTies(t *testing.T) {
	h := topFiles{}
	for i := 100; i >= 0; i-- {
		h.Add(File{Path: fmt.Sprintf("/%03d", i), Size: 100})
	}
	got := h.Sorted()
	if got[0].Path != "/000" || got[29].Path != "/029" {
		t.Fatalf("unstable ties: %+v", got)
	}
}

type scannerFunc func(context.Context, func(Progress)) error

func (f scannerFunc) Scan(ctx context.Context, report func(Progress)) error { return f(ctx, report) }
func waitJob(t *testing.T, m *Manager) Job {
	t.Helper()
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		j := m.Latest()
		if j.Status != "running" {
			return j
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatal("job did not finish")
	return Job{}
}
func TestAsyncJobSharesWorkAndKeepsOnlyLatestResult(t *testing.T) {
	var calls atomic.Int32
	started := make(chan struct{})
	release := make(chan struct{})
	m := NewManager(context.Background(), scannerFunc(func(ctx context.Context, report func(Progress)) error {
		calls.Add(1)
		report(Progress{Files: []File{{Path: "/some file", Size: 100}}, Scanned: 10})
		close(started)
		<-release
		report(Progress{Files: []File{{Path: "/biggest", Size: 200}}, Scanned: 20, Complete: true})
		return nil
	}))
	first := m.Start()
	if first.Status != "running" || first.Root != "/" {
		t.Fatal(first)
	}
	<-started
	second := m.Start()
	if !second.StartedAt.Equal(*first.StartedAt) || calls.Load() != 1 {
		t.Fatal("duplicate scan started")
	}
	if j := m.Latest(); j.Scanned != 10 {
		t.Fatal("missing progress")
	}
	close(release)
	j := waitJob(t, m)
	if j.Status != "completed" || j.Scanned != 20 || len(j.Files) != 1 || j.Files[0].Path != "/biggest" {
		t.Fatal(j)
	}
}
func TestCancellationAndPartialResults(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	m := NewManager(ctx, scannerFunc(func(ctx context.Context, report func(Progress)) error {
		report(Progress{Files: []File{{Path: "/partial", Size: 10}}, Scanned: 1})
		<-ctx.Done()
		return errors.New("canceled")
	}))
	m.Start()
	cancel()
	j := waitJob(t, m)
	if j.Status != "partial" || !j.Partial || j.Message != "canceled" {
		t.Fatal(j)
	}
}
func TestFailedAndIncompleteScans(t *testing.T) {
	for _, tc := range []struct {
		err      error
		progress Progress
		status   string
	}{{errors.New("missing helper"), Progress{}, "failed"}, {nil, Progress{Files: []File{}, Complete: false}, "failed"}, {nil, Progress{Files: []File{}, Complete: true, Partial: true}, "partial"}} {
		m := NewManager(context.Background(), scannerFunc(func(ctx context.Context, report func(Progress)) error { report(tc.progress); return tc.err }))
		m.Start()
		if j := waitJob(t, m); j.Status != tc.status {
			t.Fatal(j)
		}
	}
}
