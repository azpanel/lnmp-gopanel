package metrics

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/azpanel/lnmp-gopanel/internal/config"
)

const stubStatus = "Active connections: 0\nserver accepts handled requests\n 1 1 2\nReading: 0 Writing: 0 Waiting: 0\n"

func TestStatusParsersRequireRealValuesAndPreserveZero(t *testing.T) {
	n, err := parseNginx([]byte(stubStatus))
	if err != nil || n.Active != 0 {
		t.Fatal(n, err)
	}
	for _, data := range []string{"", "Active connections: 4", strings.Replace(stubStatus, "0\nserver", "-1\nserver", 1), strings.Replace(stubStatus, "0\nserver", "9999999999999999999999999\nserver", 1)} {
		if _, e := parseNginx([]byte(data)); e == nil {
			t.Fatal("bad nginx response accepted")
		}
	}
	php, err := parsePHPFPM([]byte(`{"active processes":0,"idle processes":2,"listen queue":0,"pool":"www"}`))
	if err != nil || php.Active != 0 || php.Idle != 2 || php.Queue != 0 {
		t.Fatal(php, err)
	}
	for _, data := range []string{`{}`, `null`, `{"active processes":null,"idle processes":0,"listen queue":0}`, `{"active processes":-1,"idle processes":0,"listen queue":0}`, `{"active processes":0,"idle processes":0}`, `<html>login</html>`} {
		if _, e := parsePHPFPM([]byte(data)); e == nil {
			t.Fatal("bad PHP response accepted", data)
		}
	}
}
func TestStatusHTTPIsBoundedAndLocal(t *testing.T) {
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		switch r.URL.Path {
		case "/ok":
			if !r.URL.Query().Has("json") || r.URL.Query().Has("full") {
				t.Error("PHP query mode not enforced")
			}
			io.WriteString(w, `{}`)
		case "/redirect":
			http.Redirect(w, r, "/ok", http.StatusFound)
		case "/large":
			io.WriteString(w, strings.Repeat("x", 65537))
		case "/slow":
			<-r.Context().Done()
		default:
			w.WriteHeader(http.StatusForbidden)
		}
	}))
	defer server.Close()
	if _, err := statusBody(context.Background(), server.URL+"/ok?full", true); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{"/redirect", "/large", "/forbidden"} {
		if _, err := statusBody(context.Background(), server.URL+path, false); err == nil {
			t.Fatal("bad response accepted", path)
		}
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	if _, err := statusBody(ctx, server.URL+"/slow", false); err == nil {
		t.Fatal("timeout ignored")
	}
	before := calls.Load()
	if _, err := statusBody(context.Background(), "http://example.com/status", false); err == nil || calls.Load() != before {
		t.Fatal("non-local probe accepted")
	}
}
func TestOptionalProbesAreDisabledAndFailuresIndependent(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/nginx" {
			io.WriteString(w, stubStatus)
		} else {
			http.Error(w, "no", 500)
		}
	}))
	defer server.Close()
	c := New()
	c.Configure(config.MetricsConfig{NginxStatusURL: server.URL + "/nginx", PHPFPMStatusURL: server.URL + "/php"})
	empty := c.sampleExtra(context.Background(), map[string]bool{}, 1)
	if empty.nginx != nil || len(empty.errors) != 0 {
		t.Fatal("disabled probes ran")
	}
	result := c.sampleExtra(context.Background(), map[string]bool{"nginx": true, "php_fpm": true, "database": true}, 1)
	if result.nginx == nil || result.php != nil || result.database != nil || result.errors["php_fpm"] == "" || result.errors["database"] == "" {
		t.Fatal("probe failures were not isolated", result)
	}
	c.Configure(config.MetricsConfig{})
	result = c.sampleExtra(context.Background(), map[string]bool{"nginx": true, "php_fpm": true}, 1)
	if !strings.Contains(result.errors["nginx"], "metrics.nginx_status_url") || !strings.Contains(result.errors["php_fpm"], "metrics.php_fpm_status_url") {
		t.Fatal("configuration hints missing")
	}
}
func TestProcessRatesAndRanking(t *testing.T) {
	at := time.Now()
	old := processCounter{created: 1, seconds: 10, at: at}
	current := processCounter{created: 1, seconds: 12, at: at.Add(2 * time.Second)}
	if got := processCPU(old, current, 4); got == nil || *got != 25 {
		t.Fatal("CPU not normalized to all cores", got)
	}
	for _, bad := range []processCounter{{created: 2, seconds: 12, at: current.at}, {created: 1, seconds: 9, at: current.at}, {created: 1, seconds: 12, at: at}} {
		if processCPU(old, bad, 4) != nil {
			t.Fatal("PID reuse/reset/zero interval accepted")
		}
	}
	if processCPU(processCounter{}, current, 4) != nil {
		t.Fatal("first observation has a rate")
	}
	all := []Process{}
	for i := 0; i < 15; i++ {
		v := float64(i)
		all = append(all, Process{PID: int32(i), CPU: &v, RSS: uint64(15 - i)})
	}
	ranked := rankProcesses(all)
	if len(ranked.CPU) != 10 || len(ranked.Memory) != 10 || ranked.CPU[0].PID != 14 || ranked.Memory[0].PID != 0 {
		t.Fatal("rankings do not select independent top 10")
	}
	all[0].CPU = nil
	if got := rankProcesses(all); len(got.CPU) != 10 || len(got.Memory) != 10 {
		t.Fatal("warming CPU removed memory data")
	}
	c := New()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, _ = c.sampleProcesses(ctx, 1)
}
func TestMemoryCompositionNeverOverlapsOrUnderflows(t *testing.T) {
	for _, values := range [][4]uint64{{100, 20, 10, 30}, {100, 150, 20, 30}, {100, 20, 100, 100}, {0, 1, 1, 1}} {
		other, free, buffers, cached := MemoryParts(values[0], values[1], values[2], values[3])
		if other+free+buffers+cached != values[0] {
			t.Fatal("segments do not sum to capacity")
		}
	}
}

type statusConnector struct {
	rows [][2]string
	fail bool
}

func (s statusConnector) Connect(context.Context) (driver.Conn, error) { return &statusConn{s: s}, nil }
func (s statusConnector) Driver() driver.Driver                        { return statusDriver{} }

type statusDriver struct{}

func (statusDriver) Open(string) (driver.Conn, error) { return nil, errors.New("unexpected Open") }

type statusConn struct{ s statusConnector }

func (*statusConn) Prepare(string) (driver.Stmt, error) { return nil, errors.New("unexpected Prepare") }
func (*statusConn) Close() error                        { return nil }
func (*statusConn) Begin() (driver.Tx, error)           { return nil, errors.New("write transaction forbidden") }
func (c *statusConn) QueryContext(_ context.Context, q string, args []driver.NamedValue) (driver.Rows, error) {
	if q != "SHOW GLOBAL STATUS WHERE Variable_name IN ('Threads_connected', 'Slow_queries')" || len(args) != 0 {
		return nil, errors.New("unexpected SQL")
	}
	if c.s.fail {
		return nil, errors.New("permission denied")
	}
	return &statusRows{rows: c.s.rows}, nil
}

type statusRows struct {
	rows  [][2]string
	index int
}

func (*statusRows) Columns() []string { return []string{"Variable_name", "Value"} }
func (*statusRows) Close() error      { return nil }
func (r *statusRows) Next(values []driver.Value) error {
	if r.index == len(r.rows) {
		return io.EOF
	}
	values[0], values[1] = r.rows[r.index][0], r.rows[r.index][1]
	r.index++
	return nil
}
func TestDatabaseUsesReadOnlyGlobalCounters(t *testing.T) {
	for _, tc := range []struct {
		rows     [][2]string
		fail, ok bool
	}{
		{rows: [][2]string{{"Threads_connected", "0"}, {"Slow_queries", "7"}}, ok: true},
		{rows: [][2]string{{"Threads_connected", "2"}}},
		{rows: [][2]string{{"Threads_connected", "-1"}, {"Slow_queries", "0"}}},
		{fail: true},
	} {
		db := sql.OpenDB(statusConnector{rows: tc.rows, fail: tc.fail})
		got, err := queryDatabase(context.Background(), db)
		db.Close()
		if (err == nil) != tc.ok {
			t.Fatal(got, err)
		}
		if tc.ok && (got.Connections != 0 || got.SlowQueries != 7) {
			t.Fatal("wrong global counts", got)
		}
	}
}

func TestEnabledWidgetsOnlyAddTheirOwnErrors(t *testing.T) {
	c := New()
	c.SetEnabled([]string{"nginx", "php_fpm", "database"})
	c.sample(context.Background())
	snapshot := c.Latest()
	if snapshot.Memory == nil || len(snapshot.Errors) < 3 || snapshot.Errors["nginx"] == "" || snapshot.Errors["php_fpm"] == "" || snapshot.Errors["database"] == "" {
		t.Fatal("unconfigured optional services affected system sampling", snapshot.Errors)
	}
	c.SetEnabled([]string{})
	c.sample(context.Background())
	snapshot = c.Latest()
	if snapshot.Errors["nginx"] != "" || snapshot.Errors["php_fpm"] != "" || snapshot.Errors["database"] != "" {
		t.Fatal("disabled probes still reported errors")
	}
}
