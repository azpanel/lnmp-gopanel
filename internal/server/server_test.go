package server

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"testing/fstest"
	"time"

	"github.com/azpanel/lnmp-gopanel/internal/auth"
	"github.com/azpanel/lnmp-gopanel/internal/components"
	"github.com/azpanel/lnmp-gopanel/internal/config"
	"github.com/azpanel/lnmp-gopanel/internal/control"
	"github.com/azpanel/lnmp-gopanel/internal/docker"
	"github.com/azpanel/lnmp-gopanel/internal/metrics"
	"github.com/azpanel/lnmp-gopanel/internal/releases"
	"github.com/azpanel/lnmp-gopanel/internal/storage"
	"github.com/azpanel/lnmp-gopanel/internal/vhosts"
	"golang.org/x/crypto/bcrypt"
)

type stubExecutor struct {
	calls atomic.Int32
	fail  bool
}

func (e *stubExecutor) Run(context.Context, string, string) (control.Result, error) {
	e.calls.Add(1)
	if e.fail {
		return control.Result{}, errors.New("operation timed out")
	}
	return control.Result{State: "running", Message: "ok"}, nil
}
func testServer(t *testing.T) (*Server, *stubExecutor) {
	t.Helper()
	hash, err := bcrypt.GenerateFromPassword([]byte("test-password-123"), bcrypt.MinCost)
	if err != nil {
		t.Fatal(err)
	}
	e := &stubExecutor{}
	s := &Server{Config: config.Config{Username: "admin", PasswordHash: string(hash)}, Sessions: auth.New(), Metrics: metrics.New(), Services: control.New([]config.Service{{Name: "nginx", Manager: "init", Program: "/etc/init.d/nginx", CheckProgram: "/usr/local/nginx/sbin/nginx"}}, e), Assets: fstest.MapFS{"index.html": &fstest.MapFile{Data: []byte("<html>panel</html>")}}, streams: make(chan struct{}, 32)}
	return s, e
}
func request(h http.Handler, method, path, body string, cookie *http.Cookie, csrf, origin string) *httptest.ResponseRecorder {
	r := httptest.NewRequest(method, path, strings.NewReader(body))
	r.Header.Set("Content-Type", "application/json")
	if cookie != nil {
		r.AddCookie(cookie)
	}
	if csrf != "" {
		r.Header.Set("X-CSRF-Token", csrf)
	}
	if origin != "" {
		r.Header.Set("Origin", origin)
	}
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	return w
}

func TestFrontendAssetCaching(t *testing.T) {
	s, _ := testServer(t)
	s.Assets = fstest.MapFS{
		"index.html":        &fstest.MapFile{Data: []byte("<html>panel</html>")},
		"asset-version.js":  &fstest.MapFile{Data: []byte("version check")},
		"theme.js":          &fstest.MapFile{Data: []byte("theme")},
		"assets/app-a1.js":  &fstest.MapFile{Data: []byte("app")},
		"assets/app-b2.css": &fstest.MapFile{Data: []byte("body {}")},
	}
	h := s.Handler()
	for _, tc := range []struct{ path, cache string }{
		{"/", "no-store"},
		{"/index.html?_asset_check=123", "no-store"},
		{"/vhosts", "no-store"},
		{"/asset-version.js", "no-cache"},
		{"/theme.js", "no-cache"},
		{"/assets/app-a1.js", "public, max-age=31536000, immutable"},
		{"/assets/app-b2.css", "public, max-age=31536000, immutable"},
	} {
		for _, method := range []string{"GET", "HEAD"} {
			w := request(h, method, tc.path, "", nil, "", "")
			if w.Code != http.StatusOK || w.Header().Get("Cache-Control") != tc.cache {
				t.Errorf("%s %s: status=%d cache=%q", method, tc.path, w.Code, w.Header().Get("Cache-Control"))
			}
		}
	}
	for _, path := range []string{"/assets/old.js", "/assets/old.css", "/missing.js"} {
		if w := request(h, "GET", path, "", nil, "", ""); w.Code != http.StatusNotFound {
			t.Errorf("missing asset %s returned %d", path, w.Code)
		}
	}
}

func TestAuthenticationAndCookie(t *testing.T) {
	s, _ := testServer(t)
	s.Config.SecureCookie = true
	h := s.Handler()
	for _, path := range []string{"/host", "/metrics", "/metrics/stream", "/services", "/auth/session"} {
		w := request(h, "GET", "/api/v1"+path, "", nil, "", "")
		if w.Code != 401 {
			t.Fatalf("%s: %d", path, w.Code)
		}
		if w.Header().Get("Cache-Control") != "no-store" {
			t.Fatal("private data must not be cached")
		}
	}
	for _, body := range []string{`{"username":"wrong","password":"test-password-123"}`, `{"username":"admin","password":"wrong"}`} {
		if w := request(h, "POST", "/api/v1/auth/login", body, nil, "", ""); w.Code != 401 {
			t.Fatal(w.Body.String())
		}
	}
	w := request(h, "POST", "/api/v1/auth/login", `{"username":"admin","password":"test-password-123"}`, nil, "", "")
	if w.Code != 200 {
		t.Fatal(w.Body.String())
	}
	cookie := w.Result().Cookies()[0]
	if !cookie.HttpOnly || !cookie.Secure || cookie.SameSite != http.SameSiteStrictMode || cookie.Path != "/" {
		t.Fatalf("insecure cookie: %+v", cookie)
	}
	var v struct {
		CSRF string `json:"csrf_token"`
	}
	_ = json.Unmarshal(w.Body.Bytes(), &v)
	if v.CSRF == "" || v.CSRF == cookie.Value {
		t.Fatal("missing CSRF token")
	}
	if w := request(h, "GET", "/api/v1/host", "", cookie, "", ""); w.Code != 200 {
		t.Fatal(w.Code)
	}
	if w := request(h, "POST", "/api/v1/auth/logout", "", cookie, v.CSRF, ""); w.Code != 204 {
		t.Fatal(w.Code)
	}
	if w := request(h, "GET", "/api/v1/host", "", cookie, "", ""); w.Code != 401 {
		t.Fatal("logout failed")
	}
}
func TestCSRFAndActionValidation(t *testing.T) {
	s, e := testServer(t)
	h := s.Handler()
	id, session := s.Sessions.Create()
	cookie := &http.Cookie{Name: auth.CookieName, Value: id}
	for _, token := range []string{"", "wrong"} {
		w := request(h, "POST", "/api/v1/services/nginx/actions/restart", "", cookie, token, "")
		if w.Code != 403 {
			t.Fatal("CSRF bypass")
		}
	}
	if w := request(h, "POST", "/api/v1/services/nginx/actions/restart", "", cookie, session.CSRF, "https://evil.example"); w.Code != 403 {
		t.Fatal("cross origin action accepted")
	}
	for _, path := range []string{"/services/sshd/actions/restart", "/services/nginx/actions/exec", "/services/nginx/actions/status", "/services/nginx/actions/version"} {
		if w := request(h, "POST", "/api/v1"+path, "", cookie, session.CSRF, ""); w.Code != 400 {
			t.Fatalf("unsafe action accepted: %s %d", path, w.Code)
		}
	}
	if e.calls.Load() != 0 {
		t.Fatal("unauthorized request executed command")
	}
	if w := request(h, "POST", "/api/v1/services/nginx/actions/restart", "", cookie, session.CSRF, ""); w.Code != 200 {
		t.Fatal(w.Body.String())
	}
	if e.calls.Load() != 2 {
		t.Fatal("operation must refresh status")
	}
}

type releaseSourceStub struct {
	calls atomic.Int32
	done  chan struct{}
}

type dockerBackendStub struct {
	calls atomic.Int32
	done  chan struct{}
}

func (b *dockerBackendStub) Docker(ctx context.Context) (docker.Snapshot, error) {
	b.calls.Add(1)
	select {
	case <-b.done:
		now := time.Now()
		return docker.Snapshot{Status: "ready", Containers: []docker.Container{{ID: "abc123", Name: "web"}}, DiskUsage: []docker.DiskUsage{{Type: "Images", Size: "1GB"}}, UpdatedAt: &now, Errors: map[string]string{}}, nil
	case <-ctx.Done():
		return docker.Snapshot{}, ctx.Err()
	}
}
func TestDockerAPIRequiresAuthAndCSRFAndReturnsAsyncSnapshot(t *testing.T) {
	s, _ := testServer(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	b := &dockerBackendStub{done: make(chan struct{})}
	s.Docker = docker.NewManager(ctx, b)
	h := s.Handler()
	for _, method := range []string{"GET", "POST"} {
		path := "/api/v1/docker"
		if method == "POST" {
			path += "/refresh"
		}
		if w := request(h, method, path, "", nil, "", ""); w.Code != 401 {
			t.Fatal("Docker exposed without auth")
		}
	}
	id, session := s.Sessions.Create()
	cookie := &http.Cookie{Name: auth.CookieName, Value: id}
	if w := request(h, "POST", "/api/v1/docker/refresh", "", cookie, "", ""); w.Code != 403 {
		t.Fatal("Docker refresh bypassed CSRF")
	}
	if w := request(h, "POST", "/api/v1/docker/refresh", "", cookie, session.CSRF, "https://evil.example"); w.Code != 403 {
		t.Fatal("cross-origin Docker refresh accepted")
	}
	if b.calls.Load() != 0 {
		t.Fatal("unauthorized Docker access")
	}
	if w := request(h, "POST", "/api/v1/docker/containers/web/stop", "", cookie, session.CSRF, ""); w.Code != 404 {
		t.Fatal("Docker mutation route exists")
	}
	for _, method := range []string{"GET", "POST"} {
		path, want := "/api/v1/docker", 200
		if method == "POST" {
			path += "/refresh"
			want = 202
		}
		w := request(h, method, path, "", cookie, session.CSRF, "")
		if w.Code != want || !strings.Contains(w.Body.String(), `"status":"loading"`) {
			t.Fatalf("missing immediate async response: %d %s", w.Code, w.Body.String())
		}
	}
	close(b.done)
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		w := request(h, "GET", "/api/v1/docker", "", cookie, "", "")
		if strings.Contains(w.Body.String(), `"status":"ready"`) {
			if !strings.Contains(w.Body.String(), `"name":"web"`) || b.calls.Load() != 1 {
				t.Fatal(w.Body.String())
			}
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatal("Docker snapshot did not complete")
}

func (s *releaseSourceStub) Fetch(ctx context.Context, name string) (releases.Release, error) {
	s.calls.Add(1)
	select {
	case <-s.done:
		return releases.Release{Version: "1.30.2", Changelog: "fixes"}, nil
	case <-ctx.Done():
		return releases.Release{}, ctx.Err()
	}
}

func TestReleaseAPIsAuthCSRFAndAsyncResponses(t *testing.T) {
	s, _ := testServer(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	source := &releaseSourceStub{done: make(chan struct{})}
	s.Releases = releases.NewManager(ctx, source)
	h := s.Handler()
	if w := request(h, "GET", "/api/v1/services/releases", "", nil, "", ""); w.Code != 401 {
		t.Fatal("releases exposed without auth")
	}
	if w := request(h, "POST", "/api/v1/services/releases/refresh", "", nil, "", ""); w.Code != 401 {
		t.Fatal("refresh exposed without auth")
	}
	id, session := s.Sessions.Create()
	cookie := &http.Cookie{Name: auth.CookieName, Value: id}
	if w := request(h, "POST", "/api/v1/services/releases/refresh", "", cookie, "", ""); w.Code != 403 {
		t.Fatal("refresh bypassed CSRF")
	}
	if source.calls.Load() != 0 {
		t.Fatal("unauthorized upstream query")
	}
	for _, method := range []string{"GET", "POST"} {
		path, want := "/api/v1/services/releases", 200
		if method == "POST" {
			path += "/refresh"
			want = 202
		}
		w := request(h, method, path, "", cookie, session.CSRF, "")
		if w.Code != want || !strings.Contains(w.Body.String(), `"status":"loading"`) {
			t.Fatalf("missing async response: %d %s", w.Code, w.Body.String())
		}
	}
	close(source.done)
	wantReleases := len(components.Names()) + len(s.Services.List())
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		w := request(h, "GET", "/api/v1/services/releases", "", cookie, "", "")
		if strings.Count(w.Body.String(), `"status":"ready"`) == wantReleases {
			if int(source.calls.Load()) != wantReleases || !strings.Contains(w.Body.String(), "1.30.2") || !strings.Contains(w.Body.String(), `"composer":`) {
				t.Fatal(w.Body.String())
			}
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatal("release query did not complete")
}
func TestOriginAndLoginRateLimit(t *testing.T) {
	s, _ := testServer(t)
	s.Config.PublicOrigin = "https://panel.example.com"
	h := s.Handler()
	body := `{"username":"admin","password":"test-password-123"}`
	if w := request(h, "POST", "/api/v1/auth/login", body, nil, "", "https://evil.example"); w.Code != 403 {
		t.Fatal("cross origin login accepted")
	}
	if w := request(h, "POST", "/api/v1/auth/login", body, nil, "", s.Config.PublicOrigin); w.Code != 200 {
		t.Fatal(w.Body.String())
	}
	for i := 0; i < 9; i++ {
		request(h, "POST", "/api/v1/auth/login", `{}`, nil, "", "")
	}
	if w := request(h, "POST", "/api/v1/auth/login", body, nil, "", ""); w.Code != 429 || w.Header().Get("Retry-After") == "" {
		t.Fatal("missing login limit")
	}
}
func TestServiceFailureAndSPA(t *testing.T) {
	s, e := testServer(t)
	e.fail = true
	h := s.Handler()
	id, session := s.Sessions.Create()
	cookie := &http.Cookie{Name: auth.CookieName, Value: id}
	if w := request(h, "POST", "/api/v1/services/nginx/actions/restart", "", cookie, session.CSRF, ""); w.Code != 502 {
		t.Fatal(w.Code)
	}
	for _, path := range []string{"/", "/system", "/services"} {
		if w := request(h, "GET", path, "", nil, "", ""); w.Code != 200 || !strings.Contains(w.Body.String(), "panel") {
			t.Fatalf("SPA route %s failed", path)
		}
	}
	for _, path := range []string{"/assets/missing.js", "/api/v1/missing"} {
		if w := request(h, "GET", path, "", nil, "", ""); w.Code != 404 {
			t.Fatal("missing asset/API must not return SPA")
		}
	}
}
func TestSSEConnectionLimit(t *testing.T) {
	s, _ := testServer(t)
	for i := 0; i < 32; i++ {
		s.streams <- struct{}{}
	}
	id, _ := s.Sessions.Create()
	if w := request(s.Handler(), "GET", "/api/v1/metrics/stream", "", &http.Cookie{Name: auth.CookieName, Value: id}, "", ""); w.Code != 429 {
		t.Fatal("stream limit missing")
	}
}
func TestSSELatestAndRevocation(t *testing.T) {
	s, _ := testServer(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go s.Metrics.Run(ctx)
	deadline := time.Now().Add(2 * time.Second)
	for s.Metrics.Latest().Timestamp.IsZero() {
		if time.Now().After(deadline) {
			t.Fatal("collector failed to initialize")
		}
		time.Sleep(5 * time.Millisecond)
	}
	ts := httptest.NewServer(s.Handler())
	defer ts.Close()
	id, _ := s.Sessions.Create()
	reqCtx, stop := context.WithTimeout(context.Background(), 5*time.Second)
	defer stop()
	req, _ := http.NewRequestWithContext(reqCtx, "GET", ts.URL+"/api/v1/metrics/stream", nil)
	req.AddCookie(&http.Cookie{Name: auth.CookieName, Value: id})
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.Header.Get("X-Accel-Buffering") != "no" {
		t.Fatal("proxy buffering not disabled")
	}
	reader := bufio.NewReader(resp.Body)
	seenConnected, seenMetrics := false, false
	for !seenMetrics {
		line, err := reader.ReadString('\n')
		if err != nil {
			t.Fatal(err)
		}
		if strings.HasPrefix(line, "event:connected") {
			seenConnected = true
		}
		if strings.HasPrefix(line, "event:metrics") {
			seenMetrics = true
		}
	}
	if !seenConnected {
		t.Fatal("missing connection event")
	}
	s.Sessions.Delete(id)
	all, err := io.ReadAll(reader)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(all), "event:auth_expired") {
		t.Fatal("revoked stream did not close with authentication event")
	}
}

type blockingScanner struct {
	calls atomic.Int32
	done  chan struct{}
}

func (s *blockingScanner) Scan(ctx context.Context, report func(storage.Progress)) error {
	s.calls.Add(1)
	select {
	case <-s.done:
		report(storage.Progress{Files: []storage.File{{Path: "/large file", Size: 42}}, Scanned: 1, Complete: true})
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}
func TestStorageAPIsRequireAuthAndCSRF(t *testing.T) {
	s, _ := testServer(t)
	scanner := &blockingScanner{done: make(chan struct{})}
	s.Storage = storage.NewManager(context.Background(), scanner)
	h := s.Handler()
	for _, path := range []string{"/storage/mounts", "/storage/scan"} {
		if w := request(h, "GET", "/api/v1"+path, "", nil, "", ""); w.Code != 401 {
			t.Fatal("storage exposed without auth")
		}
	}
	id, session := s.Sessions.Create()
	cookie := &http.Cookie{Name: auth.CookieName, Value: id}
	if w := request(h, "POST", "/api/v1/storage/scan", "", cookie, "", ""); w.Code != 403 {
		t.Fatal("scan bypassed CSRF")
	}
	if scanner.calls.Load() != 0 {
		t.Fatal("unauthorized scan executed")
	}
	for i := 0; i < 2; i++ {
		if w := request(h, "POST", "/api/v1/storage/scan", "", cookie, session.CSRF, ""); w.Code != 202 {
			t.Fatal(w.Code)
		}
	}
	close(scanner.done)
	deadline := time.Now().Add(time.Second)
	for s.Storage.Latest().Status == "running" && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	if scanner.calls.Load() != 1 {
		t.Fatal("parallel scan jobs were started")
	}
	w := request(h, "GET", "/api/v1/storage/scan", "", cookie, "", "")
	if w.Code != 200 || !strings.Contains(w.Body.String(), "/large file") {
		t.Fatal(w.Body.String())
	}
	w = request(h, "GET", "/api/v1/storage/mounts", "", cookie, "", "")
	if w.Code != 200 || !strings.Contains(w.Body.String(), `"fs_type"`) {
		t.Fatal(w.Body.String())
	}
}

type fakeVhostBackend struct {
	reads   atomic.Int32
	toggles atomic.Int32
}

func (f *fakeVhostBackend) ListVhosts(context.Context) ([]vhosts.Site, error) {
	return []vhosts.Site{{Name: "example.conf", Filename: "example.conf", Enabled: true, Domains: []string{"example.com"}, Roots: []vhosts.Root{}}}, nil
}
func (f *fakeVhostBackend) VhostUsage(context.Context, func(vhosts.Usage)) error { return nil }
func (f *fakeVhostBackend) ReadVhost(ctx context.Context, name string) (vhosts.ConfigView, error) {
	f.reads.Add(1)
	return vhosts.ConfigView{Filename: name, Content: "# preserved\nserver {}\n"}, nil
}
func (f *fakeVhostBackend) ToggleVhost(ctx context.Context, name string, enabled bool) (vhosts.ActionResult, error) {
	f.toggles.Add(1)
	return vhosts.ActionResult{Name: name, Enabled: enabled, Changed: true, Message: "ok"}, nil
}
func TestVhostAPIsValidateAuthNamesAndCSRF(t *testing.T) {
	s, _ := testServer(t)
	backend := &fakeVhostBackend{}
	s.Vhosts = vhosts.NewManager(context.Background(), backend)
	h := s.Handler()
	for _, path := range []string{"/vhosts", "/vhosts/count", "/vhosts/config?name=example.conf"} {
		if w := request(h, "GET", "/api/v1"+path, "", nil, "", ""); w.Code != 401 {
			t.Fatal("vhost API exposed")
		}
	}
	id, session := s.Sessions.Create()
	cookie := &http.Cookie{Name: auth.CookieName, Value: id}
	if w := request(h, "GET", "/api/v1/vhosts/count", "", cookie, "", ""); w.Code != 200 || !strings.Contains(w.Body.String(), `"count":1`) {
		t.Fatal("invalid vhost counter", w.Code, w.Body.String())
	}
	for _, path := range []string{"/vhosts/refresh", "/vhosts/toggle"} {
		if w := request(h, "POST", "/api/v1"+path, `{"name":"example.conf","enabled":false}`, cookie, "", ""); w.Code != 403 {
			t.Fatal("missing CSRF")
		}
	}
	for _, body := range []string{`{"name":"../nginx.conf","enabled":false}`, `{"name":"example.conf"}`, `{"name":"x.conf.disabled","enabled":true}`} {
		if w := request(h, "POST", "/api/v1/vhosts/toggle", body, cookie, session.CSRF, ""); w.Code != 400 {
			t.Fatalf("bad input %d", w.Code)
		}
	}
	if backend.toggles.Load() != 0 {
		t.Fatal("unsafe request reached helper")
	}
	if w := request(h, "GET", "/api/v1/vhosts/config?name=../nginx.conf", "", cookie, "", ""); w.Code != 400 {
		t.Fatal(w.Code)
	}
	if backend.reads.Load() != 0 {
		t.Fatal("unsafe read reached helper")
	}
	if w := request(h, "GET", "/api/v1/vhosts/config?name=example.conf", "", cookie, "", ""); w.Code != 200 || !strings.Contains(w.Body.String(), "preserved") {
		t.Fatal(w.Body.String())
	}
	if w := request(h, "POST", "/api/v1/vhosts/refresh", "", cookie, session.CSRF, ""); w.Code != 202 {
		t.Fatal(w.Code)
	}
	deadline := time.Now().Add(time.Second)
	for s.Vhosts.Latest().Status != "ready" && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	if w := request(h, "POST", "/api/v1/vhosts/toggle", `{"name":"example.conf","enabled":false}`, cookie, session.CSRF, ""); w.Code != 200 {
		t.Fatal(w.Body.String())
	}
	if backend.toggles.Load() != 1 {
		t.Fatal("missing action")
	}
	w := request(h, "GET", "/api/v1/vhosts", "", cookie, "", "")
	if !strings.Contains(w.Body.String(), "example.conf.disabled") {
		t.Fatal(w.Body.String())
	}
}
