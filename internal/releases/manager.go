package releases

import (
	"context"
	"sync"
	"time"

	"github.com/azpanel/lnmp-gopanel/internal/components"
	"github.com/azpanel/lnmp-gopanel/internal/config"
)

type Release struct {
	Range          string     `json:"range"`
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
	FetchRange(context.Context, string, string) (Release, error)
}

type Manager struct {
	mu         sync.Mutex
	ctx        context.Context
	source     Source
	entries    map[string]Release
	ranges     *rangeStore
	rangeError error
}

func NewManager(ctx context.Context, source Source) *Manager {
	return &Manager{ctx: ctx, source: source, entries: make(map[string]Release)}
}

// Latest only schedules work; requests never wait for upstream network access.
// Concurrent clients share an in-flight query and a six-hour cache per service and range.
func (m *Manager) Latest(names []string, force bool) map[string]Release {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.latestLocked(names, force)
}

func (m *Manager) latestLocked(names []string, force bool) map[string]Release {
	out := make(map[string]Release, len(names))
	for _, name := range names {
		if !config.ValidName(name) && !components.ValidName(name) {
			continue
		}
		if RangeService(name) && m.rangeError != nil {
			out[name] = Release{Status: "failed", Error: ErrRangeStorage.Error()}
			continue
		}
		scope := ""
		if m.ranges != nil {
			scope = m.ranges.values[name]
		}
		key := releaseKey(name, scope)
		entry := m.entries[key]
		entry.Range = scope
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
			m.entries[key] = entry
			go m.fetch(name, scope)
		}
		out[name] = entry
	}
	return out
}

func releaseKey(name, scope string) string {
	if scope == "" {
		return name
	}
	return name + "@" + scope
}

func (m *Manager) fetch(name, scope string) {
	ctx, cancel := context.WithTimeout(m.ctx, 30*time.Second)
	defer cancel()
	result, err := m.source.FetchRange(ctx, name, scope)
	key := releaseKey(name, scope)
	m.mu.Lock()
	defer m.mu.Unlock()
	previous := m.entries[key]
	if err != nil {
		previous.Status = "failed"
		previous.Error = err.Error()
		m.entries[key] = previous
		return
	}
	now := time.Now()
	result.Range = scope
	result.Status = "ready"
	result.CheckedAt = &now
	result.AttemptedAt = previous.AttemptedAt
	m.entries[key] = result
}
