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

type formatBackend struct {
	fakeVhostBackend
	calls int
}

func (b *formatBackend) FormatVhost(ctx context.Context, input vhosts.FormatInput) (vhosts.FormatResult, error) {
	b.calls++
	view := vhosts.ConfigView{Filename: input.Name, Content: "server {\n    listen 80;\n}\n"}
	view.Revision = vhosts.ConfigRevision(view)
	return vhosts.FormatResult{ConfigView: view, Changed: true, Message: "已格式化"}, ctx.Err()
}

func TestVhostFormatAPIRequiresAuthCSRFAndRevision(t *testing.T) {
	s, _ := testServer(t)
	b := &formatBackend{}
	s.Vhosts = vhosts.NewManager(context.Background(), b)
	h := s.Handler()
	const endpoint = "/api/v1/vhosts/config/format"
	body := `{"name":"example.conf","revision":"` + strings.Repeat("a", 64) + `"}`
	if w := request(h, "POST", endpoint, body, nil, "", ""); w.Code != 401 {
		t.Fatal("format exposed without authentication", w.Code)
	}
	id, session := s.Sessions.Create()
	cookie := &http.Cookie{Name: auth.CookieName, Value: id}
	if w := request(h, "POST", endpoint, body, cookie, "", ""); w.Code != 403 {
		t.Fatal("format accepted without CSRF", w.Code)
	}
	for _, invalid := range []string{
		`{"name":"../nginx.conf","revision":"` + strings.Repeat("a", 64) + `"}`,
		`{"name":"example.conf"}`,
		`{"name":"example.conf","revision":"bad"}`,
		strings.TrimSuffix(body, "}") + `,"content":"arbitrary config"}`,
		body + " {}",
	} {
		if w := request(h, "POST", endpoint, invalid, cookie, session.CSRF, ""); w.Code != 400 {
			t.Fatal("invalid request accepted", w.Code, w.Body.String())
		}
	}
	if b.calls != 0 {
		t.Fatal("invalid request reached privileged backend")
	}
	if w := request(h, "POST", endpoint, body, cookie, session.CSRF, ""); w.Code != 200 || !strings.Contains(w.Body.String(), `"revision"`) || !strings.Contains(w.Body.String(), `"changed":true`) {
		t.Fatal(w.Code, w.Body.String())
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	r := httptest.NewRequest("POST", endpoint, strings.NewReader(body)).WithContext(ctx)
	r.AddCookie(cookie)
	r.Header.Set("Content-Type", "application/json")
	r.Header.Set("X-CSRF-Token", session.CSRF)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != 200 || b.calls != 2 {
		t.Fatal("client disconnect interrupted formatting", w.Code)
	}
}
