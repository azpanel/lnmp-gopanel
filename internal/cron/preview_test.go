package cron

import (
	"strings"
	"testing"
	"time"
)

func TestNextRuns(t *testing.T) {
	original := time.Local
	time.Local = time.FixedZone("CST", 8*3600)
	t.Cleanup(func() { time.Local = original })
	now := time.Date(2026, 10, 26, 9, 0, 0, 0, time.Local) // Monday
	for _, tc := range []struct {
		name, raw, first string
	}{
		{"strict future", "* * * * * echo ok", "2026-10-26T09:01:00+08:00"},
		{"step", "*/15 * * * * echo ok", "2026-10-26T09:15:00+08:00"},
		{"alias", "@daily echo ok", "2026-10-27T00:00:00+08:00"},
		{"hourly", "@hourly echo ok", "2026-10-26T10:00:00+08:00"},
		{"weekday names", "0 9 * * MON-FRI echo ok", "2026-10-27T09:00:00+08:00"},
		{"sunday zero", "0 0 * * 0 echo ok", "2026-11-01T00:00:00+08:00"},
		{"sunday seven", "0 0 * * 7 echo ok", "2026-11-01T00:00:00+08:00"},
		{"sunday range", "0 0 * * FRI-7/2 echo ok", "2026-10-30T00:00:00+08:00"},
		{"dom dow union", "0 0 27 * 7 echo ok", "2026-10-27T00:00:00+08:00"},
		{"wildcard semantics", "0 0 1 * */7 echo ok", "2026-11-01T00:00:00+08:00"},
		{"leap year", "0 0 29 FEB * echo ok", "2028-02-29T00:00:00+08:00"},
		{"timezone", "CRON_TZ=UTC\n0 9 * * * echo ok", "2026-10-26T09:00:00Z"},
		{"quoted timezone", "CRON_TZ = \"Asia/Tokyo\"\n0 9 * * * echo ok", "2026-10-27T09:00:00+09:00"},
		{"later timezone ignored", "0 10 * * * echo ok\nCRON_TZ=UTC", "2026-10-26T10:00:00+08:00"},
		{"overridden invalid timezone", "CRON_TZ=invalid\nCRON_TZ='UTC'\n0 9 * * * echo ok", "2026-10-26T09:00:00Z"},
		{"timezone reset", "CRON_TZ=UTC\nCRON_TZ=\"\"\n0 10 * * * echo ok", "2026-10-26T10:00:00+08:00"},
		{"shell timezone ignored", "TZ=UTC\n0 10 * * * echo ok", "2026-10-26T10:00:00+08:00"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			view := Parse(tc.raw)
			preview := NextRuns(view, view.Tasks[0], now)
			if preview.Message != "" || len(preview.Times) != 5 || preview.Timezone == "" || preview.Times[0].Format(time.RFC3339) != tc.first {
				t.Fatalf("unexpected preview: %+v", preview)
			}
			previous := now
			for _, next := range preview.Times {
				if !next.After(previous) {
					t.Fatal("occurrences must be strictly increasing")
				}
				previous = next
			}
		})
	}
}

func TestPreviewMessages(t *testing.T) {
	for _, tc := range []struct{ raw, message string }{
		{"@reboot echo ok", "系统启动"},
		{"CRON_TZ=invalid/timezone\n0 0 * * * echo ok", "时区无效"},
		{"99 0 * * * echo ok", "表达式无效"},
		{"0 0 * * 8 echo ok", "表达式无效"},
		{"0 0 * * 5-7/0 echo ok", "表达式无效"},
		{"0 0 31 FEB * echo ok", "没有匹配"},
	} {
		view := Parse(tc.raw)
		preview := NextRuns(view, view.Tasks[0], time.Now())
		if len(preview.Times) != 0 || !strings.Contains(preview.Message, tc.message) {
			t.Fatalf("%s: %+v", tc.raw, preview)
		}
	}
}

func TestDSTPreview(t *testing.T) {
	view := Parse("CRON_TZ=America/New_York\n30 2 * * * echo ok")
	now, _ := time.Parse(time.RFC3339, "2026-03-07T03:00:00-05:00")
	preview := NextRuns(view, view.Tasks[0], now)
	// 02:30 does not exist on the spring-forward day.
	if len(preview.Times) != 5 || preview.Times[0].Format(time.RFC3339) != "2026-03-09T02:30:00-04:00" {
		t.Fatal(preview)
	}
}
