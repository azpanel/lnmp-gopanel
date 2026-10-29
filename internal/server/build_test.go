package server

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/azpanel/lnmp-gopanel/internal/auth"
	"github.com/azpanel/lnmp-gopanel/internal/buildinfo"
)

func TestBuildInfo(t *testing.T) {
	version, builtAt := buildinfo.Version, buildinfo.BuiltAt
	t.Cleanup(func() { buildinfo.Version, buildinfo.BuiltAt = version, builtAt })
	s, _ := testServer(t)
	h := s.Handler()
	if w := request(h, "GET", "/api/v1/build", "", nil, "", ""); w.Code != http.StatusUnauthorized {
		t.Fatalf("anonymous build info: status=%d", w.Code)
	}
	id, _ := s.Sessions.Create()
	cookie := &http.Cookie{Name: auth.CookieName, Value: id}
	for _, want := range []buildinfo.Info{
		{Version: "v0.2.0", BuiltAt: "2026-10-29T01:45:00Z"},
		{Version: "dev-a1b2c3d", BuiltAt: "2026-10-29T02:00:00Z"},
		{Version: "dev", BuiltAt: ""},
	} {
		buildinfo.Version, buildinfo.BuiltAt = want.Version, want.BuiltAt
		w := request(h, "GET", "/api/v1/build", "", cookie, "", "")
		if w.Code != http.StatusOK || w.Header().Get("Cache-Control") != "no-store" {
			t.Fatalf("build info: status=%d cache=%q", w.Code, w.Header().Get("Cache-Control"))
		}
		var got buildinfo.Info
		if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil || got != want {
			t.Fatalf("build info=%+v, want=%+v, error=%v", got, want, err)
		}
	}
}
