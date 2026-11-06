package server

import (
	"net/http"
	"strings"
	"testing"

	"github.com/azpanel/lnmp-gopanel/internal/auth"
	"github.com/azpanel/lnmp-gopanel/internal/backups"
)

func TestBackupNotificationTestRoutePermissionsValidationAndLimit(t *testing.T) {
	s, _ := testServer(t)
	b := &policyTestBackend{}
	s.BackupPolicies = b
	h := s.Handler()
	id, session := s.Sessions.Create()
	cookie := &http.Cookie{Name: auth.CookieName, Value: id}
	endpoint := "/api/v1/backups/notifications/test"
	body := `{"notification":{"enabled":false,"channel":"telegram","uid":"123"},"token_action":"replace","token":"123:abcdefghijklmnopqrstuv_0123456789"}`
	if w := request(h, "POST", endpoint, body, nil, "", ""); w.Code != 401 {
		t.Fatal(w.Code)
	}
	if w := request(h, "POST", endpoint, body, cookie, "", ""); w.Code != 403 {
		t.Fatal(w.Code)
	}
	for _, invalid := range []string{strings.Replace(body, `"uid":"123"`, `"uid":"@name"`, 1), strings.Replace(body, `"telegram"`, `"custom"`, 1), strings.Replace(body, `"notification":`, `"url":"http://localhost/", "notification":`, 1), strings.Replace(body, `"notification":`, `"id":"bad", "revision":1,"notification":`, 1)} {
		if w := request(h, "POST", endpoint, invalid, cookie, session.CSRF, ""); w.Code != 400 {
			t.Fatal(w.Code, w.Body.String())
		}
	}
	if b.calls != 0 {
		t.Fatal("invalid/unauthorized request called helper")
	}
	for i := 0; i < 10; i++ {
		w := request(h, "POST", endpoint, body, cookie, session.CSRF, "")
		if w.Code != 200 || strings.Contains(w.Body.String(), "abcdefghijklmnop") {
			t.Fatal(w.Code, w.Body.String())
		}
	}
	input := b.input.(backups.NotificationTestRequest)
	if b.action != "notification-test" || input.Notification.UID != "123" || input.TokenAction != "replace" {
		t.Fatal("incorrect helper action")
	}
	w := request(h, "POST", endpoint, body, cookie, session.CSRF, "")
	if w.Code != 429 || w.Header().Get("Retry-After") == "" || b.calls != 10 {
		t.Fatal("notification test not limited", w.Code, b.calls)
	}
	b.err = backups.ErrConflict
	s.notificationLimiter = auth.Limiter{}
	if w := request(h, "POST", endpoint, body, cookie, session.CSRF, ""); w.Code != 409 {
		t.Fatal("revision conflict mapping", w.Code)
	}
}
