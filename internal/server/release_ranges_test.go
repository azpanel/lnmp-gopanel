package server

import (
	"context"
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/azpanel/lnmp-gopanel/internal/auth"
	"github.com/azpanel/lnmp-gopanel/internal/control"
	"github.com/azpanel/lnmp-gopanel/internal/releases"
)

func TestReleaseRangeAPIPersistsAndRequiresAuthCSRF(t *testing.T) {
	s, _ := testServer(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	source := &releaseSourceStub{done: make(chan struct{})}
	s.Releases = releases.NewManager(ctx, source)
	dir := t.TempDir()
	if err := os.Chmod(dir, 0700); err != nil {
		t.Fatal(err)
	}
	if err := s.Releases.ConfigureRanges(dir); err != nil {
		t.Fatal(err)
	}
	h := s.Handler()
	path := "/api/v1/services/releases/nginx/range"
	if w := request(h, "PUT", path, `{"range":"1.28"}`, nil, "", ""); w.Code != 401 {
		t.Fatal(w.Code, w.Body.String())
	}
	id, session := s.Sessions.Create()
	cookie := &http.Cookie{Name: auth.CookieName, Value: id}
	if w := request(h, "PUT", path, `{"range":"1.28"}`, cookie, "", ""); w.Code != 403 {
		t.Fatal(w.Code, w.Body.String())
	}
	if source.calls.Load() != 0 {
		t.Fatal("unauthorized fetch")
	}
	for _, body := range []string{`{}`, `{"range":null}`, `{"range":8}`, `{"range":"8.3.1"}`, `{"range":"8.*"}`, `{"range":"08"}`, `{"range":"1.28","extra":true}`, `{"range":"1.28"} {}`} {
		if w := request(h, "PUT", path, body, cookie, session.CSRF, ""); w.Code != 400 {
			t.Fatalf("accepted %s: %d %s", body, w.Code, w.Body.String())
		}
	}
	for _, name := range []string{"mysql", "redis", "php-fpm", "mariadb", "unknown"} {
		if w := request(h, "PUT", "/api/v1/services/releases/"+name+"/range", `{"range":"1"}`, cookie, session.CSRF, ""); w.Code != 400 {
			t.Fatal("unconfigured or unsupported service accepted", name, w.Code)
		}
	}
	for _, scope := range []string{"1", "1.28", ""} {
		w := request(h, "PUT", path, `{"range":"`+scope+`"}`, cookie, session.CSRF, "")
		var got releases.Release
		if w.Code != 202 || json.Unmarshal(w.Body.Bytes(), &got) != nil || got.Range != scope || got.Status != "loading" {
			t.Fatalf("bad async response: %d %s", w.Code, w.Body.String())
		}
		w = request(h, "GET", "/api/v1/services/releases", "", cookie, "", "")
		var all map[string]releases.Release
		if json.Unmarshal(w.Body.Bytes(), &all) != nil || all["nginx"].Range != scope {
			t.Fatalf("GET lost range: %s", w.Body.String())
		}
	}
	restarted := releases.NewManager(ctx, source)
	if err := restarted.ConfigureRanges(dir); err != nil {
		t.Fatal(err)
	}
	if got := restarted.Latest([]string{"nginx"}, false)["nginx"]; got.Range != "" {
		t.Fatal("reset did not persist", got)
	}
	// Make the existing state file unsafe; failed writes must retain the applied range.
	if err := os.Chmod(filepath.Join(dir, "service-release-ranges.json"), 0644); err != nil {
		t.Fatal(err)
	}
	w := request(h, "PUT", path, `{"range":"1.26"}`, cookie, session.CSRF, "")
	if w.Code != 503 || !strings.Contains(w.Body.String(), "版本范围配置不可用") {
		t.Fatal(w.Code, w.Body.String())
	}
	if got := s.Releases.Latest([]string{"nginx"}, false)["nginx"]; got.Range != "" {
		t.Fatal("failed write applied", got)
	}
}

func TestComponentRangeAPIsWorkWithoutConfiguredServices(t *testing.T) {
	s, _ := testServer(t)
	s.Services = control.New(nil, &stubExecutor{})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	s.Releases = releases.NewManager(ctx, &releaseSourceStub{done: make(chan struct{})})
	dir := filepath.Join(t.TempDir(), "state")
	if err := s.Releases.ConfigureRanges(dir); err != nil {
		t.Fatal(err)
	}
	h := s.Handler()
	id, session := s.Sessions.Create()
	cookie := &http.Cookie{Name: auth.CookieName, Value: id}
	for _, tc := range []struct{ name, major, minor string }{{"node", "22", "22.23"}, {"python3", "3", "3.13"}} {
		path := "/api/v1/services/releases/" + tc.name + "/range"
		if w := request(h, "PUT", path, `{"range":"`+tc.major+`"}`, nil, "", ""); w.Code != 401 {
			t.Fatal("component ranges exposed without auth", w.Code)
		}
		if w := request(h, "PUT", path, `{"range":"`+tc.major+`"}`, cookie, "", ""); w.Code != 403 {
			t.Fatal("component range bypassed CSRF", w.Code)
		}
		for _, scope := range []string{tc.major, "", tc.minor} {
			w := request(h, "PUT", path, `{"range":"`+scope+`"}`, cookie, session.CSRF, "")
			var got releases.Release
			if w.Code != 202 || json.Unmarshal(w.Body.Bytes(), &got) != nil || got.Range != scope || got.Status != "loading" {
				t.Fatalf("component range rejected: %d %s", w.Code, w.Body.String())
			}
			w = request(h, "GET", "/api/v1/services/releases", "", cookie, "", "")
			var all map[string]releases.Release
			if json.Unmarshal(w.Body.Bytes(), &all) != nil || all[tc.name].Range != scope {
				t.Fatal("component GET lost range", w.Body.String())
			}
		}
	}
	for _, name := range []string{"go", "composer", "codex", "phpmyadmin"} {
		w := request(h, "PUT", "/api/v1/services/releases/"+name+"/range", `{"range":"3"}`, cookie, session.CSRF, "")
		if w.Code != 400 {
			t.Fatal("unsupported component accepted", name, w.Code)
		}
	}
	restored := releases.NewManager(ctx, &releaseSourceStub{done: make(chan struct{})})
	if err := restored.ConfigureRanges(dir); err != nil {
		t.Fatal(err)
	}
	got := restored.Latest([]string{"node", "python3"}, false)
	if got["node"].Range != "22.23" || got["python3"].Range != "3.13" {
		t.Fatal("restart lost component ranges", got)
	}
}
