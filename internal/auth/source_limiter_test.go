package auth

import (
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestSourceLimiterWindowsAndRetry(t *testing.T) {
	now := time.Unix(1800000000, 0)
	l := SourceLimiter{now: func() time.Time { return now }}
	for range 10 {
		if allowed, _ := l.Allow("a"); !allowed {
			t.Fatal("early rejection")
		}
	}
	now = now.Add(1500 * time.Millisecond)
	if allowed, retry := l.Allow("a"); allowed || retry != 58500*time.Millisecond {
		t.Fatalf("wrong rolling window: %v %v", allowed, retry)
	}
	if allowed, _ := l.Allow("b"); !allowed {
		t.Fatal("one source blocked another")
	}
	now = now.Add(58500 * time.Millisecond)
	if allowed, _ := l.Allow("a"); !allowed {
		t.Fatal("window did not expire at exactly one minute")
	}
	// Ten spaced arrivals must expire individually, rather than at a fixed reset.
	l = SourceLimiter{now: func() time.Time { return now }}
	for range 10 {
		l.Allow("a")
		now = now.Add(time.Second)
	}
	now = now.Add(50 * time.Second)
	if allowed, _ := l.Allow("a"); !allowed {
		t.Fatal("oldest attempt did not expire")
	}
	if allowed, retry := l.Allow("a"); allowed || retry != time.Second {
		t.Fatalf("rolling window reset too much: %v %v", allowed, retry)
	}
}

func TestSourceLimiterCapacityAndPruning(t *testing.T) {
	now := time.Unix(1800000000, 0)
	l := SourceLimiter{now: func() time.Time { return now }}
	for i := range MaxLoginSources {
		if allowed, _ := l.Allow(fmt.Sprint(i)); !allowed {
			t.Fatal("early capacity rejection", i)
		}
	}
	if allowed, retry := l.Allow("new"); allowed || retry != time.Minute || len(l.sources) != MaxLoginSources {
		t.Fatal("source table is not bounded")
	}
	for range 9 {
		if allowed, _ := l.Allow("0"); !allowed {
			t.Fatal("full table rejected an existing source")
		}
	}
	if allowed, _ := l.Allow("0"); allowed {
		t.Fatal("active window was evicted")
	}
	now = now.Add(time.Minute)
	if allowed, _ := l.Allow("new"); !allowed || len(l.sources) != 1 {
		t.Fatal("expired sources were not reclaimed")
	}
}

func TestSourceLimiterConcurrentAttempts(t *testing.T) {
	var l SourceLimiter
	var accepted atomic.Int32
	var workers sync.WaitGroup
	for range 100 {
		workers.Go(func() {
			if allowed, _ := l.Allow("same"); allowed {
				accepted.Add(1)
			}
		})
	}
	workers.Wait()
	if accepted.Load() != 10 {
		t.Fatal("concurrent requests exceeded limit", accepted.Load())
	}
}
