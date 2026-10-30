package server

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/azpanel/lnmp-gopanel/internal/auth"
	"github.com/azpanel/lnmp-gopanel/internal/logs"
	"github.com/pquerna/otp/totp"
)

const validLoginBody = `{"username":"admin","password":"test-password-123"}`

func awaitAuth[T any](t *testing.T, ch <-chan T) T {
	t.Helper()
	select {
	case v := <-ch:
		return v
	case <-time.After(3 * time.Second):
		t.Fatal("authentication operation stalled")
	}
	var zero T
	return zero
}

type authBlock struct {
	entered     chan struct{}
	release     chan struct{}
	enterOnce   sync.Once
	releaseOnce sync.Once
}

func newAuthBlock() *authBlock {
	return &authBlock{entered: make(chan struct{}), release: make(chan struct{})}
}
func (b *authBlock) wait()    { b.enterOnce.Do(func() { close(b.entered) }); <-b.release }
func (b *authBlock) unblock() { b.releaseOnce.Do(func() { close(b.release) }) }

type authSlowBody struct {
	*authBlock
	io.Reader
}

func (b *authSlowBody) Read(p []byte) (int, error) { b.wait(); return b.Reader.Read(p) }
func (b *authSlowBody) Close() error               { return nil }

func authHTTPRequest(method, path, body, source string, cookie *http.Cookie, csrf string) *http.Request {
	r := httptest.NewRequest(method, "/api/v1"+path, strings.NewReader(body))
	r.Header.Set("Content-Type", "application/json")
	r.RemoteAddr = source
	if cookie != nil {
		r.AddCookie(cookie)
	}
	if csrf != "" {
		r.Header.Set("X-CSRF-Token", csrf)
	}
	return r
}
func serveAuth(h http.Handler, r *http.Request) *httptest.ResponseRecorder {
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	return w
}
func asyncAuth(h http.Handler, r *http.Request) <-chan *httptest.ResponseRecorder {
	done := make(chan *httptest.ResponseRecorder, 1)
	go func() { done <- serveAuth(h, r) }()
	return done
}
func storedAuthSession(s *Server) (*http.Cookie, string) {
	id, v := s.Sessions.Create()
	return &http.Cookie{Name: auth.CookieName, Value: id}, v.CSRF
}

func recoveryLoginBodies(t *testing.T, s *Server) (string, string) {
	t.Helper()
	setup, err := s.account().StartSetup("setup", "test-password-123")
	if err != nil {
		t.Fatal(err)
	}
	code, err := totp.GenerateCode(setup.Secret, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	codes, err := s.account().ConfirmSetup("setup", setup.ID, code)
	if err != nil {
		t.Fatal(err)
	}
	bodies := make([]string, 2)
	for i := range bodies {
		challenge, err := s.account().StartLogin("test-password-123")
		if err != nil {
			t.Fatal(err)
		}
		body, err := json.Marshal(map[string]string{"challenge_token": challenge, "recovery_code": codes[i]})
		if err != nil {
			t.Fatal(err)
		}
		bodies[i] = string(body)
	}
	return bodies[0], bodies[1]
}

func TestAuthSlowBodiesDoNotBlockLogoutOrSecurity(t *testing.T) {
	for _, tc := range []struct {
		method, path, body string
		status             int
	}{
		{"POST", "/auth/login", `{}`, 401},
		{"POST", "/auth/login/totp", `{}`, 401},
		{"POST", "/auth/password", `{"current_password":"wrong","new_password":"anything"}`, 403},
		{"POST", "/auth/totp/setup", `{"current_password":"wrong"}`, 403},
		{"POST", "/auth/totp/confirm", `{}`, 409},
		{"DELETE", "/auth/totp", `{"current_password":"wrong"}`, 403},
		{"POST", "/auth/totp/recovery-codes", `{}`, 409},
	} {
		t.Run(tc.path, func(t *testing.T) {
			s, _ := testServer(t)
			h := s.Handler()
			cookie, csrf := storedAuthSession(s)
			other, otherCSRF := storedAuthSession(s)
			block := newAuthBlock()
			r := authHTTPRequest(tc.method, tc.path, tc.body, "203.0.113.1:1000", cookie, csrf)
			r.Body = &authSlowBody{block, strings.NewReader(tc.body)}
			done := asyncAuth(h, r)
			finished := false
			t.Cleanup(func() {
				block.unblock()
				if !finished {
					awaitAuth(t, done)
				}
			})
			awaitAuth(t, block.entered)
			out := awaitAuth(t, asyncAuth(h, authHTTPRequest("POST", "/auth/logout", "", "198.51.100.1:1000", other, otherCSRF)))
			if out.Code != 204 {
				t.Fatal(out.Code, out.Body.String())
			}
			out = awaitAuth(t, asyncAuth(h, authHTTPRequest("DELETE", "/auth/totp/setup", "", "198.51.100.1:1000", cookie, csrf)))
			if out.Code != 204 {
				t.Fatal(out.Code, out.Body.String())
			}
			block.unblock()
			out = awaitAuth(t, done)
			finished = true
			if out.Code != tc.status {
				t.Fatal(out.Code, out.Body.String())
			}
		})
	}
}

func TestLoginStrictRequestBody(t *testing.T) {
	for _, tc := range []struct {
		name, body, contentType string
		length                  int64
		status                  int
	}{
		{"valid", validLoginBody, "application/json", -2, 200},
		{"charset", validLoginBody, "application/json; charset=utf-8", -2, 200},
		{"trailing-space", validLoginBody + " \n\t", "application/json", -2, 200},
		{"boundary", validLoginBody + strings.Repeat(" ", 4096-len(validLoginBody)), "application/json", -2, 200},
		{"large", validLoginBody + strings.Repeat(" ", 4097-len(validLoginBody)), "application/json", -2, 413},
		{"chunked-large", validLoginBody + strings.Repeat(" ", 4097-len(validLoginBody)), "application/json", -1, 413},
		{"chunked-garbage", validLoginBody + strings.Repeat("x", 8192), "application/json", -1, 413},
		{"trailing-garbage", validLoginBody + "x", "application/json", -2, 400},
		{"second-object", validLoginBody + `{}`, "application/json", -2, 400},
		{"second-null", validLoginBody + `null`, "application/json", -2, 400},
		{"unknown", `{"username":"admin","password":"test-password-123","extra":true}`, "application/json", -2, 400},
		{"truncated", `{"username":"admin",`, "application/json", -2, 400},
		{"null", `null`, "application/json", -2, 400},
		{"array", `[]`, "application/json", -2, 400},
		{"empty", "", "application/json", -2, 400},
		{"unicode-whitespace", validLoginBody + "\u00a0", "application/json", -2, 400},
		{"wrong-type", validLoginBody, "text/plain", -2, 415},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := serverWithLoginLog(t)
			h := s.Handler()
			r := authHTTPRequest("POST", "/auth/login", tc.body, "192.0.2.1:1000", nil, "")
			r.Header.Set("Content-Type", tc.contentType)
			if tc.length != -2 {
				r.ContentLength = tc.length
				r.TransferEncoding = []string{"chunked"}
			}
			out := serveAuth(h, r)
			if out.Code != tc.status {
				t.Fatal(out.Code, out.Body.String())
			}
			if out.Code >= 400 {
				if len(out.Result().Cookies()) != 0 {
					t.Fatal("invalid body created a session")
				}
				events := loginEvents(t, s)
				if len(events) != 1 || events[0].Reason != "invalid_request" || events[0].Password != nil {
					t.Fatal(events)
				}
			}
		})
	}
}

func TestLoginSourcesAndStagesAreIndependent(t *testing.T) {
	s, _ := testServer(t)
	h := s.Handler()
	for range 10 {
		if out := serveAuth(h, authHTTPRequest("POST", "/auth/login", `{}`, "203.0.113.1:1000", nil, "")); out.Code != 401 {
			t.Fatal(out.Code)
		}
	}
	r := authHTTPRequest("POST", "/auth/login", validLoginBody, "203.0.113.1:2000", nil, "")
	r.Header.Set("X-Forwarded-For", "198.51.100.1")
	r.Header.Set("X-Real-IP", "198.51.100.2")
	out := serveAuth(h, r)
	retry, err := strconv.Atoi(out.Header().Get("Retry-After"))
	if out.Code != 429 || err != nil || retry < 1 || retry > 60 {
		t.Fatal(out.Code, retry, err)
	}
	if out = serveAuth(h, authHTTPRequest("POST", "/auth/login", validLoginBody, "198.51.100.1:1000", nil, "")); out.Code != 200 {
		t.Fatal(out.Code)
	}
	for range 10 {
		if out = serveAuth(h, authHTTPRequest("POST", "/auth/login/totp", `{}`, "203.0.113.1:1000", nil, "")); out.Code != 401 {
			t.Fatal("password quota blocked TOTP", out.Code)
		}
	}
	if out = serveAuth(h, authHTTPRequest("POST", "/auth/login/totp", `{}`, "203.0.113.1:1000", nil, "")); out.Code != 429 {
		t.Fatal(out.Code)
	}
	if out = serveAuth(h, authHTTPRequest("POST", "/auth/login/totp", `{}`, "198.51.100.1:1000", nil, "")); out.Code != 401 {
		t.Fatal(out.Code)
	}
}

func TestLoginNormalizesDirectIP(t *testing.T) {
	for _, addresses := range [][2]string{
		{"192.0.2.1:1000", "[::ffff:192.0.2.1]:2000"},
		{"[2001:db8::1]:1000", "[2001:0db8:0:0:0:0:0:1]:2000"},
	} {
		s, _ := testServer(t)
		h := s.Handler()
		for range 10 {
			serveAuth(h, authHTTPRequest("POST", "/auth/login", `{}`, addresses[0], nil, ""))
		}
		if out := serveAuth(h, authHTTPRequest("POST", "/auth/login", validLoginBody, addresses[1], nil, "")); out.Code != 429 {
			t.Fatal("equivalent addresses got separate limits", out.Code)
		}
	}
}

// Signal that the handler reached a cancellable lock wait without sleeps.
type authWaitContext struct {
	context.Context
	entered chan struct{}
	once    sync.Once
}

func (c *authWaitContext) Done() <-chan struct{} {
	c.once.Do(func() { close(c.entered) })
	return c.Context.Done()
}

func TestCanceledAuthWaitDoesNotCommitOrLeakSlots(t *testing.T) {
	for _, path := range []string{"/auth/login", "/auth/login/totp", "/auth/password"} {
		t.Run(path, func(t *testing.T) {
			s, _ := testServer(t)
			h := s.Handler()
			cookie, csrf := storedAuthSession(s)
			if !s.authMu.lock(context.Background()) {
				t.Fatal("cannot hold gate")
			}
			held := true
			defer func() {
				if held {
					s.authMu.unlock()
				}
			}()
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			observed := &authWaitContext{Context: ctx, entered: make(chan struct{})}
			body := validLoginBody
			if path == "/auth/login/totp" {
				body, _ = recoveryLoginBodies(t, s)
			} else if path == "/auth/password" {
				body = `{"current_password":"test-password-123","new_password":"` + serverNewPassword + `"}`
			}
			done := asyncAuth(h, authHTTPRequest("POST", path, body, "192.0.2.1:1000", cookie, csrf).WithContext(observed))
			awaitAuth(t, observed.entered)
			cancel()
			out := awaitAuth(t, done)
			if out.Header().Get("Set-Cookie") != "" {
				t.Fatal("canceled request altered cookies")
			}
			if !s.passwordWork.tryLock() {
				t.Fatal("execution slot leaked")
			}
			s.passwordWork.unlock()
			s.authMu.unlock()
			held = false
			if _, ok := s.Sessions.Get(cookie.Value); !ok {
				t.Fatal("canceled mutation revoked session")
			}
			if _, err := s.account().StartLogin("test-password-123"); err != nil {
				t.Fatal("canceled request changed password", err)
			}
			followPath, followBody := "/auth/login", validLoginBody
			if path == "/auth/login/totp" {
				if s.account().Security().RecoveryRemaining != 10 {
					t.Fatal("canceled TOTP consumed proof")
				}
				followPath, followBody = path, body
			}
			if out = serveAuth(h, authHTTPRequest("POST", followPath, followBody, "192.0.2.1:1000", nil, "")); out.Code != 200 {
				t.Fatal(out.Code, out.Body.String())
			}
			if len(out.Result().Cookies()) != 1 {
				t.Fatal("execution slot or proof was not reusable")
			}

		})
	}
}

func TestQueuedSecurityRequestRechecksSessionAndCSRF(t *testing.T) {
	for _, change := range []string{"revoke", "csrf"} {
		t.Run(change, func(t *testing.T) {
			s, _ := testServer(t)
			h := s.Handler()
			cookie, csrf := storedAuthSession(s)
			s.authMu.lock(context.Background())
			held := true
			defer func() {
				if held {
					s.authMu.unlock()
				}
			}()
			observed := &authWaitContext{Context: context.Background(), entered: make(chan struct{})}
			r := authHTTPRequest("POST", "/auth/totp/setup", `{"current_password":"test-password-123"}`, "192.0.2.1:1000", cookie, csrf).WithContext(observed)
			done := asyncAuth(h, r)
			awaitAuth(t, observed.entered)
			expected := 401
			if change == "revoke" {
				s.Sessions.DeleteAll()
			} else {
				// Middleware has finished. The transaction gate synchronizes this write
				// with the handler's recheck after it acquires the gate.
				r.Header.Set("X-CSRF-Token", "wrong")
				expected = 403
			}
			s.authMu.unlock()
			held = false
			out := awaitAuth(t, done)
			if out.Code != expected {
				t.Fatal(out.Code, out.Body.String())
			}
		})
	}
}

type authBlockingWriter struct {
	*httptest.ResponseRecorder
	*authBlock
}

func (w *authBlockingWriter) Write(p []byte) (int, error) {
	w.wait()
	return w.ResponseRecorder.Write(p)
}

type authBlockingAudit struct {
	LoginLogStore
	*authBlock
}

func (l *authBlockingAudit) Append(event logs.LoginEvent) error {
	// Block one event; subsequent requests still exercise the real audit store.
	first := false
	l.enterOnce.Do(func() { first = true; close(l.entered) })
	if first {
		<-l.release
	}
	return l.LoginLogStore.Append(event)
}

func TestLoginSlowOutputDoesNotHoldAuthOrExecutionGates(t *testing.T) {
	for _, stage := range []string{"password", "totp"} {
		for _, where := range []string{"response", "audit"} {
			t.Run(stage+"/"+where, func(t *testing.T) {
				s := serverWithLoginLog(t)
				h := s.Handler()
				cookie, csrf := storedAuthSession(s)
				firstBody, secondBody := validLoginBody, validLoginBody
				path := "/auth/login"
				if stage == "totp" {
					firstBody, secondBody = recoveryLoginBodies(t, s)
					path = "/auth/login/totp"
				}
				block := newAuthBlock()
				var w http.ResponseWriter = httptest.NewRecorder()
				if where == "response" {
					w = &authBlockingWriter{httptest.NewRecorder(), block}
				} else {
					s.LoginLog = &authBlockingAudit{s.LoginLog, block}
				}
				done := make(chan struct{})
				go func() {
					defer close(done)
					h.ServeHTTP(w, authHTTPRequest("POST", path, firstBody, "192.0.2.1:1000", nil, ""))
				}()
				t.Cleanup(func() { block.unblock(); awaitAuth(t, done) })
				awaitAuth(t, block.entered)
				out := awaitAuth(t, asyncAuth(h, authHTTPRequest("POST", "/auth/logout", "", "192.0.2.1:1000", cookie, csrf)))
				if out.Code != 204 {
					t.Fatal(out.Code, out.Body.String())
				}
				out = awaitAuth(t, asyncAuth(h, authHTTPRequest("POST", path, secondBody, "192.0.2.1:1000", nil, "")))
				if out.Code != 200 {
					t.Fatal("output held execution slot", out.Code, out.Body.String())
				}
			})
		}
	}
}

func TestAnonymousExecutionSlotsRejectWithoutQueueing(t *testing.T) {
	s, _ := testServer(t)
	h := s.Handler()
	if !s.passwordWork.tryLock() {
		t.Fatal("cannot hold password slot")
	}
	defer s.passwordWork.unlock()
	out := serveAuth(h, authHTTPRequest("POST", "/auth/login", validLoginBody, "192.0.2.1:1000", nil, ""))
	if out.Code != 429 || out.Header().Get("Retry-After") != "1" {
		t.Fatal(out.Code, out.Header())
	}
	out = serveAuth(h, authHTTPRequest("POST", "/auth/login/totp", `{}`, "192.0.2.1:1000", nil, ""))
	if out.Code != 401 {
		t.Fatal("password slot blocked TOTP", out.Code)
	}
	if !s.totpWork.tryLock() {
		t.Fatal("cannot hold TOTP slot")
	}
	out = serveAuth(h, authHTTPRequest("POST", "/auth/login/totp", `{}`, "192.0.2.1:1000", nil, ""))
	s.totpWork.unlock()
	if out.Code != 429 || out.Header().Get("Retry-After") != "1" {
		t.Fatal(out.Code, out.Header())
	}
	cookie, csrf := storedAuthSession(s)
	out = serveAuth(h, authHTTPRequest("POST", "/auth/logout", "", "192.0.2.1:1000", cookie, csrf))
	if out.Code != 204 {
		t.Fatal("anonymous work blocked logout", out.Code)
	}
}
