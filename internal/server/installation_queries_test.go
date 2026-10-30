package server

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/azpanel/lnmp-gopanel/internal/lnmpinstall"
)

func awaitQueryState(t *testing.T, q *installationQueries, ready func() bool) {
	t.Helper()
	q.init()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		q.mu.Lock()
		ok := ready()
		q.mu.Unlock()
		if ok {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatal("query state did not settle")
}

func TestInstallationQueriesShareWorkAndIndependentCancellation(t *testing.T) {
	var q installationQueries
	var calls atomic.Int32
	key := installationQueryKey{inspect: true}
	release := make(chan struct{})
	workCanceled := make(chan struct{})
	fetch := func(ctx context.Context) (installationValue, error) {
		calls.Add(1)
		deadline, ok := ctx.Deadline()
		if !ok || time.Until(deadline) > 20*time.Second {
			return installationValue{}, errors.New("missing work deadline")
		}
		select {
		case <-ctx.Done():
			close(workCanceled)
			return installationValue{}, ctx.Err()
		case <-release:
			return installationValue{snapshot: lnmpinstall.Snapshot{Installed: true}}, nil
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 32)
	go func() { _, err := q.get(ctx, key, fetch); done <- err }()
	for i := 0; i < 31; i++ {
		go func() { _, err := q.get(context.Background(), key, fetch); done <- err }()
	}
	awaitQueryState(t, &q, func() bool { return q.calls[key] != nil && q.calls[key].waiters == 32 })
	cancel()
	if err := <-done; !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	select {
	case <-workCanceled:
		t.Fatal("one waiter canceled shared work")
	default:
	}
	close(release)
	for range 31 {
		if err := <-done; err != nil {
			t.Fatal(err)
		}
	}
	if calls.Load() != 1 {
		t.Fatal("duplicated shared query", calls.Load())
	}
	if _, err := q.get(context.Background(), key, fetch); err != nil || calls.Load() != 1 {
		t.Fatal("cache miss", err)
	}
}

func TestInstallationQueriesCancelLastWaiterAndDoNotCache(t *testing.T) {
	var q installationQueries
	ctx, cancel := context.WithCancel(context.Background())
	entered, canceled := make(chan struct{}), make(chan struct{})
	done := make(chan error, 1)
	key := installationQueryKey{inspect: true}
	go func() {
		_, err := q.get(ctx, key, func(work context.Context) (installationValue, error) {
			close(entered)
			<-work.Done()
			close(canceled)
			return installationValue{}, work.Err()
		})
		done <- err
	}()
	<-entered
	cancel()
	if err := <-done; !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	select {
	case <-canceled:
	case <-time.After(time.Second):
		t.Fatal("abandoned helper was not canceled")
	}
	awaitQueryState(t, &q, func() bool { return q.active == 0 })
	q.mu.Lock()
	cached := len(q.cache)
	q.mu.Unlock()
	if cached != 0 {
		t.Fatal("canceled query was cached")
	}
	if _, err := q.get(context.Background(), key, func(context.Context) (installationValue, error) { return installationValue{}, nil }); err != nil {
		t.Fatal(err)
	}
}

func TestInstallationQueriesBoundConcurrencyAndCancelableCapacity(t *testing.T) {
	var q installationQueries
	var calls atomic.Int32
	release := make(chan struct{})
	done := make(chan error, 4)
	fetch := func(context.Context) (installationValue, error) {
		calls.Add(1)
		<-release
		return installationValue{}, nil
	}
	for i := 0; i < 4; i++ {
		go func() {
			_, err := q.get(context.Background(), installationQueryKey{request: lnmpinstall.ReadRequest{After: int64(i)}}, fetch)
			done <- err
		}()
	}
	awaitQueryState(t, &q, func() bool { return q.active == 4 && calls.Load() == 4 })
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	_, err := q.get(ctx, installationQueryKey{request: lnmpinstall.ReadRequest{After: 99}}, fetch)
	if !errors.Is(err, context.DeadlineExceeded) || calls.Load() != 4 {
		t.Fatal("capacity did not bound work", err, calls.Load())
	}
	close(release)
	for range 4 {
		if err := <-done; err != nil {
			t.Fatal(err)
		}
	}
}

func TestInstallationQueriesInvalidateInFlightAndCacheErrors(t *testing.T) {
	var q installationQueries
	key := installationQueryKey{inspect: true}
	var calls atomic.Int32
	entered, release := make(chan struct{}), make(chan struct{})
	fetch := func(context.Context) (installationValue, error) {
		n := calls.Add(1)
		if n == 1 {
			close(entered)
			<-release
			return installationValue{snapshot: lnmpinstall.Snapshot{Installed: false}}, nil
		}
		return installationValue{snapshot: lnmpinstall.Snapshot{Installed: true}}, nil
	}
	done := make(chan installationValue, 1)
	go func() { v, _ := q.get(context.Background(), key, fetch); done <- v }()
	<-entered
	q.invalidate()
	fresh, err := q.get(context.Background(), key, fetch)
	if err != nil || !fresh.snapshot.Installed {
		t.Fatal("failed to refresh invalidated state", err)
	}
	close(release)
	if stale := <-done; !stale.snapshot.Installed || calls.Load() != 2 {
		t.Fatal("published pre-invalidation state", calls.Load())
	}
	q.invalidate()
	failure := errors.New("helper failed")
	fail := func(context.Context) (installationValue, error) { calls.Add(1); return installationValue{}, failure }
	for range 2 {
		if _, err := q.get(context.Background(), key, fail); !errors.Is(err, failure) {
			t.Fatal(err)
		}
	}
	if calls.Load() != 3 {
		t.Fatal("failure was not shared")
	}
	q.mu.Lock()
	for _, e := range q.cache {
		entry := e.Value.(installationCached)
		entry.expires = time.Now().Add(-time.Second)
		e.Value = entry
	}
	q.mu.Unlock()
	q.get(context.Background(), key, fail)
	if calls.Load() != 4 {
		t.Fatal("expired failure was not retried")
	}
}

func TestInstallationQueriesBoundLRUAndBytes(t *testing.T) {
	for _, size := range []int{0, 1024 * 1024, installationCacheBytes + 1} {
		t.Run(fmt.Sprint(size), func(t *testing.T) {
			var q installationQueries
			var calls int
			fetch := func(context.Context) (installationValue, error) {
				calls++
				return installationValue{page: lnmpinstall.LogPage{Lines: []lnmpinstall.Line{{Text: strings.Repeat("x", size)}}}}, nil
			}
			for i := 0; i < 34; i++ {
				key := installationQueryKey{request: lnmpinstall.ReadRequest{After: int64(i)}}
				if _, err := q.get(context.Background(), key, fetch); err != nil {
					t.Fatal(err)
				}
				q.mu.Lock()
				count, bytes := len(q.cache), q.bytes
				q.mu.Unlock()
				if count > installationCacheEntries || bytes > installationCacheBytes {
					t.Fatal("unbounded cache", count, bytes)
				}
				if size > installationCacheBytes && count != 0 {
					t.Fatal("oversized page was retained")
				}
				// One oversized result suffices to test bypass without allocating
				// hundreds of MiB under the race detector.
				if size > installationCacheBytes {
					break
				}
			}
			if size == 0 {
				q.get(context.Background(), installationQueryKey{}, fetch)
				if calls != 35 {
					t.Fatal("oldest page was not evicted")
				}
			}
		})
	}
}

type sharedInstallationBackend struct {
	installStub
	inspect func(context.Context) (lnmpinstall.Snapshot, error)
	read    func(context.Context, lnmpinstall.ReadRequest) (lnmpinstall.LogPage, error)
}

func (b *sharedInstallationBackend) InstallInspect(ctx context.Context) (lnmpinstall.Snapshot, error) {
	return b.inspect(ctx)
}
func (b *sharedInstallationBackend) InstallRead(ctx context.Context, r lnmpinstall.ReadRequest) (lnmpinstall.LogPage, error) {
	return b.read(ctx, r)
}

func TestInstallationSSE32ClientsShareInspectAndRead(t *testing.T) {
	s, _ := testServer(t)
	var inspections, reads atomic.Int32
	s.Installation = &sharedInstallationBackend{
		inspect: func(context.Context) (lnmpinstall.Snapshot, error) {
			inspections.Add(1)
			return lnmpinstall.Snapshot{Job: &lnmpinstall.Job{ID: installTestID, Status: "ready"}}, nil
		},
		read: func(context.Context, lnmpinstall.ReadRequest) (lnmpinstall.LogPage, error) {
			reads.Add(1)
			return lnmpinstall.LogPage{Cursor: 100, Lines: []lnmpinstall.Line{{ID: 100, Text: "shared output"}}}, nil
		},
	}
	h := s.Handler()
	cookie, _ := storedAuthSession(s)
	done := make(chan string, 32)
	for range 32 {
		go func() {
			w := request(h, "GET", "/api/v1/lnmp-installation/jobs/"+installTestID+"/events", "", cookie, "", "")
			if w.Code != http.StatusOK {
				done <- fmt.Sprint(w.Code)
				return
			}
			done <- w.Body.String()
		}()
	}
	for range 32 {
		body := <-done
		if !strings.Contains(body, "shared output") || !strings.Contains(body, "event:complete") {
			t.Fatal(body)
		}
	}
	if inspections.Load() != 1 || reads.Load() != 1 {
		t.Fatalf("client count amplified helper calls: inspect=%d read=%d", inspections.Load(), reads.Load())
	}
}

func TestInstallationTerminalStreamDoesNotReuseRunningEmptyPage(t *testing.T) {
	s, _ := testServer(t)
	var reads atomic.Int32
	s.Installation = &sharedInstallationBackend{
		inspect: func(context.Context) (lnmpinstall.Snapshot, error) {
			return lnmpinstall.Snapshot{Job: &lnmpinstall.Job{ID: installTestID, Status: "ready"}}, nil
		},
		read: func(context.Context, lnmpinstall.ReadRequest) (lnmpinstall.LogPage, error) {
			if reads.Add(1) == 1 {
				return lnmpinstall.LogPage{Lines: []lnmpinstall.Line{}}, nil
			}
			return lnmpinstall.LogPage{Cursor: 100, Lines: []lnmpinstall.Line{{ID: 100, Text: "last line"}}}, nil
		},
	}
	h := s.Handler()
	cookie, _ := storedAuthSession(s)
	if w := request(h, "GET", "/api/v1/lnmp-installation/jobs/"+installTestID+"/logs", "", cookie, "", ""); w.Code != 200 {
		t.Fatal(w.Body.String())
	}
	w := request(h, "GET", "/api/v1/lnmp-installation/jobs/"+installTestID+"/events", "", cookie, "", "")
	if !strings.Contains(w.Body.String(), "last line") || !strings.Contains(w.Body.String(), "event:complete") || reads.Load() != 2 {
		t.Fatal("terminal output lost", w.Body.String(), reads.Load())
	}
}

func TestInstallationStreamPaginationAndReconnect(t *testing.T) {
	s, _ := testServer(t)
	var reads atomic.Int32
	s.Installation = &sharedInstallationBackend{
		inspect: func(context.Context) (lnmpinstall.Snapshot, error) {
			return lnmpinstall.Snapshot{Job: &lnmpinstall.Job{ID: installTestID, Status: "ready"}}, nil
		},
		read: func(_ context.Context, r lnmpinstall.ReadRequest) (lnmpinstall.LogPage, error) {
			reads.Add(1)
			next := r.After + 100
			return lnmpinstall.LogPage{Cursor: next, More: next == 100, Lines: []lnmpinstall.Line{{ID: next, Text: fmt.Sprintf("page-%d", next)}}}, nil
		},
	}
	h := s.Handler()
	cookie, _ := storedAuthSession(s)
	path := "/api/v1/lnmp-installation/jobs/" + installTestID + "/events"
	w := request(h, "GET", path, "", cookie, "", "")
	if strings.Count(w.Body.String(), "event:log") != 2 || !strings.Contains(w.Body.String(), "page-100") || !strings.Contains(w.Body.String(), "page-200") || !strings.Contains(w.Body.String(), "event:complete") {
		t.Fatal("pagination lost output", w.Body.String())
	}
	r := httptest.NewRequest("GET", path, nil)
	r.AddCookie(cookie)
	r.Header.Set("Last-Event-ID", "100")
	w = httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if strings.Contains(w.Body.String(), "page-100") || !strings.Contains(w.Body.String(), "page-200") || reads.Load() != 2 {
		t.Fatal("reconnect replayed output or missed shared page", w.Body.String(), reads.Load())
	}
}

func TestInstallationStreamRejectsStalledCursor(t *testing.T) {
	s, _ := testServer(t)
	s.Installation = &sharedInstallationBackend{
		inspect: func(context.Context) (lnmpinstall.Snapshot, error) {
			return lnmpinstall.Snapshot{Job: &lnmpinstall.Job{ID: installTestID, Status: "ready"}}, nil
		},
		read: func(context.Context, lnmpinstall.ReadRequest) (lnmpinstall.LogPage, error) {
			return lnmpinstall.LogPage{More: true, Lines: []lnmpinstall.Line{}}, nil
		},
	}
	h := s.Handler()
	cookie, _ := storedAuthSession(s)
	w := request(h, "GET", "/api/v1/lnmp-installation/jobs/"+installTestID+"/events", "", cookie, "", "")
	if !strings.Contains(w.Body.String(), "event:failure") || strings.Contains(w.Body.String(), "event:complete") {
		t.Fatal(w.Body.String())
	}
}
