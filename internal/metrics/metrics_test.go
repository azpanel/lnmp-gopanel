package metrics

import (
	"context"
	"testing"
	"time"
)

func TestRate(t *testing.T) {
	for _, tc := range []struct {
		name          string
		a, b          uint64
		seconds, want float64
		ok            bool
	}{{"normal", 100, 300, 2, 100, true}, {"reset", 300, 100, 2, 0, false}, {"zero interval", 100, 300, 0, 0, false}, {"negative interval", 100, 300, -1, 0, false}, {"idle", 100, 100, 2, 0, true}} {
		t.Run(tc.name, func(t *testing.T) {
			v, ok := Rate(tc.a, tc.b, tc.seconds)
			if ok != tc.ok || v != tc.want {
				t.Fatalf("got %v,%v", v, ok)
			}
		})
	}
}
func TestSlowSubscribersOnlyReceiveLatest(t *testing.T) {
	c := New()
	a, stopA := c.Subscribe()
	defer stopA()
	b, stopB := c.Subscribe()
	defer stopB()
	for i := 0; i < 100; i++ {
		c.publish(Snapshot{Timestamp: time.Unix(int64(i+1), 0)})
	}
	for _, ch := range []chan Snapshot{a, b} {
		if len(ch) != 1 {
			t.Fatal("subscriber queues history")
		}
		if v := <-ch; v.Timestamp.Unix() != 100 {
			t.Fatal("subscriber did not receive latest snapshot")
		}
	}
	latest := c.Latest()
	reconnect, stop := c.Subscribe()
	defer stop()
	if got := <-reconnect; !got.Timestamp.Equal(latest.Timestamp) {
		t.Fatal("reconnect should receive only latest")
	}
}
func TestSamplingProducesSnapshot(t *testing.T) {
	c := New()
	c.sample(context.Background())
	s := c.Latest()
	if s.Timestamp.IsZero() || s.Errors == nil {
		t.Fatal("missing snapshot metadata")
	}
	if s.CPU != nil || s.Network != nil || s.DiskIO != nil {
		t.Fatal("first rate sample should be unavailable")
	}
	if s.Memory == nil && s.Errors["memory"] == "" {
		t.Fatal("collection failure must be explicit")
	}
}
func TestRatesResetAndFirstSample(t *testing.T) {
	now := time.Now()
	if rates(counter{}, counter{100, 100, now}) != nil {
		t.Fatal("first observation has no rate")
	}
	if rates(counter{200, 100, now}, counter{100, 200, now.Add(time.Second)}) != nil {
		t.Fatal("reset must invalidate rate")
	}
	r := rates(counter{100, 100, now}, counter{300, 500, now.Add(2 * time.Second)})
	if r.Read != 100 || r.Write != 200 {
		t.Fatalf("invalid rates: %+v", r)
	}
}
