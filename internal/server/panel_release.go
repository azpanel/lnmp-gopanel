package server

import (
	"context"
	"sync"
	"time"

	"github.com/azpanel/lnmp-gopanel/internal/buildinfo"
	"github.com/azpanel/lnmp-gopanel/internal/releases"
	"github.com/gin-gonic/gin"
)

const (
	panelReleaseTTL        = 5 * time.Minute
	panelReleaseFailureTTL = time.Minute
	panelReleaseTimeout    = 12 * time.Second
)

type panelReleaseResult struct {
	release   releases.PanelRelease
	err       error
	expiresAt time.Time
	done      chan struct{}
}

// The running panel has one build version, so one bounded entry serves all
// authenticated sessions. A zero expiry denotes the shared in-flight query.
type panelReleaseCache struct {
	mu     sync.Mutex
	result *panelReleaseResult
	now    func() time.Time
}

func (cache *panelReleaseCache) read(ctx context.Context, fetch func(context.Context) (releases.PanelRelease, error)) (releases.PanelRelease, error) {
	if err := ctx.Err(); err != nil {
		return releases.PanelRelease{}, err
	}
	cache.mu.Lock()
	now := cache.now
	if now == nil {
		now = time.Now
	}
	result := cache.result
	if result == nil || !result.expiresAt.IsZero() && !now().Before(result.expiresAt) {
		result = &panelReleaseResult{done: make(chan struct{})}
		cache.result = result
		go func() {
			// Browser cancellation only stops that caller's wait. Bound the
			// shared network request independently so other callers can finish.
			query, cancel := context.WithTimeout(context.Background(), panelReleaseTimeout)
			defer cancel()
			release, err := fetch(query)
			ttl := panelReleaseTTL
			if err != nil {
				ttl = panelReleaseFailureTTL
			}
			cache.mu.Lock()
			defer cache.mu.Unlock()
			result.release, result.err, result.expiresAt = release, err, now().Add(ttl)
			close(result.done)
		}()
	}
	cache.mu.Unlock()
	select {
	case <-ctx.Done():
		return releases.PanelRelease{}, ctx.Err()
	case <-result.done:
		return result.release, result.err
	}
}

func (s *Server) panelRelease(c *gin.Context) {
	ctx, cancel := context.WithTimeout(c.Request.Context(), panelReleaseTimeout)
	defer cancel()
	release, err := s.panelReleaseCache.read(ctx, func(query context.Context) (releases.PanelRelease, error) {
		source := s.PanelReleases
		if source == nil {
			source = releases.NewFetcher()
		}
		return source.FetchPanel(query, buildinfo.Version)
	})
	if err != nil {
		fail(c, 502, "面板更新检查失败："+err.Error())
		return
	}
	c.JSON(200, release)
}
