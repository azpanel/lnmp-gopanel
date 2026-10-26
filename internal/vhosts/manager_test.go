package vhosts

import (
	"context"
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
