package vhosts

import (
	"context"
	"errors"
	"sync"
	"time"
)

type Backend interface {
	ListVhosts(context.Context) ([]Site, error)
	VhostUsage(context.Context, func(Usage)) error
	ReadVhost(context.Context, string) (ConfigView, error)
	ToggleVhost(context.Context, string, bool) (ActionResult, error)
}
type Snapshot struct {
	Status    string           `json:"status"`
	Sites     []Site           `json:"sites"`
	Usage     map[string]Usage `json:"usage"`
	UpdatedAt *time.Time       `json:"updated_at"`
	Error     string           `json:"error,omitempty"`
}
type change struct {
	revision uint64
	enabled  bool
}
type Manager struct {
	mu       sync.RWMutex
	actions  sync.Mutex
	ctx      context.Context
	backend  Backend
	snapshot Snapshot
	revision uint64
	changes  map[string]change
}

func NewManager(ctx context.Context, b Backend) *Manager {
	return &Manager{ctx: ctx, backend: b, snapshot: Snapshot{Status: "idle", Sites: []Site{}, Usage: map[string]Usage{}}, changes: map[string]change{}}
}
func (m *Manager) Latest() Snapshot { m.mu.RLock(); defer m.mu.RUnlock(); return m.snapshot }
func (m *Manager) Refresh() Snapshot {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.snapshot.Status == "loading" || m.snapshot.Status == "scanning" {
		return m.snapshot
	}
	m.snapshot.Status = "loading"
	m.snapshot.Error = ""
	revision := m.revision
	go m.refresh(revision)
	return m.snapshot
}
func (m *Manager) refresh(revision uint64) {
	ctx, cancel := context.WithTimeout(m.ctx, 5*time.Minute)
	defer cancel()
	sites, e := m.backend.ListVhosts(ctx)
	if e != nil {
		m.mu.Lock()
		m.snapshot.Status = "failed"
		m.snapshot.Error = e.Error()
		m.mu.Unlock()
		return
	}
	m.mu.Lock()
	for i := range sites {
		if c, ok := m.changes[sites[i].Name]; ok && c.revision > revision {
			sites[i].Enabled = c.enabled
			sites[i].Filename = sites[i].Name
			if !c.enabled {
				sites[i].Filename += ".disabled"
			}
		}
	}
	m.snapshot.Sites = sites
	m.snapshot.Usage = map[string]Usage{}
	m.snapshot.Status = "scanning"
	m.mu.Unlock()
	e = m.backend.VhostUsage(ctx, func(u Usage) {
		m.mu.Lock()
		defer m.mu.Unlock()
		next := make(map[string]Usage, len(m.snapshot.Usage)+1)
		for k, v := range m.snapshot.Usage {
			next[k] = v
		}
		next[u.Path] = u
		m.snapshot.Usage = next
	})
	m.mu.Lock()
	defer m.mu.Unlock()
	now := time.Now()
	m.snapshot.UpdatedAt = &now
	m.snapshot.Status = "ready"
	if e != nil {
		m.snapshot.Error = e.Error()
	}
}
func (m *Manager) Read(ctx context.Context, name string) (ConfigView, error) {
	if !ValidName(name) {
		return ConfigView{}, errors.New("配置文件名无效")
	}
	return m.backend.ReadVhost(ctx, name)
}
func (m *Manager) Toggle(name string, enabled bool) (ActionResult, error) {
	if !ValidName(name) {
		return ActionResult{}, errors.New("配置文件名无效")
	}
	if !m.actions.TryLock() {
		return ActionResult{}, errors.New("另一个站点操作正在执行，请稍后再试")
	}
	defer m.actions.Unlock()
	// A lost browser connection must not interrupt a rename/reload transaction.
	ctx, cancel := context.WithTimeout(m.ctx, 45*time.Second)
	defer cancel()
	result, e := m.backend.ToggleVhost(ctx, name, enabled)
	if e != nil {
		return result, e
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.revision++
	m.changes[name] = change{m.revision, enabled}
	sites := append([]Site{}, m.snapshot.Sites...)
	for i := range sites {
		if sites[i].Name == name {
			sites[i].Enabled = enabled
			sites[i].Filename = name
			if !enabled {
				sites[i].Filename += ".disabled"
			}
		}
	}
	m.snapshot.Sites = sites
	return result, nil
}
