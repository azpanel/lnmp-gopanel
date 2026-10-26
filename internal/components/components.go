// Package components tracks installed development tools independently of services.
package components

import (
	"context"
	"sync"
	"time"
)

const Timeout = 10 * time.Second

func Names() []string { return []string{"python3", "go", "node", "codex"} }
func ValidName(name string) bool {
	for _, n := range Names() {
		if name == n {
			return true
		}
	}
	return false
}

type Item struct {
	Name      string     `json:"name"`
	Status    string     `json:"status"`
	Version   string     `json:"version"`
	Path      string     `json:"path"`
	Error     string     `json:"error,omitempty"`
	CheckedAt *time.Time `json:"checked_at"`
}
type Snapshot struct {
	Status string `json:"status"`
	Items  []Item `json:"items"`
	Error  string `json:"error,omitempty"`
}
type Backend interface {
	Components(context.Context) (Snapshot, error)
}
type Manager struct {
	mu        sync.Mutex
	ctx       context.Context
	backend   Backend
	latest    Snapshot
	attempted time.Time
}

func NewManager(ctx context.Context, backend Backend) *Manager {
	items := make([]Item, 0, 4)
	for _, name := range Names() {
		items = append(items, Item{Name: name, Status: "loading"})
	}
	return &Manager{ctx: ctx, backend: backend, latest: Snapshot{Status: "idle", Items: items}}
}
func (m *Manager) Latest(force bool) Snapshot {
	m.mu.Lock()
	defer m.mu.Unlock()
	ttl := time.Minute
	if force {
		ttl = 5 * time.Second
	}
	if m.latest.Status != "loading" && time.Since(m.attempted) >= ttl && m.ctx.Err() == nil {
		m.attempted = time.Now()
		m.latest.Status = "loading"
		go m.collect()
	}
	return m.latest
}
func (m *Manager) collect() {
	ctx, cancel := context.WithTimeout(m.ctx, Timeout+2*time.Second)
	defer cancel()
	result, err := m.backend.Components(ctx)
	m.mu.Lock()
	defer m.mu.Unlock()
	if err != nil {
		m.latest.Status, m.latest.Error = "failed", err.Error()
		return
	}
	// Failed probes retain the last successful version; a missing installation clears it.
	for i := range result.Items {
		item := &result.Items[i]
		if item.Status != "failed" {
			continue
		}
		for _, old := range m.latest.Items {
			if old.Name == item.Name {
				item.Version, item.Path = old.Version, old.Path
			}
		}
	}
	m.latest = result
}
