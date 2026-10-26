package screenshots

import (
	"bytes"
	"context"
	"image/jpeg"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestChromiumIntegration(t *testing.T) {
	path := os.Getenv("SCREENSHOT_TEST_BROWSER")
	if path == "" {
		t.Skip("set SCREENSHOT_TEST_BROWSER to run real Chromium integration tests")
	}
	dir := t.TempDir()
	c := &Chromium{Browser: path, StateDir: dir, testNoSandbox: os.Geteuid() == 0}
	var leaked atomic.Bool
	other := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "" {
			leaked.Store(true)
		}
		w.Header().Set("WWW-Authenticate", `Basic realm="other"`)
		w.WriteHeader(401)
	}))
	defer other.Close()
	var authenticated atomic.Bool
	site := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/slow" {
			<-r.Context().Done()
			return
		}
		if r.URL.Path != "/plain" {
			u, p, ok := r.BasicAuth()
			if !ok || u != "site-user" || p != "test-private-password" {
				w.Header().Set("WWW-Authenticate", `Basic realm="site"`)
				w.WriteHeader(401)
				return
			}
			authenticated.Store(true)
		}
		if r.URL.Path == "/redirect" {
			http.Redirect(w, r, other.URL, http.StatusFound)
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write([]byte(`<!doctype html><html><body style="background:#ff0000"><h1>网站截图测试</h1><iframe src="` + other.URL + `"></iframe></body></html>`))
	}))
	defer site.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	for _, path := range []string{"/plain", "/private"} {
		t.Run(path, func(t *testing.T) {
			image, err := c.Capture(ctx, Target{URL: site.URL + path, AuthEnabled: path != "/plain", Username: "site-user", Password: "test-private-password"})
			if err != nil {
				t.Fatal(err)
			}
			decoded, err := jpeg.Decode(bytes.NewReader(image))
			if err != nil || decoded.Bounds().Dx() != 1440 || decoded.Bounds().Dy() != 900 {
				t.Fatal("invalid screenshot", err)
			}
			r, g, b, _ := decoded.At(1430, 890).RGBA()
			if r < 50000 || g > 10000 || b > 10000 {
				t.Fatal("did not capture the website background")
			}
		})
	}
	if !authenticated.Load() {
		t.Fatal("Basic Auth was never supplied")
	}
	for _, target := range []Target{{URL: site.URL + "/private", AuthEnabled: true, Username: "site-user", Password: "bad-private-password"}, {URL: site.URL + "/redirect", AuthEnabled: true, Username: "site-user", Password: "test-private-password"}} {
		if _, err := c.Capture(ctx, target); err == nil || !strings.Contains(err.Error(), "认证失败") {
			t.Fatalf("expected auth failure, got %v", err)
		}
	}
	if leaked.Load() {
		t.Fatal("credentials leaked to another origin")
	}
	tlsSite := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write([]byte("test")) }))
	defer tlsSite.Close()
	if _, err := c.Capture(ctx, Target{URL: tlsSite.URL}); err == nil || !strings.Contains(err.Error(), "证书") {
		t.Fatal("TLS failure not explained", err)
	}
	short, done := context.WithTimeout(ctx, 600*time.Millisecond)
	_, err := c.Capture(short, Target{URL: site.URL + "/slow"})
	done()
	if err == nil || !strings.Contains(err.Error(), "超时") {
		t.Fatal("timeout not handled", err)
	}
	files, err := filepath.Glob(filepath.Join(dir, ".browser-*"))
	if err != nil || len(files) != 0 {
		t.Fatal("browser profiles leaked", files, err)
	}
}
func TestBrowserErrorDoesNotExposeSecrets(t *testing.T) {
	err := browserError(context.Background(), &urlError{"https://secret:password@example.com/?token=private"})
	if strings.Contains(err.Error(), "password") || strings.Contains(err.Error(), "token") {
		t.Fatal("raw browser error leaked")
	}
}

type urlError struct{ value string }

func (e *urlError) Error() string { return e.value }
