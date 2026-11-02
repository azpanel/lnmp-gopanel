package server

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"testing"

	"github.com/azpanel/lnmp-gopanel/internal/auth"
	"github.com/azpanel/lnmp-gopanel/internal/backups"
)

type policyTestBackend struct {
	calls  int
	action string
	input  any
	err    error
}

func (b *policyTestBackend) BackupPolicy(_ context.Context, action string, input any) (json.RawMessage, error) {
	b.calls++
	b.action = action
	b.input = input
	if b.err != nil {
		return nil, b.err
	}
	if action == "save" {
		r := input.(backups.SavePolicyRequest)
		r.ID = strings.Repeat("a", 32)
		return json.Marshal(r.Policy)
	}
	if action == "list" {
		return json.RawMessage(`[]`), nil
	}
	if action == "paths" {
		return json.RawMessage(`{"errors":[],"warnings":[]}`), nil
	}
	if action == "files" {
		return json.RawMessage(`{"path":"/srv","type":"directory","valid":true,"complete":false,"original_bytes":100,"included_bytes":50,"error":"部分结果"}`), nil
	}
	return json.RawMessage(`{}`), nil
}
func policyRequestBody() string {
	return `{"name":"网站备份","notes":"","enabled":true,"schedule":"0 2 * * *","compression":{"format":"zip","level":5,"has_password":false},"retention":{"databases":7,"files":7},"databases":{"enabled":true,"mode":"all","excluded":["information_schema","performance_schema","phpmyadmin","sys"],"selected":[],"rules":[]},"files":[],"destinations":[{"type":"local","path":"/var/backups/test"}],"password_action":"replace","password":"secret-password"}`
}
func TestBackupPolicyRoutesAuthenticationValidationAndLimits(t *testing.T) {
	s, _ := testServer(t)
	b := &policyTestBackend{}
	s.BackupPolicies = b
	s.backupScanSlots = make(chan struct{}, 2)
	h := s.Handler()
	id, session := s.Sessions.Create()
	cookie := &http.Cookie{Name: auth.CookieName, Value: id}
	policyID := strings.Repeat("a", 32)
	routes := []struct{ method, path, body string }{{"POST", "/backups/policies/validate-paths", `{"files":[],"destinations":[]}`}, {"GET", "/backups/policies", ""}, {"GET", "/backups/policies/" + policyID, ""}, {"POST", "/backups/policies", policyRequestBody()}, {"PUT", "/backups/policies/" + policyID, policyRequestBody()}, {"DELETE", "/backups/policies/" + policyID, `{"revision":1}`}, {"POST", "/backups/schedule/preview", `{"schedule":"0 2 * * *"}`}, {"POST", "/backups/databases/inspect", `{"password":"temporary"}`}, {"POST", "/backups/databases/tables", `{"database":"app","password":"temporary","page":1}`}, {"POST", "/backups/files/inspect", `{"path":"/srv","excludes":[]}`}}
	for _, r := range routes {
		if w := request(h, r.method, "/api/v1"+r.path, r.body, nil, "", ""); w.Code != 401 {
			t.Fatal(r.path, w.Code)
		}
		if r.method != "GET" {
			if w := request(h, r.method, "/api/v1"+r.path, r.body, cookie, "", ""); w.Code != 403 {
				t.Fatal(r.path, w.Code)
			}
		}
	}
	if b.calls != 0 {
		t.Fatal("unauthorized helper call")
	}
	for _, r := range routes {
		w := request(h, r.method, "/api/v1"+r.path, r.body, cookie, session.CSRF, "")
		want := 200
		if r.method == "POST" && r.path == "/backups/policies" {
			want = 201
		}
		if w.Code != want {
			t.Fatal(r.path, w.Code, w.Body.String())
		}
		if strings.Contains(w.Body.String(), "secret-password") {
			t.Fatal("password echoed")
		}
	}
	body := strings.Replace(policyRequestBody(), `"files":[]`, `"files":[{"path":"/srv","excludes":["`+strings.Repeat("x", 5000)+`"]}]`, 1)
	if w := request(h, "POST", "/api/v1/backups/policies", body, cookie, session.CSRF, ""); w.Code != 400 {
		t.Fatal("long invalid pattern accepted", w.Code)
	}
	// A valid request beyond the legacy 4KiB limit reaches the helper intact.
	patterns := []string{}
	for i := 0; i < 80; i++ {
		patterns = append(patterns, strings.Repeat("x", 70))
	}
	raw, _ := json.Marshal([]backups.FileSource{{Path: "/srv", Excludes: patterns}})
	body = strings.Replace(policyRequestBody(), `"files":[]`, `"files":`+string(raw), 1)
	if w := request(h, "POST", "/api/v1/backups/policies", body, cookie, session.CSRF, ""); w.Code != 201 {
		t.Fatal(w.Code, w.Body.String())
	}
	for _, tc := range []struct {
		err    error
		status int
	}{{backups.ErrInvalid, 400}, {backups.ErrNotFound, 404}, {backups.ErrConflict, 409}, {errors.New("unavailable"), 502}} {
		b.err = tc.err
		if w := request(h, "GET", "/api/v1/backups/policies", "", cookie, "", ""); w.Code != tc.status {
			t.Fatal(w.Code)
		}
	}
	b.err = nil
	before := b.calls
	if w := request(h, "POST", "/api/v1/backups/policies", strings.Replace(policyRequestBody(), `"schedule":"0 2 * * *"`, `"schedule":"@reboot"`, 1), cookie, session.CSRF, ""); w.Code != 400 {
		t.Fatal(w.Code)
	}
	if b.calls != before {
		t.Fatal("invalid policy reached helper")
	}
	s.backupScanSlots <- struct{}{}
	s.backupScanSlots <- struct{}{}
	if w := request(h, "POST", "/api/v1/backups/files/inspect", `{"path":"/srv","excludes":[]}`, cookie, session.CSRF, ""); w.Code != 429 {
		t.Fatal(w.Code)
	}
}
