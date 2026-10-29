package server

import (
	"context"
	"encoding/json"
	"errors"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/azpanel/lnmp-gopanel/internal/logs"
	"github.com/pquerna/otp/totp"
)

func serverWithLoginLog(t *testing.T) *Server {
	t.Helper()
	s, _ := testServer(t)
	var err error
	s.LoginLog, err = logs.OpenLoginLog(filepath.Join(t.TempDir(), "state"))
	if err != nil {
		t.Fatal(err)
	}
	s.logReads = make(chan struct{}, 4)
	return s
}

func loginEvents(t *testing.T, s *Server) []logs.LoginEvent {
	t.Helper()
	window, err := s.LoginLog.Read(context.Background(), logs.Request{Path: s.LoginLog.Path(), Mode: "tail"})
	if err != nil {
		t.Fatal(err)
	}
	events := []logs.LoginEvent{}
	for _, line := range window.Lines {
		var event logs.LoginEvent
		if err := json.Unmarshal([]byte(line.Text), &event); err != nil {
			t.Fatal(err)
		}
		events = append(events, event)
	}
	return events
}

type cancelCatalogLogs struct {
	stubLogs
	cancel context.CancelFunc
}

func (s cancelCatalogLogs) List(context.Context) (logs.Catalog, error) {
	s.cancel()
	return logs.Catalog{}, context.DeadlineExceeded
}

func TestLoginAuditPasswordAndMetadata(t *testing.T) {
	s := serverWithLoginLog(t)
	h := s.Handler()
	for _, tc := range []struct {
		username, password, result string
		status                     int
		keepPassword               bool
	}{
		{"admin", "wrong\npassword", "failure", 401, true},
		{"wrong-user", "wrong", "failure", 401, true},
		{"wrong-user", "test-password-123", "failure", 401, false},
		{"admin", "test-password-123", "success", 200, false},
		{"admin", "", "failure", 401, true},
	} {
		body, _ := json.Marshal(map[string]string{"username": tc.username, "password": tc.password})
		r := httptest.NewRequest("POST", "/api/v1/auth/login", strings.NewReader(string(body)))
		r.RemoteAddr = "203.0.113.7:1234"
		r.Header.Set("Content-Type", "application/json")
		r.Header.Set("User-Agent", "Audit-Test/1.0")
		r.Header.Set("X-Forwarded-For", "198.51.100.2")
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		if w.Code != tc.status {
			t.Fatal(w.Code, w.Body.String())
		}
		events := loginEvents(t, s)
		event := events[len(events)-1]
		if event.Result != tc.result || event.Username != tc.username || event.IP != "203.0.113.7" || event.UA != "Audit-Test/1.0" || event.Time.IsZero() {
			t.Fatalf("wrong metadata: %+v", event)
		}
		if (event.Password != nil) != tc.keepPassword || event.Password != nil && *event.Password != tc.password {
			t.Fatalf("wrong password retention for %s", tc.username)
		}
	}
	for _, body := range []string{`{"username":"admin","password":`, `{"username":"admin","password":"` + strings.Repeat("x", 73) + `"}`} {
		if w := request(h, "POST", "/api/v1/auth/login", body, nil, "", ""); w.Code != 400 {
			t.Fatal(w.Code)
		}
		events := loginEvents(t, s)
		if event := events[len(events)-1]; event.Password != nil || event.Reason != "invalid_request" {
			t.Fatal("invalid request retained password", event)
		}
	}
	for s.limiter.Allow() {
	}
	if w := request(h, "POST", "/api/v1/auth/login", `{"username":"admin","password":"limited-secret"}`, nil, "", ""); w.Code != 429 {
		t.Fatal(w.Code)
	}
	events := loginEvents(t, s)
	if event := events[len(events)-1]; event.Password != nil || event.Reason != "rate_limited" {
		t.Fatal("rate limit retained password", event)
	}
}

func TestLoginAuditSecondFactor(t *testing.T) {
	s := serverWithLoginLog(t)
	setup, err := s.account().StartSetup("setup-session", "test-password-123")
	if err != nil {
		t.Fatal(err)
	}
	code, err := totp.GenerateCode(setup.Secret, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	codes, err := s.account().ConfirmSetup("setup-session", setup.ID, code)
	if err != nil {
		t.Fatal(err)
	}
	h := s.Handler()
	start := func() string {
		t.Helper()
		w := authRequest(h, "POST", "/auth/login", map[string]string{"username": "admin", "password": "test-password-123"}, nil, "")
		var out struct {
			Token string `json:"challenge_token"`
		}
		if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &out) != nil || out.Token == "" {
			t.Fatal(w.Code, w.Body.String())
		}
		return out.Token
	}
	challenge := start()
	if len(loginEvents(t, s)) != 0 {
		t.Fatal("pending login recorded as success")
	}
	if w := authRequest(h, "POST", "/auth/login/totp", map[string]string{"challenge_token": challenge, "code": code}, nil, ""); w.Code != 401 {
		t.Fatal("replayed enrollment code accepted", w.Code)
	}
	sessionFrom(t, authRequest(h, "POST", "/auth/login/totp", map[string]string{"challenge_token": challenge, "recovery_code": codes[0]}, nil, ""))
	challenge = start()
	nextCode, err := totp.GenerateCode(setup.Secret, time.Now().Add(30*time.Second))
	if err != nil {
		t.Fatal(err)
	}
	sessionFrom(t, authRequest(h, "POST", "/auth/login/totp", map[string]string{"challenge_token": challenge, "code": nextCode}, nil, ""))
	challenge = start()
	if w := authRequest(h, "POST", "/auth/login/totp", map[string]string{"challenge_token": challenge, "recovery_code": codes[0]}, nil, ""); w.Code != 401 {
		t.Fatal(w.Code)
	}
	events := loginEvents(t, s)
	if len(events) != 4 || events[0].Result != "failure" || events[1].Result != "success" || events[2].Result != "success" || events[3].Result != "failure" {
		t.Fatal("incorrect second-factor outcomes", events)
	}
	for _, event := range events {
		if event.Stage != "totp" || event.Username != "admin" || event.Password != nil {
			t.Fatal("incorrect second-factor metadata", event)
		}
	}
	data, _ := os.ReadFile(s.LoginLog.Path())
	for _, secret := range []string{"test-password-123", code, nextCode, codes[0], challenge, setup.Secret} {
		if strings.Contains(string(data), secret) {
			t.Fatal("second-factor secret leaked")
		}
	}
}

func TestLoginLogEndpointsWithoutHelper(t *testing.T) {
	s := serverWithLoginLog(t)
	s.Logs = &stubLogs{err: errors.New("helper unavailable")}
	h := s.Handler()
	cookie, csrf := sessionFrom(t, authRequest(h, "POST", "/auth/login", map[string]string{"username": "admin", "password": "test-password-123"}, nil, ""))
	w := request(h, "GET", "/api/v1/logs", "", cookie, "", "")
	var catalog logs.Catalog
	if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &catalog) != nil || len(catalog.Files) != 1 || len(catalog.Warnings) != 1 || catalog.Files[0].Name != "登录日志" || catalog.Files[0].Custom || !catalog.Files[0].Available {
		t.Fatal("local catalog unavailable", w.Code, w.Body.String())
	}
	body, _ := json.Marshal(logs.Request{Path: s.LoginLog.Path(), Mode: "tail", Rules: []logs.Rule{}})
	for _, tc := range []struct {
		authenticated, csrf bool
		status              int
	}{{false, false, 401}, {true, false, 403}, {true, true, 200}} {
		c := cookie
		if !tc.authenticated {
			c = nil
		}
		token := ""
		if tc.csrf {
			token = csrf
		}
		w := request(h, "POST", "/api/v1/logs/read", string(body), c, token, "")
		if w.Code != tc.status {
			t.Fatal(w.Code, w.Body.String())
		}
		if tc.status == 200 && !strings.Contains(w.Body.String(), "authenticated") {
			t.Fatal("missing login entry")
		}
	}
	for _, path := range []string{filepath.Join(filepath.Dir(s.LoginLog.Path()), "auth.json"), "/var/lib/lnmp-gopanel/auth.json", s.LoginLog.Path() + "/../auth.json"} {
		body, _ := json.Marshal(logs.Request{Path: path, Mode: "tail"})
		if w := request(h, "POST", "/api/v1/logs/read", string(body), cookie, csrf, ""); w.Code != 400 {
			t.Fatal("other state file allowed", w.Code)
		}
	}
	// A storage failure must not turn valid credentials into a rejected login.
	if err := os.Chmod(s.LoginLog.Path(), 0644); err != nil {
		t.Fatal(err)
	}
	sessionFrom(t, authRequest(h, "POST", "/auth/login", map[string]string{"username": "admin", "password": "test-password-123"}, nil, ""))
	if s.LoginLog.File(context.Background()).Error == "" {
		t.Fatal("storage failure not exposed")
	}
	if err := os.Chmod(s.LoginLog.Path(), 0600); err != nil {
		t.Fatal(err)
	}
	sessionFrom(t, authRequest(h, "POST", "/auth/login", map[string]string{"username": "admin", "password": "test-password-123"}, nil, ""))
	if s.LoginLog.File(context.Background()).Error != "" {
		t.Fatal("storage recovery not exposed")
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	s.Logs = &cancelCatalogLogs{cancel: cancel}
	r := httptest.NewRequest("GET", "/api/v1/logs", nil).WithContext(ctx)
	r.AddCookie(cookie)
	w = httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &catalog) != nil || !catalog.Files[0].Available {
		t.Fatal("helper cancellation marked local log unavailable", w.Code, w.Body.String())
	}
	s.Logs = nil
	if w := request(h, "GET", "/api/v1/logs", "", cookie, "", ""); w.Code != 200 {
		t.Fatal("nil helper blocks local log", w.Code)
	}
	// Existing sessions and origin protection remain the source of access control.
	if w := request(h, "POST", "/api/v1/logs/read", string(body), cookie, csrf, "https://evil.example"); w.Code != 403 {
		t.Fatal("origin bypass", w.Code)
	}
}
