package server

import (
	"github.com/azpanel/lnmp-gopanel/internal/auth"
	"github.com/azpanel/lnmp-gopanel/internal/backups"
	"net/http"
	"strings"
	"testing"
)

func TestBackupRunValidationAndBackendContracts(t *testing.T) {
	s, _ := testServer(t)
	b := &policyTestBackend{}
	s.BackupPolicies = b
	h := s.Handler()
	id, session := s.Sessions.Create()
	cookie := &http.Cookie{Name: auth.CookieName, Value: id}
	policy := strings.Repeat("a", 32)
	for _, tc := range []struct{ method, path, body string }{
		{"POST", "/backups/policies/" + policy + "/run", `{}`},
		{"POST", "/backups/policies/" + policy + "/run", `{"revision":0}`},
		{"POST", "/backups/policies/" + policy + "/run", `{"revision":1,"password":"secret"}`},
		{"POST", "/backups/policies/" + policy + "/run", `{"revision":1} {}`},
		{"POST", "/backups/policies/" + policy + "/run", `{"revision":1,"id":"other"}`},
		{"POST", "/backups/policies/invalid/run", `{"revision":1}`},
		{"GET", "/backups/policies/" + policy + "/runs?page=0", ""},
		{"GET", "/backups/policies/" + policy + "/runs?page=-1", ""},
		{"GET", "/backups/policies/" + policy + "/runs?page=abc", ""},
		{"GET", "/backups/policies/invalid/runs", ""},
		{"GET", "/backups/runs/invalid", ""},
	} {
		if w := request(h, tc.method, "/api/v1"+tc.path, tc.body, cookie, session.CSRF, ""); w.Code != 400 {
			t.Fatal(tc, w.Code, w.Body.String())
		}
	}
	if b.calls != 0 {
		t.Fatal("invalid request reached privileged helper", b.calls)
	}
	if w := request(h, "POST", "/api/v1/backups/policies/"+policy+"/run", `{"revision":5}`, cookie, session.CSRF, ""); w.Code != 202 || b.action != "run" || b.input.(backups.PolicyIdentity).Revision != 5 {
		t.Fatal(w.Code, b)
	}
	if w := request(h, "GET", "/api/v1/backups/policies/"+policy+"/runs?page=3", "", cookie, "", ""); w.Code != 200 || b.action != "runs" || b.input.(backups.RunQuery).Page != 3 {
		t.Fatal(w.Code, b)
	}
	if w := request(h, "GET", "/api/v1/backups/runs/"+policy, "", cookie, "", ""); w.Code != 200 || b.action != "run-get" || b.input.(backups.RunQuery).ID != policy {
		t.Fatal(w.Code, b)
	}
}
