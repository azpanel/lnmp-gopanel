package server

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/azpanel/lnmp-gopanel/internal/auth"
	"github.com/azpanel/lnmp-gopanel/internal/vhosts"
)

type fakeRenewProvisioner struct{ fakeProvisioner }

func (*fakeRenewProvisioner) PreviewRenewal(_ context.Context, names []string) ([]vhosts.RenewPreview, error) {
	out := []vhosts.RenewPreview{}
	for _, name := range names {
		out = append(out, vhosts.RenewPreview{Name: name, Domains: []string{"example.com"}, Provider: "zerossl", Eligible: true})
	}
	return out, nil
}
func (p *fakeRenewProvisioner) RenewVhost(ctx context.Context, _ string, item vhosts.RenewItem, emit func(vhosts.CreateEvent)) error {
	p.calls.Add(1)
	emit(vhosts.CreateEvent{Phase: "ssl", Line: "order for " + item.Name})
	select {
	case <-p.release:
	case <-ctx.Done():
		return ctx.Err()
	}
	if item.Provider == "zerossl" {
		return errors.New("first failed")
	}
	return nil
}
func TestRenewalAPIAndSSE(t *testing.T) {
	s, _ := testServer(t)
	p := &fakeRenewProvisioner{fakeProvisioner{release: make(chan struct{})}}
	s.VhostJobs = vhosts.NewJobs(context.Background(), p, nil)
	h := s.Handler()
	id, session := s.Sessions.Create()
	cookie := &http.Cookie{Name: auth.CookieName, Value: id}
	for _, path := range []string{"/api/v1/vhosts/ssl/renew/preview", "/api/v1/vhosts/ssl/renew"} {
		if w := request(h, "POST", path, "{}", nil, "", ""); w.Code != 401 {
			t.Fatal("auth missing", w.Code)
		}
		if w := request(h, "POST", path, "{}", cookie, "", ""); w.Code != 403 {
			t.Fatal("csrf missing", w.Code)
		}
	}
	preview := `{"names":["first.conf","second.conf"]}`
	w := request(h, "POST", "/api/v1/vhosts/ssl/renew/preview", preview, cookie, session.CSRF, "")
	if w.Code != 200 || !strings.Contains(w.Body.String(), `"eligible":true`) {
		t.Fatal(w.Code, w.Body.String())
	}
	body := `{"request_id":"renew-test-123","items":[{"name":"first.conf","provider":"zerossl","email":"a@example.com"},{"name":"second.conf","provider":"letsencrypt","email":"b@example.com"}]}`
	for _, bad := range []string{`{}`, strings.Replace(body, "zerossl", "other", 1), strings.Replace(body, "first.conf", "../first.conf", 1), strings.Replace(body, "second.conf", "first.conf", 1), strings.Replace(body, "a@example.com", "bad", 1), strings.TrimSuffix(body, "}") + `,"unknown":true}`} {
		if w = request(h, "POST", "/api/v1/vhosts/ssl/renew", bad, cookie, session.CSRF, ""); w.Code != 400 {
			t.Fatal("bad accepted", w.Code, w.Body.String())
		}
	}
	for range 2 {
		if w = request(h, "POST", "/api/v1/vhosts/ssl/renew", body, cookie, session.CSRF, ""); w.Code != 202 {
			t.Fatal(w.Code, w.Body.String())
		}
	}
	if w = request(h, "POST", "/api/v1/vhosts/ssl/renew", strings.Replace(body, "renew-test-123", "renew-another", 1), cookie, session.CSRF, ""); w.Code != 409 {
		t.Fatal("concurrent batch accepted", w.Code)
	}
	close(p.release)
	deadline := time.Now().Add(time.Second)
	for {
		j, _ := s.VhostJobs.Get("renew-test-123")
		if j.Status != "running" {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("task stuck")
		}
		time.Sleep(time.Millisecond)
	}
	if p.calls.Load() != 2 {
		t.Fatal("duplicate executed", p.calls.Load())
	}
	w = request(h, "GET", "/api/v1/vhosts/jobs/renew-test-123", "", cookie, "", "")
	var job vhosts.Job
	if err := json.Unmarshal(w.Body.Bytes(), &job); err != nil {
		t.Fatal(err)
	}
	if job.Completed != 2 || job.Failed != 1 || job.Succeeded != 1 || job.Kind != "ssl-renew" {
		t.Fatal(w.Body.String())
	}
	r := httptest.NewRequest("GET", "/api/v1/vhosts/jobs/renew-test-123/events", nil)
	r.AddCookie(cookie)
	r.Header.Set("Last-Event-ID", "1")
	w = httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if !strings.Contains(w.Body.String(), "event:complete") || !strings.Contains(w.Body.String(), `"completed":2`) || strings.Contains(w.Body.String(), "开始提前续签：first.conf") {
		t.Fatal("bad replay", w.Body.String())
	}
}
