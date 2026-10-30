package server

import (
	"container/list"
	"context"
	"encoding/json"
	"errors"
	"sync"
	"time"

	"github.com/azpanel/lnmp-gopanel/internal/lnmpinstall"
)

const installationCacheTTL = time.Second
const installationCacheEntries = 32
const installationCacheBytes = 8 * 1024 * 1024
const installationQueryLimit = 4

type installationQueryKey struct {
	inspect bool
	request lnmpinstall.ReadRequest
	// A terminal read must not reuse an empty page collected while the worker
	// was still running, including a query that is still in flight.
	terminal   bool
	generation uint64
}
type installationValue struct {
	snapshot lnmpinstall.Snapshot
	page     lnmpinstall.LogPage
}
type installationCall struct {
	done    chan struct{}
	cancel  context.CancelFunc
	waiters int
	value   installationValue
	err     error
}
type installationCached struct {
	key     installationQueryKey
	value   installationValue
	err     error
	expires time.Time
	size    int
}

// Queries are demand-driven. The zero value is ready to use; neither cache
// maintenance nor clients waiting for capacity require background goroutines.
type installationQueries struct {
	once       sync.Once
	mu         sync.Mutex
	generation uint64
	calls      map[installationQueryKey]*installationCall
	cache      map[installationQueryKey]*list.Element
	lru        list.List
	bytes      int
	active     int
	available  chan struct{}
}

func (q *installationQueries) init() {
	q.once.Do(func() {
		q.calls = make(map[installationQueryKey]*installationCall)
		q.cache = make(map[installationQueryKey]*list.Element)
		q.available = make(chan struct{})
	})
}

func (q *installationQueries) invalidate() {
	q.init()
	q.mu.Lock()
	defer q.mu.Unlock()
	q.generation++
	clear(q.cache)
	q.lru.Init()
	q.bytes = 0
}

func (q *installationQueries) remove(e *list.Element) {
	entry := e.Value.(installationCached)
	q.bytes -= entry.size
	delete(q.cache, entry.key)
	q.lru.Remove(e)
}

func (q *installationQueries) get(ctx context.Context, key installationQueryKey, fetch func(context.Context) (installationValue, error)) (installationValue, error) {
	q.init()
	for {
		q.mu.Lock()
		if err := ctx.Err(); err != nil {
			q.mu.Unlock()
			return installationValue{}, err
		}
		key.generation = q.generation
		if e := q.cache[key]; e != nil {
			entry := e.Value.(installationCached)
			if time.Now().Before(entry.expires) {
				q.lru.MoveToFront(e)
				q.mu.Unlock()
				return entry.value, entry.err
			}
			q.remove(e)
		}
		call := q.calls[key]
		if call == nil {
			if q.active >= installationQueryLimit {
				available := q.available
				q.mu.Unlock()
				select {
				case <-ctx.Done():
					return installationValue{}, ctx.Err()
				case <-available:
					continue
				}
			}
			work, cancel := context.WithTimeout(context.WithoutCancel(ctx), 20*time.Second)
			call = &installationCall{done: make(chan struct{}), cancel: cancel}
			q.calls[key] = call
			q.active++
			go q.run(work, key, call, fetch)
		}
		call.waiters++
		q.mu.Unlock()
		select {
		case <-ctx.Done():
			q.mu.Lock()
			call.waiters--
			if call.waiters == 0 {
				call.cancel()
				if q.calls[key] == call {
					delete(q.calls, key)
				}
			}
			q.mu.Unlock()
			return installationValue{}, ctx.Err()
		case <-call.done:
			q.mu.Lock()
			call.waiters--
			stale := key.generation != q.generation
			q.mu.Unlock()
			if stale {
				continue
			}
			if err := ctx.Err(); err != nil {
				return installationValue{}, err
			}
			return call.value, call.err
		}
	}
}

func (q *installationQueries) run(ctx context.Context, key installationQueryKey, call *installationCall, fetch func(context.Context) (installationValue, error)) {
	value, err := fetch(ctx)
	var encoded []byte
	if key.inspect {
		encoded, _ = json.Marshal(value.snapshot)
	} else {
		encoded, _ = json.Marshal(value.page)
	}
	size := len(encoded)
	if err != nil {
		size += len(err.Error())
	}
	q.mu.Lock()
	defer q.mu.Unlock()
	call.value, call.err = value, err
	if q.calls[key] == call {
		delete(q.calls, key)
		// Canceled/deadline work must not poison subsequent clients' cache.
		if key.generation == q.generation && call.waiters > 0 && ctx.Err() == nil && !errors.Is(err, context.Canceled) && !errors.Is(err, context.DeadlineExceeded) && size <= installationCacheBytes {
			for q.lru.Len() >= installationCacheEntries || q.bytes+size > installationCacheBytes {
				q.remove(q.lru.Back())
			}
			entry := installationCached{key: key, value: value, err: err, expires: time.Now().Add(installationCacheTTL), size: size}
			q.cache[key] = q.lru.PushFront(entry)
			q.bytes += size
		}
	}
	call.cancel()
	q.active--
	close(q.available)
	q.available = make(chan struct{})
	close(call.done)
}

func (s *Server) installationSnapshot(ctx context.Context) (lnmpinstall.Snapshot, error) {
	v, err := s.installQueries.get(ctx, installationQueryKey{inspect: true}, func(work context.Context) (installationValue, error) {
		snapshot, err := s.inspectInstallation(work)
		return installationValue{snapshot: snapshot}, err
	})
	return v.snapshot, err
}

func (s *Server) installationRead(ctx context.Context, r lnmpinstall.ReadRequest, terminal bool) (lnmpinstall.LogPage, error) {
	v, err := s.installQueries.get(ctx, installationQueryKey{request: r, terminal: terminal}, func(work context.Context) (installationValue, error) {
		page, err := s.Installation.InstallRead(work, r)
		return installationValue{page: page}, err
	})
	return v.page, err
}
