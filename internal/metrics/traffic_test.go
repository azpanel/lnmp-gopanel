package metrics

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func trafficFixture(name, period string, updated int64, entries string) []byte {
	return []byte(fmt.Sprintf(`{"vnstatversion":"2.13","jsonversion":"2","interfaces":[{"name":%q,"updated":{"timestamp":%d},"traffic":{%q:[%s]}}]}`, name, updated, period, entries))
}

func TestTrafficPeriodsWindowsGapsAndBytes(t *testing.T) {
	zone := time.FixedZone("CST", 8*3600)
	now := time.Date(2026, 11, 18, 13, 25, 0, 0, zone)
	for _, period := range []string{"day", "hour"} {
		t.Run(period, func(t *testing.T) {
			end := time.Date(2026, 11, 18, 0, 0, 0, 0, zone)
			step, count := 24*time.Hour, 30
			if period == "hour" {
				end = end.Add(13 * time.Hour)
				step, count = time.Hour, 24
			}
			start := end.Add(-time.Duration(count-1) * step)
			entries := fmt.Sprintf(`{"timestamp":%d,"rx":1073741824,"tx":1024},{"timestamp":%d,"rx":0,"tx":0},{"timestamp":%d,"rx":99,"tx":99}`, end.Unix(), start.Unix(), start.Add(-step).Unix())
			got, err := parseTraffic(trafficFixture("eth0", period, now.Unix(), entries), "eth0", period, now)
			if err != nil {
				t.Fatal(err)
			}
			if len(got.Points) != count || *got.Points[0].RX != 0 || got.Points[1].RX != nil || *got.Points[count-1].RX != 1073741824 || *got.Points[count-1].TX != 1024 {
				t.Fatalf("bad window or bytes: %+v", got)
			}
			if !got.Points[0].Time.Equal(start) || !got.UpdatedAt.Equal(now) {
				t.Fatal("wrong timestamps")
			}
			if !strings.HasPrefix(got.Points[count-1].Label, "2026-11-18") {
				t.Fatal("wrong timezone", got.Points[count-1].Label)
			}
			body, _ := json.Marshal(got)
			if !strings.Contains(string(body), `"rx":null`) {
				t.Fatal("gap represented as zero")
			}
		})
	}
}

func TestTrafficEmptyMissingAndInvalidData(t *testing.T) {
	now := time.Now()
	for _, entries := range []string{"", `{"timestamp":1,"rx":0,"tx":0}`} {
		got, err := parseTraffic(trafficFixture("eth0", "day", now.Unix(), entries), "eth0", "day", now)
		if err != nil || len(got.Points) != 0 {
			t.Fatal("empty/outdated history", got, err)
		}
	}
	valid := string(trafficFixture("eth0", "day", now.Unix(), ""))
	for _, body := range []string{
		`{`, `null`, valid + ` {}`, strings.Replace(valid, `"jsonversion":"2"`, `"jsonversion":"1"`, 1),
		strings.Replace(valid, `"vnstatversion":"2.13"`, `"vnstatversion":"1.18"`, 1),
		string(trafficFixture("eth0", "day", now.Unix(), `{"timestamp":123,"rx":-1,"tx":0}`)),
		string(trafficFixture("eth0", "day", now.Unix(), `{"timestamp":123,"rx":1}`)),
	} {
		if _, err := parseTraffic([]byte(body), "eth0", "day", now); err == nil {
			t.Fatalf("accepted %s", body)
		}
	}
	if _, err := parseTraffic([]byte(valid), "eth1", "day", now); err == nil || !strings.Contains(err.Error(), "eth1") {
		t.Fatal("wrong interface accepted", err)
	}
}

func TestTrafficServerTimezoneAcrossDST(t *testing.T) {
	zone, err := time.LoadLocation("America/New_York")
	if err != nil {
		t.Fatal(err)
	}
	// 01:30 occurs twice on this day. Query during the second occurrence.
	now := time.Date(2026, 11, 1, 6, 30, 0, 0, time.UTC).In(zone)
	end := now.Add(-30 * time.Minute)
	entries := fmt.Sprintf(`{"timestamp":%d,"rx":1,"tx":2},{"timestamp":%d,"rx":3,"tx":4}`, end.Add(-time.Hour).Unix(), end.Unix())
	got, err := parseTraffic(trafficFixture("eth0", "hour", now.Unix(), entries), "eth0", "hour", now)
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Points) != 24 || got.Points[22].Label != "2026-11-01 01:00 -04:00" || got.Points[23].Label != "2026-11-01 01:00 -05:00" || *got.Points[23].RX != 3 {
		t.Fatal("repeated hour lost", got)
	}
	day := time.Date(2026, 11, 1, 0, 0, 0, 0, zone)
	entries = fmt.Sprintf(`{"timestamp":%d,"rx":1,"tx":2}`, day.Unix())
	got, err = parseTraffic(trafficFixture("eth0", "day", now.Unix(), entries), "eth0", "day", now)
	if err != nil || len(got.Points) != 30 || got.Points[29].Label != "2026-11-01" || *got.Points[29].RX != 1 {
		t.Fatal("day window lost", got, err)
	}
}

func TestTrafficCacheConcurrentReadersCancellationAndRouteChange(t *testing.T) {
	r := NewTrafficReader()
	now := time.Now()
	name := "eth0"
	r.now = func() time.Time { return now }
	r.interfaceName = func() (string, error) { return name, nil }
	var calls atomic.Int32
	started, release := make(chan struct{}), make(chan struct{})
	r.run = func(ctx context.Context, name, period string) ([]byte, error) {
		if calls.Add(1) == 1 {
			close(started)
			<-release
		}
		return trafficFixture(name, period, now.Unix(), ""), nil
	}
	ctx, cancel := context.WithCancel(t.Context())
	first := make(chan error, 1)
	go func() { _, err := r.Read(ctx, "day"); first <- err }()
	<-started
	var wg sync.WaitGroup
	for range 12 {
		wg.Go(func() {
			if _, err := r.Read(t.Context(), "day"); err != nil {
				t.Error(err)
			}
		})
	}
	cancel()
	if err := <-first; !errors.Is(err, context.Canceled) {
		t.Fatal("cancellation", err)
	}
	close(release)
	wg.Wait()
	if calls.Load() != 1 {
		t.Fatal("queries not coalesced", calls.Load())
	}
	if _, err := r.Read(t.Context(), "day"); err != nil || calls.Load() != 1 {
		t.Fatal("cache missed", err)
	}
	now = now.Add(time.Minute)
	r.Read(t.Context(), "day")
	r.Read(t.Context(), "hour")
	name = "eth1"
	got, err := r.Read(t.Context(), "day")
	if err != nil || got.Interface != "eth1" || calls.Load() != 4 || len(r.cache) != 1 {
		t.Fatal("cache expiry, period or route change", got, err, calls.Load())
	}
}

func TestTrafficFailuresAreCached(t *testing.T) {
	r := NewTrafficReader()
	r.interfaceName = func() (string, error) { return "eth0", nil }
	var calls int
	r.run = func(context.Context, string, string) ([]byte, error) { calls++; return nil, errors.New("unavailable") }
	for range 2 {
		if _, err := r.Read(t.Context(), "day"); err == nil {
			t.Fatal("failure hidden")
		}
	}
	if calls != 1 {
		t.Fatal("failure not cached")
	}
	if _, err := r.Read(t.Context(), "invalid"); err == nil || calls != 1 {
		t.Fatal("invalid period queried vnstat")
	}
}

func TestVNStatCommandArgumentsErrorsAndOutputLimit(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("PATH", dir)
	if _, err := runVNStat(t.Context(), "eth0", "day"); err == nil || !strings.Contains(err.Error(), "未安装") {
		t.Fatal(err)
	}
	program := filepath.Join(dir, "vnstat")
	for _, period := range []string{"day", "hour"} {
		mode, limit := "d", "30"
		if period == "hour" {
			mode, limit = "h", "24"
		}
		script := fmt.Sprintf("#!/bin/sh\n[ \"$1\" = --json ] && [ \"$2\" = %s ] && [ \"$3\" = %s ] && [ \"$4\" = -i ] && [ \"$5\" = 'eth0;false' ] || exit 1\nprintf success\n", mode, limit)
		if err := os.WriteFile(program, []byte(script), 0755); err != nil {
			t.Fatal(err)
		}
		body, err := runVNStat(t.Context(), "eth0;false", period)
		if err != nil || string(body) != "success" {
			t.Fatal("arguments", string(body), err)
		}
	}
	for _, tc := range []struct{ script, message string }{
		{"printf 'Permission denied' >&2; exit 1", "权限不足"},
		{"exit 1", "查询失败"},
		{"while :; do :; done", "查询超时"},
	} {
		if err := os.WriteFile(program, []byte("#!/bin/sh\n"+tc.script), 0755); err != nil {
			t.Fatal(err)
		}
		ctx, cancel := context.WithTimeout(t.Context(), 30*time.Millisecond)
		_, err := runVNStat(ctx, "eth0", "day")
		cancel()
		if err == nil || !strings.Contains(err.Error(), tc.message) {
			t.Fatal(tc.message, err)
		}
	}
	var output trafficOutput
	if _, err := output.Write(make([]byte, 1024*1024)); err != nil {
		t.Fatal(err)
	}
	if _, err := output.Write([]byte{0}); err == nil || output.Len() != 1024*1024 {
		t.Fatal("output not bounded")
	}
}
