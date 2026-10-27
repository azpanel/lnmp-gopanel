package server

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
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
