package server

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/azpanel/lnmp-gopanel/internal/auth"
	"github.com/azpanel/lnmp-gopanel/internal/ipgeo"
	"github.com/azpanel/lnmp-gopanel/internal/logs"
)

type stubLogs struct {
	calls int
	err   error
	lines []logs.Line
}

func TestCustomLogEndpoints(t *testing.T) {
	s, _ := testServer(t)
	s.Logs = logs.Store{}
	s.logReads = make(chan struct{}, 1)
	state := filepath.Join(t.TempDir(), "state")
	var err error
	s.CustomLogs, err = logs.OpenRegistry(state)
	if err != nil {
		t.Fatal(err)
	}
	h := s.Handler()
	id, session := s.Sessions.Create()
	cookie := &http.Cookie{Name: auth.CookieName, Value: id}
	path := filepath.Join(t.TempDir(), "custom log.txt")
	if err := os.WriteFile(path, []byte("custom entry\n"), 0600); err != nil {
		t.Fatal(err)
	}
	body := fmt.Sprintf(`{"path":%q}`, path)
	readBody := fmt.Sprintf(`{"path":%q,"mode":"tail","rules":[]}`, path)
	for _, method := range []string{"POST", "DELETE"} {
		if w := request(h, method, "/api/v1/logs/custom", body, nil, "", ""); w.Code != 401 {
			t.Fatal("custom catalog auth bypass", w.Code)
		}
		if w := request(h, method, "/api/v1/logs/custom", body, cookie, "", ""); w.Code != 403 {
			t.Fatal("custom catalog CSRF bypass", w.Code)
		}
		if w := request(h, method, "/api/v1/logs/custom", body, cookie, session.CSRF, "https://evil.example"); w.Code != 403 {
			t.Fatal("custom catalog origin bypass", w.Code)
		}
	}
	for _, bad := range []string{
		`{"path":"/etc/shadow"}`, `{"path":"/root/private.log"}`, `{"path":"/srv/app/.env"}`,
		`{"path":"/srv/app/../app/log"}`, `{"path":"relative.log"}`, `{"path":"/srv/key.pem"}`,
		body + `{}`, body + strings.Repeat(" ", 4096), `{"path":"/srv/app.log","command":"cat"}`,
	} {
		if w := request(h, "POST", "/api/v1/logs/custom", bad, cookie, session.CSRF, ""); w.Code != 400 {
			t.Fatalf("invalid custom log accepted: %d %s", w.Code, w.Body.String())
		}
	}
	if w := request(h, "POST", "/api/v1/logs/read", readBody, cookie, session.CSRF, ""); w.Code != 400 {
		t.Fatal("unregistered custom path readable", w.Code)
	}
	for range 2 {
		w := request(h, "POST", "/api/v1/logs/custom", body, cookie, session.CSRF, "")
		var file logs.File
		if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &file) != nil || !file.Custom || !file.Available || file.Size != 13 {
			t.Fatalf("add: %d %s", w.Code, w.Body.String())
		}
	}
	if len(s.CustomLogs.Paths()) != 1 {
		t.Fatal("duplicate paths stored")
	}
	s.CustomLogs, err = logs.OpenRegistry(state)
	if err != nil {
		t.Fatal(err)
	}
	w := request(h, "POST", "/api/v1/logs/read", readBody, cookie, session.CSRF, "")
	var window logs.Window
	if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &window) != nil || len(window.Lines) != 1 || window.Lines[0].Text != "custom entry" {
		t.Fatalf("custom read after reload: %d %s", w.Code, w.Body.String())
	}
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	w = request(h, "GET", "/api/v1/logs", "", cookie, "", "")
	var catalog logs.Catalog
	if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &catalog) != nil {
		t.Fatalf("catalog: %d %s", w.Code, w.Body.String())
	}
	found := false
	for _, file := range catalog.Files {
		if file.Path == path {
			found = true
			if !file.Custom || file.Available || file.Error == "" {
				t.Fatal("missing custom log not represented", file)
			}
		}
	}
	if !found {
		t.Fatal("custom log lost when file disappeared")
	}
	if err := os.WriteFile(path, []byte("retained file\n"), 0600); err != nil {
		t.Fatal(err)
	}
	for range 2 {
		if w := request(h, "DELETE", "/api/v1/logs/custom", body, cookie, session.CSRF, ""); w.Code != 204 {
			t.Fatal("remove failed", w.Code, w.Body.String())
		}
	}
	if data, err := os.ReadFile(path); err != nil || string(data) != "retained file\n" {
		t.Fatal("removing catalog entry touched original file", err)
	}
	if w := request(h, "POST", "/api/v1/logs/read", readBody, cookie, session.CSRF, ""); w.Code != 400 {
		t.Fatal("removed custom path still readable", w.Code)
	}
}

func TestLegacyCustomLogsStayVisibleButCannotBeRead(t *testing.T) {
	s, _ := testServer(t)
	reader := &stubLogs{}
	s.Logs = reader
	s.logReads = make(chan struct{}, 1)
	dir := filepath.Join(t.TempDir(), "state")
	if err := os.Mkdir(dir, 0700); err != nil {
		t.Fatal(err)
	}
	paths := []string{"/var/log/syslog", "/srv/database.php", "/srv/app.log.1", "/srv/app.txt"}
	data, err := json.Marshal(paths)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "custom-logs.json"), data, 0600); err != nil {
		t.Fatal(err)
	}
	s.CustomLogs, err = logs.OpenRegistry(dir)
	if err != nil {
		t.Fatal(err)
	}
	h := s.Handler()
	id, session := s.Sessions.Create()
	cookie := &http.Cookie{Name: auth.CookieName, Value: id}
	w := request(h, "GET", "/api/v1/logs", "", cookie, "", "")
	var catalog logs.Catalog
	if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &catalog) != nil || len(catalog.Files) != len(paths) {
		t.Fatalf("legacy catalog failed: %d %s", w.Code, w.Body.String())
	}
	// Only the built-in catalog and the compliant custom file reach the backend.
	if reader.calls != 2 {
		t.Fatal("legacy metadata reached backend", reader.calls)
	}
	for i, file := range catalog.Files {
		if !file.Custom || file.Path != paths[i] {
			t.Fatal("legacy entry lost", file)
		}
		if i < 3 && (file.Available || !strings.Contains(file.Error, ".log 或 .txt")) {
			t.Fatal("legacy entry not marked unreadable", file)
		}
		if i == 3 && !file.Available {
			t.Fatal("compliant entry unavailable", file)
		}
	}
	for _, path := range paths[:3] {
		before := reader.calls
		body := fmt.Sprintf(`{"path":%q}`, path)
		for _, endpoint := range []struct{ path, body string }{
			{"/api/v1/logs/custom", body},
			{"/api/v1/logs/read", fmt.Sprintf(`{"path":%q,"mode":"tail"}`, path)},
		} {
			w := request(h, "POST", endpoint.path, endpoint.body, cookie, session.CSRF, "")
			if w.Code != 400 || !strings.Contains(w.Body.String(), ".log 或 .txt") {
				t.Fatalf("legacy path accepted: %d %s", w.Code, w.Body.String())
			}
		}
		if reader.calls != before {
			t.Fatal("rejected path reached backend")
		}
		if w := request(h, "DELETE", "/api/v1/logs/custom", body, cookie, session.CSRF, ""); w.Code != 204 {
			t.Fatalf("legacy removal failed: %d %s", w.Code, w.Body.String())
		}
	}
	reloaded, err := logs.OpenRegistry(dir)
	if err != nil || len(reloaded.Paths()) != 1 || !reloaded.Contains(paths[3]) {
		t.Fatal("legacy removal did not persist", err)
	}
}

func (s *stubLogs) List(context.Context) (logs.Catalog, error) {
	s.calls++
	return logs.Catalog{Files: []logs.File{}, Warnings: []string{}}, s.err
}
func (s *stubLogs) Read(_ context.Context, req logs.Request) (logs.Window, error) {
	s.calls++
	if s.lines != nil {
		return logs.Window{Lines: append([]logs.Line(nil), s.lines...)}, s.err
	}
	return logs.Window{Lines: []logs.Line{{Offset: "0", Text: "hello"}}, Start: "start", End: "end", From: "0", To: "6"}, s.err
}

func TestLogLocations(t *testing.T) {
	s, _ := testServer(t)
	s.Logs = &stubLogs{lines: []logs.Line{
		{Offset: "0", Text: `114.114.114.114 - - [27/Oct/2026:14:23:45 +0800] "GET / HTTP/1.1" 200 123`},
		{Offset: "100", Text: "114.114.114.114\t- - repeated"},
		{Offset: "200", Text: "2001:db8::1 - - request"},
		{Offset: "300", Text: "unrecognized access log"},
		{Offset: "400", Text: "bad\"ip - - request"},
		{Offset: "500", Text: "fe80::1%eth0 - - request"},
	}}
	s.logReads = make(chan struct{}, 1)
	id, session := s.Sessions.Create()
	cookie := &http.Cookie{Name: auth.CookieName, Value: id}
	h := s.Handler()
	for _, path := range []string{logs.Directory + "/access.log", logs.ManagedDirectory + "/access.log", logs.PHP, logs.MySQL} {
		body := fmt.Sprintf(`{"path":%q,"mode":"tail","rules":[]}`, path)
		w := request(h, "POST", "/api/v1/logs/read", body, cookie, session.CSRF, "")
		var out logs.Window
		if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &out) != nil || len(out.Lines) != 6 {
			t.Fatalf("read %s: %d %s", path, w.Code, w.Body.String())
		}
		for i, line := range out.Lines {
			want := ""
			if path != logs.PHP && path != logs.MySQL {
				if i < 2 {
					want = ipgeo.Lookup("114.114.114.114")
				}
				if i == 2 {
					want = "暂不支持"
				}
			}
			if line.IPLocation != want {
				t.Errorf("%s line %d: got %q; want %q", path, i, line.IPLocation, want)
			}
		}
	}
}
func TestLogEndpoints(t *testing.T) {
	s, _ := testServer(t)
	reader := &stubLogs{}
	s.Logs = reader
	s.logReads = make(chan struct{}, 1)
	h := s.Handler()
	body := `{"path":"/home/wwwlogs/a.log","mode":"tail","rules":[]}`
	for _, endpoint := range []struct{ method, path string }{{"GET", "/api/v1/logs"}, {"POST", "/api/v1/logs/read"}} {
		if w := request(h, endpoint.method, endpoint.path, body, nil, "", ""); w.Code != 401 {
			t.Fatal("auth bypass", w.Code)
		}
	}
	id, session := s.Sessions.Create()
	cookie := &http.Cookie{Name: auth.CookieName, Value: id}
	if w := request(h, "POST", "/api/v1/logs/read", body, cookie, "", ""); w.Code != 403 || reader.calls != 0 {
		t.Fatal("CSRF bypass")
	}
	if w := request(h, "POST", "/api/v1/logs/read", body, cookie, session.CSRF, "https://evil.example"); w.Code != 403 {
		t.Fatal("origin bypass")
	}
	for _, bad := range []string{
		`{"path":"/etc/shadow","mode":"tail"}`,
		`{"path":"/home/wwwlogs/../secret.log","mode":"tail"}`,
		`{"path":"/home/wwwlogs/a.log","mode":"after","end":"bad"}`,
		`{"path":"/home/wwwlogs/a.log","mode":"tail","command":"cat"}`,
		`{"path":"/home/wwwlogs/a.log","mode":"tail","rules":[{"kind":"regex","action":"hide","enabled":true,"pattern":"["}]}`,
		body + `{}`, body + strings.Repeat(" ", 4096),
	} {
		if w := request(h, "POST", "/api/v1/logs/read", bad, cookie, session.CSRF, ""); w.Code != 400 || reader.calls != 0 {
			t.Fatalf("unsafe input reached reader: %d %s", w.Code, w.Body.String())
		}
	}
	w := request(h, "GET", "/api/v1/logs", "", cookie, "", "")
	if w.Code != 200 || w.Header().Get("Cache-Control") != "no-store" {
		t.Fatal("list failed")
	}
	w = request(h, "POST", "/api/v1/logs/read", body, cookie, session.CSRF, "")
	var out logs.Window
	if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &out) != nil || out.Lines[0].Text != "hello" {
		t.Fatal("read failed")
	}
	for _, tc := range []struct {
		err  error
		code int
	}{{logs.ErrChanged, 409}, {logs.ErrInvalid, 400}, {errors.New("unavailable"), 503}} {
		reader.err = tc.err
		if w := request(h, "POST", "/api/v1/logs/read", body, cookie, session.CSRF, ""); w.Code != tc.code {
			t.Fatal("error code", w.Code)
		}
	}
	s.logReads <- struct{}{}
	if w := request(h, "GET", "/api/v1/logs", "", cookie, "", ""); w.Code != 429 {
		t.Fatal("concurrency bound failed")
	}
	<-s.logReads
	s.Logs = nil
	if w := request(h, "GET", "/api/v1/logs", "", cookie, "", ""); w.Code != 503 {
		t.Fatal("nil reader")
	}
}
