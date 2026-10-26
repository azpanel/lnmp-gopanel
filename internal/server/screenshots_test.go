package server

import (
	"bytes"
	"context"
	"image"
	"image/jpeg"
	"net/http"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/azpanel/lnmp-gopanel/internal/auth"
	"github.com/azpanel/lnmp-gopanel/internal/screenshots"
	"github.com/azpanel/lnmp-gopanel/internal/vhosts"
)

type screenshotRenderer struct{}

func (screenshotRenderer) Available() error { return nil }
func (screenshotRenderer) Capture(context.Context, screenshots.Target) ([]byte, error) {
	var b bytes.Buffer
	_ = jpeg.Encode(&b, image.NewRGBA(image.Rect(0, 0, 1440, 900)), nil)
	return b.Bytes(), nil
}
func TestScreenshotAPIsAuthenticationPersistenceAndImages(t *testing.T) {
	s, _ := testServer(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	s.Vhosts = vhosts.NewManager(ctx, &fakeVhostBackend{})
	s.Vhosts.Refresh()
	deadline := time.Now().Add(time.Second)
	for len(s.Vhosts.Latest().Sites) == 0 && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	dir := t.TempDir()
	if err := os.Chmod(dir, 0700); err != nil {
		t.Fatal(err)
	}
	var err error
	s.Screenshots, err = screenshots.NewManager(ctx, dir, screenshotRenderer{})
	if err != nil {
		t.Fatal(err)
	}
	h := s.Handler()
	id, session := s.Sessions.Create()
	cookie := &http.Cookie{Name: auth.CookieName, Value: id}
	base := "/api/v1/vhosts/screenshot"
	for _, endpoint := range []struct{ method, path string }{{"GET", base + "s"}, {"GET", base + "/image?name=example.conf"}, {"PUT", base + "?name=example.conf"}, {"POST", base + "/refresh?name=example.conf"}, {"DELETE", base + "?name=example.conf"}} {
		if w := request(h, endpoint.method, endpoint.path, "{}", nil, "", ""); w.Code != 401 {
			t.Fatal("exposed screenshot endpoint", endpoint, w.Code)
		}
		if endpoint.method != "GET" {
			if w := request(h, endpoint.method, endpoint.path, "{}", cookie, "", ""); w.Code != 403 {
				t.Fatal("missing CSRF", w.Code)
			}
			if w := request(h, endpoint.method, endpoint.path, "{}", cookie, session.CSRF, "https://other.example"); w.Code != 403 {
				t.Fatal("cross-origin write", w.Code)
			}
		}
	}
	for _, name := range []string{"../bad.conf", "missing.conf"} {
		w := request(h, "PUT", base+"?name="+name, `{"url":"http://localhost/"}`, cookie, session.CSRF, "")
		if w.Code != 400 && w.Code != 404 {
			t.Fatal("invalid site accepted", w.Code)
		}
	}
	w := request(h, "PUT", base+"?name=example.conf", `{"url":"http://localhost/private","auth_enabled":true,"username":"site-user","password":"private-screenshot-password"}`, cookie, session.CSRF, "")
	if w.Code != 202 || strings.Contains(w.Body.String(), "private-screenshot-password") || strings.Contains(w.Body.String(), "password_cipher") {
		t.Fatal("save failed or leaked credentials", w.Code, w.Body.String())
	}
	deadline = time.Now().Add(3 * time.Second)
	for s.Screenshots.Latest().Items["example.conf"].Status != "ready" && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}
	w = request(h, "GET", base+"s", "", cookie, "", "")
	if w.Code != 200 || !strings.Contains(w.Body.String(), `"status":"ready"`) || strings.Contains(w.Body.String(), "private-screenshot-password") {
		t.Fatal("invalid metadata", w.Body.String())
	}
	w = request(h, "GET", base+"/image?name=example.conf", "", cookie, "", "")
	if w.Code != 200 || w.Header().Get("Content-Type") != "image/jpeg" || w.Header().Get("Cache-Control") != "no-store" {
		t.Fatal("image response", w.Code, w.Header())
	}
	if _, err := jpeg.DecodeConfig(w.Body); err != nil {
		t.Fatal(err)
	}
	w = request(h, "POST", base+"/refresh?name=example.conf", "", cookie, session.CSRF, "")
	if w.Code != 202 {
		t.Fatal(w.Body.String())
	}
	w = request(h, "DELETE", base+"?name=example.conf", "", cookie, session.CSRF, "")
	if w.Code != 204 {
		t.Fatal(w.Body.String())
	}
	if w := request(h, "GET", base+"/image?name=example.conf", "", cookie, "", ""); w.Code != 404 {
		t.Fatal("deleted image still served")
	}
}
