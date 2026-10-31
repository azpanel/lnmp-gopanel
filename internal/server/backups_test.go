package server

import (
	"context"
	"errors"
	"github.com/azpanel/lnmp-gopanel/internal/auth"
	"github.com/azpanel/lnmp-gopanel/internal/backups"
	"net/http"
	"strings"
	"testing"
)

type backupBackend struct {
	calls int
	err   error
}

func (b *backupBackend) BackupComponents(context.Context) (backups.Snapshot, error) {
	b.calls++
	return backups.Snapshot{Items: []backups.Item{{Name: "gdrive", Status: "ready", Version: "3.9.1", Supported: true}}}, b.err
}
func (b *backupBackend) InstallBackupComponents(_ context.Context, r backups.Request) (backups.Job, error) {
	b.calls++
	return backups.Job{ID: "test-job", Status: "running"}, b.err
}
func TestBackupAPIAuthValidationAndErrors(t *testing.T) {
	s, _ := testServer(t)
	b := &backupBackend{}
	s.Backups = b
	h := s.Handler()
	get := "/api/v1/backups/components"
	post := get + "/install"
	for _, tc := range []struct{ method, path string }{{"GET", get}, {"POST", post}} {
		if w := request(h, tc.method, tc.path, `{"components":["gdrive"]}`, nil, "", ""); w.Code != 401 {
			t.Fatal(w.Code, w.Body.String())
		}
	}
	id, session := s.Sessions.Create()
	cookie := &http.Cookie{Name: auth.CookieName, Value: id}
	if w := request(h, "POST", post, `{"components":["gdrive"]}`, cookie, "", ""); w.Code != 403 {
		t.Fatal(w.Code)
	}
	if b.calls != 0 {
		t.Fatal("unauthorized helper call")
	}
	for _, body := range []string{`{}`, `{"components":[]}`, `{"components":["bash"]}`, `{"components":["gdrive","gdrive"]}`, `{"components":["gdrive"],"url":"evil"}`, `{"components":["gdrive"]} {}`} {
		w := request(h, "POST", post, body, cookie, session.CSRF, "")
		if w.Code != 400 {
			t.Fatal(body, w.Code, w.Body.String())
		}
	}
	if b.calls != 0 {
		t.Fatal("invalid request reached helper")
	}
	if w := request(h, "GET", get, "", cookie, "", ""); w.Code != 200 || !strings.Contains(w.Body.String(), `"version":"3.9.1"`) {
		t.Fatal(w.Code, w.Body.String())
	}
	if w := request(h, "POST", post, `{"components":["gdrive","rclone"]}`, cookie, session.CSRF, ""); w.Code != 202 {
		t.Fatal(w.Code, w.Body.String())
	}
	for _, tc := range []struct {
		err    error
		status int
	}{{backups.ErrBusy, 409}, {backups.ErrInvalid, 400}, {errors.New("network failed"), 502}} {
		b.err = tc.err
		if w := request(h, "POST", post, `{"components":["gdrive"]}`, cookie, session.CSRF, ""); w.Code != tc.status {
			t.Fatal(w.Code, w.Body.String())
		}
	}
}
