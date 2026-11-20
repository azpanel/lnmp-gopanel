package server

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"testing/synctest"
	"time"

	"github.com/azpanel/lnmp-gopanel/internal/releases"
)

func TestPanelReleaseCacheExpiryAndRecovery(t *testing.T) {
	now := time.Date(2026, 11, 20, 12, 0, 0, 0, time.UTC)
	cache := panelReleaseCache{now: func() time.Time { return now }}
	want := releases.PanelRelease{CurrentVersion: "v1.0.0", LatestVersion: "v1.0.1", HasUpdate: true}
	upstreamError := errors.New("upstream unavailable")
	var fetchError error
	calls := 0
	fetch := func(context.Context) (releases.PanelRelease, error) {
		calls++
		// Account for network time: TTLs start when a fetch finishes.
		now = now.Add(10 * time.Second)
		if fetchError != nil {
			return releases.PanelRelease{}, fetchError
		}
		return want, nil
	}
	check := func(wantCalls int, wantError error) {
		t.Helper()
		got, err := cache.read(t.Context(), fetch)
		if calls != wantCalls || !errors.Is(err, wantError) {
			t.Fatalf("calls=%d want=%d error=%v want=%v", calls, wantCalls, err, wantError)
		}
		if err == nil && got != want || err != nil && got != (releases.PanelRelease{}) {
			t.Fatalf("unexpected release: %+v", got)
		}
	}
	check(1, nil)
	successAt := now
	fetchError = upstreamError
	now = successAt.Add(panelReleaseTTL - time.Nanosecond)
	check(1, nil)
	now = successAt.Add(panelReleaseTTL)
	check(2, upstreamError)
	failureAt := now
	fetchError = nil
	want.LatestVersion = "v1.0.2"
	now = failureAt.Add(panelReleaseFailureTTL - time.Nanosecond)
	check(2, upstreamError)
	now = failureAt.Add(panelReleaseFailureTTL)
	check(3, nil)
	check(3, nil)
}

func TestPanelReleaseCacheConcurrentReadersAndCancellation(t *testing.T) {
	synctest.Test(t, testPanelReleaseCacheConcurrentReadersAndCancellation)
}

func testPanelReleaseCacheConcurrentReadersAndCancellation(t *testing.T) {
	var cache panelReleaseCache
	var calls atomic.Int32
	started := make(chan context.Context, 1)
	release := make(chan struct{})
	releaseFetch := sync.OnceFunc(func() { close(release) })
	// Release the shared fetch even if an assertion ends the test early.
	defer releaseFetch()
	want := releases.PanelRelease{CurrentVersion: "v1.0.0", LatestVersion: "v1.0.1", HasUpdate: true}
	fetch := func(ctx context.Context) (releases.PanelRelease, error) {
		if calls.Add(1) == 1 {
			started <- ctx
		}
		select {
		case <-release:
			return want, nil
		case <-ctx.Done():
			return releases.PanelRelease{}, ctx.Err()
		}
	}
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	first := make(chan error, 1)
	go func() { _, err := cache.read(ctx, fetch); first <- err }()
	query := <-started
	if deadline, ok := query.Deadline(); !ok || time.Until(deadline) > panelReleaseTimeout {
		t.Fatal("shared fetch must have its own bounded deadline")
	}
	cancel()
	if err := <-first; !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled waiter: %v", err)
	}
	if err := query.Err(); err != nil {
		t.Fatalf("one waiter canceled the shared fetch: %v", err)
	}

	const readers = 16
	results := make(chan error, readers)
	for range readers {
		go func() {
			got, err := cache.read(t.Context(), fetch)
			if err == nil && got != want {
				err = errors.New("unexpected shared release")
			}
			results <- err
		}()
	}
	// Ensure every reader is waiting on the in-flight result before the source
	// completes, so cache hits alone cannot make this coalescing test pass.
	synctest.Wait()
	if calls.Load() != 1 {
		t.Fatalf("in-flight readers made %d upstream calls", calls.Load())
	}
	releaseFetch()
	for range readers {
		if err := <-results; err != nil {
			t.Fatal(err)
		}
	}
	if calls.Load() != 1 {
		t.Fatalf("concurrent readers made %d upstream calls", calls.Load())
	}
}

func TestPanelReleaseCacheTimeoutAndBackoff(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		var cache panelReleaseCache
		calls := 0
		fetch := func(ctx context.Context) (releases.PanelRelease, error) {
			calls++
			<-ctx.Done()
			return releases.PanelRelease{}, ctx.Err()
		}
		start := time.Now()
		for range 2 {
			if _, err := cache.read(t.Context(), fetch); !errors.Is(err, context.DeadlineExceeded) {
				t.Fatalf("shared fetch timeout: %v", err)
			}
		}
		if calls != 1 || time.Since(start) != panelReleaseTimeout {
			t.Fatalf("timeout not cached: calls=%d elapsed=%s", calls, time.Since(start))
		}
		time.Sleep(panelReleaseFailureTTL)
		if _, err := cache.read(t.Context(), fetch); !errors.Is(err, context.DeadlineExceeded) || calls != 2 {
			t.Fatalf("timeout did not retry after backoff: calls=%d error=%v", calls, err)
		}
	})
}

func TestPanelReleaseCacheAlreadyCanceledReaderDoesNotFetch(t *testing.T) {
	var cache panelReleaseCache
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	_, err := cache.read(ctx, func(context.Context) (releases.PanelRelease, error) {
		t.Error("an already canceled request must not start a fetch")
		return releases.PanelRelease{}, nil
	})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled request: %v", err)
	}
}
