package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/azpanel/lnmp-gopanel/internal/auth"
	"github.com/pquerna/otp/totp"
)

const serverNewPassword = "N7!vQ2_zK9@pL5#sT8$xR4"

func authRequest(h http.Handler, method, path string, body any, cookie *http.Cookie, csrf string) *httptest.ResponseRecorder {
	data, _ := json.Marshal(body)
	return request(h, method, "/api/v1"+path, string(data), cookie, csrf, "")
}
func sessionFrom(t *testing.T, w *httptest.ResponseRecorder) (*http.Cookie, string) {
	t.Helper()
	if w.Code != 200 || len(w.Result().Cookies()) != 1 {
		t.Fatalf("expected session: %d %s", w.Code, w.Body.String())
	}
	var value struct {
		CSRF    string `json:"csrf_token"`
		Session struct {
			CSRF string `json:"csrf_token"`
		} `json:"session"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &value); err != nil {
		t.Fatal(err)
	}
	if value.CSRF == "" {
		value.CSRF = value.Session.CSRF
	}
	if value.CSRF == "" {
		t.Fatal("missing CSRF")
	}
	return w.Result().Cookies()[0], value.CSRF
}
func TestAccountPasswordAPIRevokesEverySession(t *testing.T) {
	s, _ := testServer(t)
	h := s.Handler()
	cookie, csrf := sessionFrom(t, authRequest(h, "POST", "/auth/login", map[string]string{"username": "admin", "password": "test-password-123"}, nil, ""))
	other, _ := s.Sessions.Create()
	for _, item := range []struct {
		body   any
		csrf   string
		status int
	}{
		{map[string]string{"current_password": "test-password-123", "new_password": serverNewPassword}, "", 403},
		{map[string]string{"current_password": "wrong", "new_password": serverNewPassword}, csrf, 403},
		{map[string]string{"current_password": "test-password-123", "new_password": "passwordpasswordpassword"}, csrf, 400},
		{map[string]string{"current_password": "test-password-123", "new_password": serverNewPassword, "extra": "bad"}, csrf, 400},
	} {
		w := authRequest(h, "POST", "/auth/password", item.body, cookie, item.csrf)
		if w.Code != item.status {
			t.Fatalf("got %d want %d: %s", w.Code, item.status, w.Body.String())
		}
	}
	w := request(h, "POST", "/api/v1/auth/password", `{"current_password":"test-password-123","new_password":"`+serverNewPassword+`"}`, cookie, csrf, "https://evil.example")
	if w.Code != 403 {
		t.Fatal("cross-origin password change accepted")
	}
	w = authRequest(h, "POST", "/auth/password", map[string]string{"current_password": "test-password-123", "new_password": serverNewPassword}, cookie, csrf)
	if w.Code != 204 {
		t.Fatal(w.Code, w.Body.String())
	}
	if _, ok := s.Sessions.Get(other); ok {
		t.Fatal("other session survived password change")
	}
	if authRequest(h, "GET", "/auth/session", nil, cookie, "").Code != 401 {
		t.Fatal("current session survived password change")
	}
	if authRequest(h, "POST", "/auth/login", map[string]string{"username": "admin", "password": "test-password-123"}, nil, "").Code != 401 {
		t.Fatal("old password accepted")
	}
	sessionFrom(t, authRequest(h, "POST", "/auth/login", map[string]string{"username": "admin", "password": serverNewPassword}, nil, ""))
}
func TestTOTPAPIEnrollmentAndRecoveryLogin(t *testing.T) {
	s, _ := testServer(t)
	h := s.Handler()
	cookie, csrf := sessionFrom(t, authRequest(h, "POST", "/auth/login", map[string]string{"username": "admin", "password": "test-password-123"}, nil, ""))
	other, _ := s.Sessions.Create()
	w := authRequest(h, "POST", "/auth/totp/setup", map[string]string{"current_password": "test-password-123"}, cookie, csrf)
	if w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	var setup struct {
		ID     string `json:"setup_id"`
		Secret string `json:"secret"`
		QR     string `json:"qr_code"`
	}
	json.Unmarshal(w.Body.Bytes(), &setup)
	if setup.Secret == "" || setup.QR == "" {
		t.Fatal("missing enrollment material")
	}
	code, _ := totp.GenerateCode(setup.Secret, time.Now())
	w = authRequest(h, "POST", "/auth/totp/confirm", map[string]string{"setup_id": setup.ID, "code": code}, cookie, csrf)
	var confirmation struct {
		Codes []string `json:"recovery_codes"`
	}
	json.Unmarshal(w.Body.Bytes(), &confirmation)
	if len(confirmation.Codes) != 10 {
		t.Fatal("missing recovery codes", w.Body.String())
	}
	nextCookie, nextCSRF := sessionFrom(t, w)
	if nextCookie.Value == cookie.Value || nextCSRF == csrf {
		t.Fatal("session not rotated")
	}
	if _, ok := s.Sessions.Get(other); ok {
		t.Fatal("other session not revoked")
	}
	if authRequest(h, "GET", "/auth/session", nil, cookie, "").Code != 401 {
		t.Fatal("old session accepted")
	}
	w = authRequest(h, "GET", "/auth/security", nil, nextCookie, "")
	if w.Code != 200 {
		t.Fatal(w.Code)
	}
	var security auth.Security
	json.Unmarshal(w.Body.Bytes(), &security)
	if !security.TOTPEnabled || security.RecoveryRemaining != 10 {
		t.Fatal("wrong security state")
	}
	if containsSecret(w.Body.String(), setup.Secret) || containsSecret(w.Body.String(), confirmation.Codes[0]) {
		t.Fatal("security status leaked secrets")
	}
	login := func() string {
		w := authRequest(h, "POST", "/auth/login", map[string]string{"username": "admin", "password": "test-password-123"}, nil, "")
		if w.Code != 200 || len(w.Result().Cookies()) != 0 {
			t.Fatal("partial login granted cookie", w.Code)
		}
		var c struct {
			Token string `json:"challenge_token"`
		}
		json.Unmarshal(w.Body.Bytes(), &c)
		if c.Token == "" {
			t.Fatal("missing challenge")
		}
		return c.Token
	}
	challenge := login()
	if authRequest(h, "POST", "/auth/login/totp", map[string]string{"challenge_token": challenge, "code": code}, nil, "").Code != 401 {
		t.Fatal("enrollment OTP reused")
	}
	w = authRequest(h, "POST", "/auth/login/totp", map[string]string{"challenge_token": challenge, "recovery_code": confirmation.Codes[0]}, nil, "")
	recoveredCookie, recoveredCSRF := sessionFrom(t, w)
	challenge = login()
	if authRequest(h, "POST", "/auth/login/totp", map[string]string{"challenge_token": challenge, "recovery_code": confirmation.Codes[0]}, nil, "").Code != 401 {
		t.Fatal("recovery code reused")
	}
	w = authRequest(h, "DELETE", "/auth/totp", map[string]string{"current_password": "test-password-123", "recovery_code": confirmation.Codes[1]}, recoveredCookie, recoveredCSRF)
	sessionFrom(t, w)
	sessionFrom(t, authRequest(h, "POST", "/auth/login", map[string]string{"username": "admin", "password": "test-password-123"}, nil, ""))
}
func containsSecret(value, secret string) bool {
	for i := 0; i+len(secret) <= len(value); i++ {
		if value[i:i+len(secret)] == secret {
			return true
		}
	}
	return false
}
func TestSecurityAuthenticationValidationAndLimits(t *testing.T) {
	for _, path := range []string{"/auth/password", "/auth/totp/setup", "/auth/totp/confirm", "/auth/totp/recovery-codes"} {
		t.Run(path, func(t *testing.T) {
			s, _ := testServer(t)
			h := s.Handler()
			if authRequest(h, "POST", path, map[string]string{}, nil, "").Code != 401 {
				t.Fatal("unauthenticated write allowed")
			}
		})
	}
	s, _ := testServer(t)
	h := s.Handler()
	id, v := s.Sessions.Create()
	cookie := &http.Cookie{Name: auth.CookieName, Value: id}
	for range 10 {
		authRequest(h, "POST", "/auth/totp/setup", map[string]string{"current_password": "wrong"}, cookie, v.CSRF)
	}
	w := authRequest(h, "POST", "/auth/totp/setup", map[string]string{"current_password": "test-password-123"}, cookie, v.CSRF)
	if w.Code != 429 || w.Header().Get("Retry-After") == "" {
		t.Fatal("security limiter missing")
	}
}
func TestFailedAuthPersistenceDoesNotCreateSession(t *testing.T) {
	s, _ := testServer(t)
	dir := filepath.Join(t.TempDir(), "auth")
	account, err := auth.OpenAccount(dir, s.Config.Username, s.Config.PasswordHash)
	if err != nil {
		t.Fatal(err)
	}
	defer account.Close()
	s.Account = account
	setup, err := account.StartSetup("setup", "test-password-123")
	if err != nil {
		t.Fatal(err)
	}
	code, _ := totp.GenerateCode(setup.Secret, time.Now())
	codes, err := account.ConfirmSetup("setup", setup.ID, code)
	if err != nil {
		t.Fatal(err)
	}
	h := s.Handler()
	w := authRequest(h, "POST", "/auth/login", map[string]string{"username": "admin", "password": "test-password-123"}, nil, "")
	var challenge struct {
		Token string `json:"challenge_token"`
	}
	json.Unmarshal(w.Body.Bytes(), &challenge)
	// A file permission error is deterministic even when the tests run as root.
	if err = os.Chmod(filepath.Join(dir, "auth-state.json"), 0644); err != nil {
		t.Fatal(err)
	}
	w = authRequest(h, "POST", "/auth/login/totp", map[string]string{"challenge_token": challenge.Token, "recovery_code": codes[0]}, nil, "")
	if w.Code != 503 {
		t.Fatal("failed save granted login", w.Code, w.Body.String())
	}
	for _, cookie := range w.Result().Cookies() {
		if cookie.Value != "" {
			t.Fatal("session granted after write failure")
		}
	}
}
