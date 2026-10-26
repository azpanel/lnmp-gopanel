package components

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"
)

type backend struct {
	calls    atomic.Int32
	results  chan Snapshot
	failures chan error
}

func (b *backend) Components(ctx context.Context) (Snapshot, error) {
	b.calls.Add(1)
	select {
	case result := <-b.results:
		return result, nil
	case err := <-b.failures:
		return Snapshot{}, err
	case <-ctx.Done():
		return Snapshot{}, ctx.Err()
	}
}
func wait(t *testing.T, m *Manager) Snapshot {
	t.Helper()
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		result := m.Latest(false)
		if result.Status != "loading" {
			return result
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatal("collection did not finish")
	return Snapshot{}
}
func expire(m *Manager) { m.mu.Lock(); m.attempted = time.Now().Add(-2 * time.Minute); m.mu.Unlock() }
func TestAsyncCacheAndFailurePreservation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	b := &backend{results: make(chan Snapshot, 1), failures: make(chan error, 1)}
	m := NewManager(ctx, b)
	if result := m.Latest(false); result.Status != "loading" || len(result.Items) != 4 {
		t.Fatal(result)
	}
	for i := 0; i < 10; i++ {
		m.Latest(true)
	}
	b.results <- Snapshot{Status: "ready", Items: []Item{{Name: "node", Status: "ready", Version: "22.23.4", Path: "/root/.nvm/node"}}}
	result := wait(t, m)
	if b.calls.Load() != 1 || result.Items[0].Version != "22.23.4" {
		t.Fatal(result, b.calls.Load())
	}
	m.Latest(true)
	if b.calls.Load() != 1 {
		t.Fatal("manual refresh bypassed minimum interval")
	}
	expire(m)
	m.Latest(false)
	b.results <- Snapshot{Status: "partial", Items: []Item{{Name: "node", Status: "failed", Error: "timeout"}}}
	result = wait(t, m)
	if result.Items[0].Version != "22.23.4" || result.Items[0].Error != "timeout" {
		t.Fatal(result)
	}
	expire(m)
	m.Latest(false)
	b.failures <- errors.New("helper unavailable")
	result = wait(t, m)
	if result.Status != "failed" || result.Error == "" || result.Items[0].Version != "22.23.4" {
		t.Fatal(result)
	}
	expire(m)
	m.Latest(false)
	b.results <- Snapshot{Status: "ready", Items: []Item{{Name: "node", Status: "missing"}}}
	result = wait(t, m)
	if result.Items[0].Version != "" || result.Error != "" {
		t.Fatal(result)
	}
}
