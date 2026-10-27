package server

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/azpanel/lnmp-gopanel/internal/auth"
	"github.com/azpanel/lnmp-gopanel/internal/vhosts"
)

type authBackend struct {
	fakeVhostBackend
	state vhosts.BasicAuthState
	calls int
}

func (b *authBackend) ReadVhostBasicAuth(context.Context, string) (vhosts.BasicAuthState, error) {
	return b.state, nil
}
func (b *authBackend) SetVhostBasicAuth(ctx context.Context, r vhosts.BasicAuthInput) (vhosts.BasicAuthState, error) {
	b.calls++
	b.state = vhosts.BasicAuthState{Name: r.Name, Status: "enabled", Managed: true, Username: r.Username, SiteEnabled: true}
	return b.state, ctx.Err()
}
func (b *authBackend) DisableVhostBasicAuth(context.Context, string) (vhosts.BasicAuthState, error) {
	b.calls++
	b.state.Status = "disabled"
	b.state.Username = ""
	b.state.Managed = false
	return b.state, nil
}
func TestVhostBasicAuthAPI(t *testing.T) {
	s, _ := testServer(t)
	b := &authBackend{state: vhosts.BasicAuthState{Name: "example.conf", Status: "disabled", SiteEnabled: true}}
	s.Vhosts = vhosts.NewManager(context.Background(), b)
	h := s.Handler()
	const endpoint = "/api/v1/vhosts/basic-auth"
	for _, method := range []string{"GET", "PUT", "DELETE"} {
		if w := request(h, method, endpoint, "", nil, "", ""); w.Code != 401 {
			t.Fatal("API exposed", method, w.Code)
		}
	}
	id, session := s.Sessions.Create()
	cookie := &http.Cookie{Name: auth.CookieName, Value: id}
	for _, method := range []string{"PUT", "DELETE"} {
		if w := request(h, method, endpoint, "", cookie, "", ""); w.Code != 403 {
			t.Fatal("missing CSRF accepted", w.Code)
		}
	}
	if w := request(h, "GET", endpoint+"?name=example.conf", "", cookie, "", ""); w.Code != 200 || !strings.Contains(w.Body.String(), `"status":"disabled"`) {
		t.Fatal(w.Code, w.Body.String())
	}
	for _, body := range []string{
		`{"name":"../example.conf","username":"user","password":"test-password"}`,
		`{"name":"example.conf","username":"u:ser","password":"test-password"}`,
		`{"name":"example.conf","username":"user","password":"short"}`,
		`{"name":"example.conf","username":"user","password":"test-password","extra":true}`,
		`{"name":"example.conf","username":"user","password":"test-password"} {}`,
		`{"name":"example.conf","username":"user","password":"` + strings.Repeat("x", 4096) + `"}`,
	} {
		if w := request(h, "PUT", endpoint, body, cookie, session.CSRF, ""); w.Code != 400 {
			t.Fatal(w.Code, w.Body.String())
		}
	}
	if b.calls != 0 {
		t.Fatal("invalid requests reached backend")
	}
	body := `{"name":"example.conf","username":"user","password":"test-password"}`
	w := request(h, "PUT", endpoint, body, cookie, session.CSRF, "")
	if w.Code != 200 || !strings.Contains(w.Body.String(), `"status":"enabled"`) {
		t.Fatal(w.Code, w.Body.String())
	}
	if strings.Contains(w.Body.String(), "password") || strings.Contains(w.Body.String(), "hash") || strings.Contains(w.Body.String(), "test-password") {
		t.Fatal("secret in response")
	}
	if w.Header().Get("Cache-Control") != "no-store" {
		t.Fatal("auth response cached")
	}
	if w := request(h, "DELETE", endpoint+"?name=example.conf", "", cookie, session.CSRF, ""); w.Code != 200 || !strings.Contains(w.Body.String(), `"status":"disabled"`) {
		t.Fatal(w.Code, w.Body.String())
	}
	// A disconnected browser must not cancel the configuration transaction.
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	req := httptest.NewRequest("PUT", endpoint, strings.NewReader(body)).WithContext(ctx)
	req.AddCookie(cookie)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-CSRF-Token", session.CSRF)
	disconnected := httptest.NewRecorder()
	h.ServeHTTP(disconnected, req)
	if disconnected.Code != 200 {
		t.Fatal("browser cancellation interrupted mutation", disconnected.Code)
	}
	for _, method := range []string{"GET", "DELETE"} {
		if w := request(h, method, endpoint+"?name=../nginx.conf", "", cookie, session.CSRF, ""); w.Code != 400 {
			t.Fatal("unsafe name accepted")
		}
	}
}
