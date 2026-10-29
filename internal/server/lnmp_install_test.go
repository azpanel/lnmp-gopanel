package server

import (
	"context"
	"errors"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/azpanel/lnmp-gopanel/internal/lnmpinstall"
)

type installStub struct {
	calls    int
	conflict bool
	reads    []lnmpinstall.ReadRequest
}

const installTestID = "11111111-1111-4111-8111-111111111111"

func (b *installStub) InstallInspect(context.Context) (lnmpinstall.Snapshot, error) {
	return lnmpinstall.Snapshot{Blocked: []string{}, Job: &lnmpinstall.Job{ID: installTestID, Status: "ready"}}, nil
}
func (b *installStub) InstallPrepare(context.Context, string) (lnmpinstall.Job, error) {
	b.calls++
	if b.conflict {
		return lnmpinstall.Job{}, lnmpinstall.ErrConflict
	}
	return lnmpinstall.Job{ID: installTestID, Status: "preparing"}, nil
}
func (b *installStub) InstallStart(context.Context, lnmpinstall.Request) (lnmpinstall.Job, error) {
	b.calls++
	return lnmpinstall.Job{}, errors.New("invalid options")
}
func (b *installStub) InstallRead(_ context.Context, r lnmpinstall.ReadRequest) (lnmpinstall.LogPage, error) {
	b.reads = append(b.reads, r)
	if r.After == 0 {
		return lnmpinstall.LogPage{Cursor: 100, Lines: []lnmpinstall.Line{{ID: 100, Text: "compile output", Time: time.Now()}}}, nil
	}
	return lnmpinstall.LogPage{Cursor: r.After, Lines: []lnmpinstall.Line{}}, nil
}
func TestInstallationAuthenticationAndStreams(t *testing.T) {
	s, _ := testServer(t)
	b := &installStub{}
	s.Installation = b
	h := s.Handler()
	if w := request(h, "GET", "/api/v1/lnmp-installation", "", nil, "", ""); w.Code != 401 {
		t.Fatal(w.Code)
	}
	w := request(h, "POST", "/api/v1/auth/login", `{"username":"admin","password":"test-password-123"}`, nil, "", "")
	cookies := w.Result().Cookies()
	if len(cookies) == 0 {
		t.Fatal(w.Body.String())
	}
	cookie := cookies[0]
	session, _ := s.Sessions.Get(cookie.Value)
	path := "/api/v1/lnmp-installation/prepare"
	body := `{"id":"` + installTestID + `"}`
	if w = request(h, "POST", path, body, cookie, "", ""); w.Code != 403 || b.calls != 0 {
		t.Fatal("CSRF did not stop mutation")
	}
	if w = request(h, "POST", path, body, cookie, session.CSRF, ""); w.Code != 202 || b.calls != 1 {
		t.Fatal(w.Code, w.Body.String())
	}
	b.conflict = true
	if w = request(h, "POST", path, body, cookie, session.CSRF, ""); w.Code != 409 {
		t.Fatal(w.Code)
	}
	if w = request(h, "POST", path, `{"id":"../../etc/shadow"}`, cookie, session.CSRF, ""); w.Code != 400 {
		t.Fatal(w.Code)
	}
	if w = request(h, "GET", "/api/v1/lnmp-installation/jobs/"+installTestID+"/logs?after=-1", "", cookie, "", ""); w.Code != 400 {
		t.Fatal(w.Code)
	}
	logsPath := "/api/v1/lnmp-installation/jobs/" + installTestID + "/logs"
	for _, query := range []string{"?tail=invalid", "?tail=1&after=100", "?tail=1&download=1"} {
		if w = request(h, "GET", logsPath+query, "", cookie, "", ""); w.Code != 400 || len(b.reads) != 0 {
			t.Fatal(query, w.Code, w.Body.String())
		}
	}
	if w = request(h, "GET", logsPath+"?tail=1", "", cookie, "", ""); w.Code != 200 || len(b.reads) != 1 || !b.reads[0].Tail {
		t.Fatal(w.Code, w.Body.String(), b.reads)
	}
	if w = request(h, "GET", logsPath+"?after=100", "", cookie, "", ""); w.Code != 200 || b.reads[1].Tail || b.reads[1].After != 100 {
		t.Fatal(w.Code, w.Body.String(), b.reads)
	}
	if w = request(h, "GET", "/api/v1/lnmp-installation/jobs/"+installTestID+"/events", "", cookie, "", ""); w.Code != 200 || !strings.Contains(w.Body.String(), "compile output") || !strings.Contains(w.Body.String(), "event:complete") {
		t.Fatal(w.Code, w.Body.String())
	}
	r := httptest.NewRequest("GET", "/api/v1/lnmp-installation/jobs/"+installTestID+"/events", nil)
	r.AddCookie(cookie)
	r.Header.Set("Last-Event-ID", "100")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, r)
	if strings.Contains(rec.Body.String(), "compile output") {
		t.Fatal("reconnect duplicated output")
	}
}
