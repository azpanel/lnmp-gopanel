package releases

import (
	"context"
	"sync"
	"time"

	"github.com/azpanel/lnmp-gopanel/internal/components"
	"github.com/azpanel/lnmp-gopanel/internal/config"
)

type Release struct {
	Status         string     `json:"status"`
	Version        string     `json:"version"`
	SourceURL      string     `json:"source_url"`
	ChangelogURL   string     `json:"changelog_url"`
	Changelog      string     `json:"changelog"`
	ChangelogError string     `json:"changelog_error,omitempty"`
	Error          string     `json:"error,omitempty"`
	CheckedAt      *time.Time `json:"checked_at"`
	AttemptedAt    *time.Time `json:"attempted_at"`
}

type Source interface {
	Fetch(context.Context, string) (Release, error)
}

type Manager struct {
	mu      sync.Mutex
	ctx     context.Context
	source  Source
	entries map[string]Release
}

func NewManager(ctx context.Context, source Source) *Manager {
	return &Manager{ctx: ctx, source: source, entries: make(map[string]Release)}
}

// Latest only schedules work; requests never wait for upstream network access.
// Concurrent clients share a single in-flight query and a six-hour memory cache.
func (m *Manager) Latest(names []string, force bool) map[string]Release {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make(map[string]Release, len(names))
	for _, name := range names {
		if !config.ValidName(name) && !components.ValidName(name) {
			continue
		}
		entry := m.entries[name]
		ttl := 6 * time.Hour
		if entry.Status == "failed" || entry.ChangelogError != "" {
			ttl = 15 * time.Minute
		}
		if force {
			ttl = time.Minute
		}
		if entry.Status != "loading" && (entry.AttemptedAt == nil || time.Since(*entry.AttemptedAt) >= ttl) && m.ctx.Err() == nil {
			now := time.Now()
			entry.Status = "loading"
			entry.AttemptedAt = &now
			m.entries[name] = entry
			go m.fetch(name)
		}
		out[name] = entry
	}
	return out
}

func (m *Manager) fetch(name string) {
	ctx, cancel := context.WithTimeout(m.ctx, 30*time.Second)
	defer cancel()
	result, err := m.source.Fetch(ctx, name)
	m.mu.Lock()
	defer m.mu.Unlock()
	previous := m.entries[name]
	if err != nil {
		previous.Status = "failed"
		previous.Error = err.Error()
		m.entries[name] = previous
		return
	}
	now := time.Now()
	result.Status = "ready"
	result.CheckedAt = &now
	result.AttemptedAt = previous.AttemptedAt
	m.entries[name] = result
}
