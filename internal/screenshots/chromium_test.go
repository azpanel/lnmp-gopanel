package screenshots

import (
	"bytes"
	"context"
	"fmt"
	"image/jpeg"
	"math"
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

func TestChromiumIntegrationLanguageAndDelayedCSS(t *testing.T) {
	path := os.Getenv("SCREENSHOT_TEST_BROWSER")
	if path == "" {
		t.Skip("set SCREENSHOT_TEST_BROWSER to run real Chromium integration tests")
	}
	c := &Chromium{Browser: path, StateDir: t.TempDir(), testNoSandbox: os.Geteuid() == 0}
	var correctLanguage atomic.Bool
	site := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/slow.css" {
			select {
			case <-r.Context().Done():
				return
			case <-time.After(3 * time.Second):
			}
			w.Header().Set("Content-Type", "text/css")
			_, _ = w.Write([]byte("body[data-language=correct] { background: #00ff00; }"))
			return
		}
		if r.URL.Path == "/never.css" {
			<-r.Context().Done()
			return
		}
		if r.URL.Path == "/timeout" {
			w.Header().Set("Content-Type", "text/html")
			_, _ = w.Write([]byte(`<!doctype html><html><head><link rel="stylesheet" href="/never.css"></head><body>Loading</body></html>`))
			return
		}
		correctLanguage.Store(strings.HasPrefix(r.Header.Get("Accept-Language"), "en-US"))
		w.Header().Set("Content-Type", "text/html")
		// The stylesheet is requested after load and finishes later than the
		// previous fixed two-second sleep would have captured it.
		_, _ = w.Write([]byte(`<!doctype html><html><head><style>body { background: #ff0000; }</style></head><body><script>
		if (navigator.language === "en-US" && Intl.DateTimeFormat().resolvedOptions().locale === "en-US") {
			document.body.dataset.language = "correct";
		}
		window.addEventListener("load", () => setTimeout(() => {
			const css = document.createElement("link");
			css.rel = "stylesheet"; css.href = "/slow.css";
			document.head.appendChild(css);
		}, 500));
		</script></body></html>`))
	}))
	defer site.Close()
	image, err := c.Capture(context.Background(), Target{URL: site.URL, Language: "en-US", LoadTimeoutSeconds: 15})
	if err != nil {
		t.Fatal(err)
	}
	decoded, err := jpeg.Decode(bytes.NewReader(image))
	if err != nil {
		t.Fatal(err)
	}
	r, g, b, _ := decoded.At(1430, 890).RGBA()
	if !correctLanguage.Load() || r > 10000 || g < 50000 || b > 10000 {
		t.Fatal("language overrides or delayed CSS were not applied")
	}
	started := time.Now()
	_, err = c.Capture(context.Background(), Target{URL: site.URL + "/timeout", LoadTimeoutSeconds: 5})
	if err == nil || !strings.Contains(err.Error(), "超时") || time.Since(started) > 8*time.Second {
		t.Fatal("configured resource loading timeout was not enforced", err)
	}
}

func TestChromiumIntegrationZoom(t *testing.T) {
	path := os.Getenv("SCREENSHOT_TEST_BROWSER")
	if path == "" {
		t.Skip("set SCREENSHOT_TEST_BROWSER to run real Chromium integration tests")
	}
	c := &Chromium{Browser: path, StateDir: t.TempDir(), testNoSandbox: os.Geteuid() == 0}
	site := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		_, _ = w.Write([]byte(`<!doctype html><html><head><style>
			body { margin: 0; min-height: 20000px; background: white; }
			#box { width: 100px; height: 100px; background: red; }
			@media (min-width: 1600px) { #box { background: lime; } }
			#fixed { position: fixed; right: 0; bottom: 0; width: 100px; height: 100px; background: blue; }
		</style></head><body><div id="box"></div><div id="fixed"></div></body></html>`))
	}))
	defer site.Close()
	for _, tc := range []struct{ width, height, percent, scroll int }{
		{1440, 900, 100, 0}, {1440, 900, 80, 0}, {1440, 900, 125, 0},
		{1537, 961, 67, 0}, {1537, 961, 125, 0}, {3840, 2400, 25, 0}, {390, 844, 200, 0},
		{1440, 900, 80, 1200},
	} {
		t.Run(fmt.Sprintf("%dx%d_%d_scroll%d", tc.width, tc.height, tc.percent, tc.scroll), func(t *testing.T) {
			zoom := float64(tc.percent) / 100
			cssWidth := int(math.Round(float64(tc.width) / zoom))
			cssHeight := int(math.Round(float64(tc.height) / zoom))
			data, err := c.Capture(context.Background(), Target{
				URL: site.URL, ViewportWidth: tc.width, ViewportHeight: tc.height, ZoomPercent: tc.percent,
				JavaScript: fmt.Sprintf(`
					if (innerWidth !== %d || innerHeight !== %d || Math.abs(devicePixelRatio - %f) > 0.00001)
						throw Error("incorrect zoom viewport");
					document.querySelector("#box").style.marginTop = "%dpx";
					scrollTo(0, %d);
				`, cssWidth, cssHeight, zoom, tc.scroll, tc.scroll),
			})
			if err != nil {
				t.Fatal(err)
			}
			image, err := jpeg.Decode(bytes.NewReader(data))
			if err != nil {
				t.Fatal(err)
			}
			if image.Bounds().Dx() != tc.width || image.Bounds().Dy() != tc.height {
				t.Fatal("zoom changed output dimensions", image.Bounds())
			}
			assertPixel := func(x, y int, red, green, blue bool) {
				t.Helper()
				r, g, b, _ := image.At(x, y).RGBA()
				if (r > 0x8000) != red || (g > 0x8000) != green || (b > 0x8000) != blue {
					t.Fatalf("unexpected pixel at %d,%d: %d,%d,%d", x, y, r, g, b)
				}
			}
			// Check responsive layout, scaled content size, and viewport-fixed content.
			assertPixel(10, 10, cssWidth < 1600, cssWidth >= 1600, false)
			assertPixel(tc.percent-6, 10, cssWidth < 1600, cssWidth >= 1600, false)
			assertPixel(tc.percent+6, 10, true, true, true)
			assertPixel(tc.width-10, tc.height-10, false, false, true)
		})
	}
}

func TestChromiumIntegrationViewportAndJavaScript(t *testing.T) {
	path := os.Getenv("SCREENSHOT_TEST_BROWSER")
	if path == "" {
		t.Skip("set SCREENSHOT_TEST_BROWSER to run real Chromium integration tests")
	}
	dir := t.TempDir()
	c := &Chromium{Browser: path, StateDir: dir, testNoSandbox: os.Geteuid() == 0}
	var executions atomic.Int32
	site := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/executed":
			executions.Add(1)
		case "/script.css":
			select {
			case <-r.Context().Done():
				return
			case <-time.After(time.Second):
			}
			w.Header().Set("Content-Type", "text/css")
			_, _ = w.Write([]byte("body { background: #00ff00; }"))
		default:
			w.Header().Set("Content-Type", "text/html")
			_, _ = w.Write([]byte(`<!doctype html><html><body style="margin:0"><div id="popup" style="position:fixed;inset:0;background:red"></div></body></html>`))
		}
	}))
	defer site.Close()
	t.Run("synchronous custom viewport", func(t *testing.T) {
		data, err := c.Capture(context.Background(), Target{URL: site.URL, ViewportWidth: 777, ViewportHeight: 555, JavaScript: `
			if (innerWidth !== 777 || innerHeight !== 555 || devicePixelRatio !== 1) throw Error("incorrect viewport");
			document.querySelector("#popup").remove();
			document.body.style.background = "#00ff00";
			return document.body;
		`})
		assertGreenScreenshot(t, data, err, 777, 555)
	})
	t.Run("async script and added stylesheet", func(t *testing.T) {
		data, err := c.Capture(context.Background(), Target{URL: site.URL, ViewportWidth: 390, ViewportHeight: 844, JavaScript: `
			if (innerWidth !== 390 || innerHeight !== 844 || devicePixelRatio !== 1) throw Error("incorrect viewport");
			await new Promise(resolve => setTimeout(resolve, 100));
			await fetch("/executed");
			document.querySelector("#popup").remove();
			const css = document.createElement("link");
			css.rel = "stylesheet"; css.href = "/script.css";
			document.head.appendChild(css);
		`})
		assertGreenScreenshot(t, data, err, 390, 844)
		if executions.Load() != 1 {
			t.Fatal("script was not executed exactly once")
		}
	})
	t.Run("script failures preserve the last image", func(t *testing.T) {
		m := newTestManager(t, dir, c)
		if _, err := m.Save("example.conf", Input{URL: site.URL}); err != nil {
			t.Fatal(err)
		}
		old := waitStatus(t, m, "example.conf", "ready")
		for _, tc := range []struct {
			name, script string
			timeout      bool
		}{
			{"syntax", "const =", false},
			{"exception", `throw new Error("private-script-token")`, false},
			{"rejection", `await Promise.reject("private-script-token")`, false},
			{"unresolved promise", "await new Promise(() => {});", true},
			{"infinite loop", "while (true) {}", true},
		} {
			t.Run(tc.name, func(t *testing.T) {
				started := time.Now()
				if _, err := m.Save("example.conf", Input{URL: site.URL, JavaScript: tc.script, LoadTimeoutSeconds: 5}); err != nil {
					t.Fatal(err)
				}
				deadline := time.Now().Add(10 * time.Second)
				var v View
				for time.Now().Before(deadline) {
					v = m.Latest().Items["example.conf"]
					if v.Status == "failed" {
						break
					}
					time.Sleep(10 * time.Millisecond)
				}
				if v.Status != "failed" || !strings.Contains(v.Error, "JavaScript") || strings.Contains(v.Error, "private-script-token") {
					t.Fatal("script failure not handled safely", v)
				}
				if strings.Contains(v.Error, "超时") != tc.timeout || time.Since(started) > 8*time.Second {
					t.Fatal("incorrect script timeout handling", v.Error, time.Since(started))
				}
				if v.ImageVersion != old.ImageVersion {
					t.Fatal("script failure replaced previous screenshot")
				}
				if data, err := m.Image("example.conf"); err != nil || len(data) == 0 {
					t.Fatal("previous screenshot unavailable", err)
				}
			})
		}
		// A fresh capture must still work after a script blocked the renderer.
		if _, err := m.Save("example.conf", Input{URL: site.URL}); err != nil {
			t.Fatal(err)
		}
		if v := waitStatus(t, m, "example.conf", "ready"); v.ImageVersion == old.ImageVersion {
			t.Fatal("capture did not recover after timeout")
		}
	})
	if files, err := filepath.Glob(filepath.Join(dir, ".browser-*")); err != nil || len(files) != 0 {
		t.Fatal("browser profiles leaked", files, err)
	}
}

func assertGreenScreenshot(t *testing.T, data []byte, captureErr error, width, height int) {
	t.Helper()
	if captureErr != nil {
		t.Fatal(captureErr)
	}
	decoded, err := jpeg.Decode(bytes.NewReader(data))
	if err != nil || decoded.Bounds().Dx() != width || decoded.Bounds().Dy() != height {
		t.Fatal("incorrect screenshot dimensions", err, fmt.Sprintf("want %dx%d", width, height))
	}
	r, g, b, _ := decoded.At(width-10, height-10).RGBA()
	if r > 10000 || g < 50000 || b > 10000 {
		t.Fatal("script modifications or added resources missing from screenshot")
	}
}

type urlError struct{ value string }

func (e *urlError) Error() string { return e.value }
