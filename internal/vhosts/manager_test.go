package vhosts

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

type testBackend struct {
	calls        atomic.Int32
	usageStarted chan struct{}
	release      chan struct{}
}

func (b *testBackend) ListVhosts(context.Context) ([]Site, error) {
	b.calls.Add(1)
	return []Site{{Name: "site.conf", Filename: "site.conf", Enabled: true}}, nil
}
func (b *testBackend) VhostUsage(ctx context.Context, report func(Usage)) error {
	close(b.usageStarted)
	select {
	case <-b.release:
		n := uint64(8192)
		report(Usage{Path: "/site", Bytes: &n})
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}
func (b *testBackend) ReadVhost(context.Context, string) (ConfigView, error) {
	return ConfigView{}, nil
}
func (b *testBackend) ToggleVhost(ctx context.Context, name string, enabled bool) (ActionResult, error) {
	return ActionResult{Name: name, Enabled: enabled, Changed: true}, nil
}
func TestRefreshPublishesMetadataBeforeUsageAndPreservesToggle(t *testing.T) {
	b := &testBackend{usageStarted: make(chan struct{}), release: make(chan struct{})}
	m := NewManager(context.Background(), b)
	m.Refresh()
	<-b.usageStarted
	early := m.Latest()
	if early.Status != "scanning" || len(early.Sites) != 1 {
		t.Fatal(early)
	}
	if count, err := m.Count(context.Background()); err != nil || count != 1 || b.calls.Load() != 1 {
		t.Fatal("count should reuse refreshed metadata", count, err)
	}
	m.Refresh()
	if b.calls.Load() != 1 {
		t.Fatal("parallel directory scans started")
	}
	if _, e := m.Toggle("site.conf", false); e != nil {
		t.Fatal(e)
	}
	close(b.release)
	deadline := time.Now().Add(time.Second)
	for m.Latest().Status != "ready" && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	s := m.Latest()
	if s.Sites[0].Enabled || s.Sites[0].Filename != "site.conf.disabled" || s.Usage["/site"].Bytes == nil {
		t.Fatal(s)
	}
}

type countBackend struct {
	testBackend
	sites []Site
	err   error
}

func (b *countBackend) ListVhosts(context.Context) ([]Site, error) {
	b.calls.Add(1)
	return b.sites, b.err
}
func TestCountsAreSharedAndNeverScanUsage(t *testing.T) {
	b := &countBackend{testBackend: testBackend{usageStarted: make(chan struct{})}, sites: []Site{{Name: "a.conf", Enabled: true}, {Name: "b.conf", Enabled: false}}}
	m := NewManager(context.Background(), b)
	var wg sync.WaitGroup
	for i := 0; i < 12; i++ {
		wg.Go(func() {
			if count, err := m.Count(context.Background()); err != nil || count != 2 {
				t.Errorf("count=%d err=%v", count, err)
			}
		})
	}
	wg.Wait()
	if b.calls.Load() != 1 {
		t.Fatal("concurrent counts were not cached")
	}
	select {
	case <-b.usageStarted:
		t.Fatal("counter started a directory usage scan")
	default:
	}
	if m.Latest().Status != "idle" {
		t.Fatal("counter changed page scan state")
	}
}
func TestCountDistinguishesEmptyAndErrorAndCachesFailures(t *testing.T) {
	for _, err := range []error{nil, errors.New("helper unavailable")} {
		b := &countBackend{err: err}
		m := NewManager(context.Background(), b)
		for i := 0; i < 2; i++ {
			count, e := m.Count(context.Background())
			if count != 0 || !errors.Is(e, err) {
				t.Fatal(count, e)
			}
		}
		if b.calls.Load() != 1 {
			t.Fatal("empty count/error was not cached")
		}
	}
}
