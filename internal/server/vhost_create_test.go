package server

import (
	"context"
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/azpanel/lnmp-gopanel/internal/auth"
	"github.com/azpanel/lnmp-gopanel/internal/vhosts"
)

type fakeProvisioner struct {
	calls   atomic.Int32
	release chan struct{}
}

func (*fakeProvisioner) VhostOptions(context.Context) (vhosts.CreateOptions, error) {
	return vhosts.CreateOptions{PHP: []vhosts.PHPRuntime{}, Certificates: []vhosts.CertificateOption{}, Rewrites: []string{"none"}, ACMEAvailable: true}, nil
}
func (p *fakeProvisioner) CreateVhost(ctx context.Context, r vhosts.CreateRequest, emit func(vhosts.CreateEvent)) (vhosts.CreateResult, error) {
	p.calls.Add(1)
	emit(vhosts.CreateEvent{Phase: "ssl", Line: "first output"})
	emit(vhosts.CreateEvent{Phase: "ssl", Line: "second output"})
	select {
	case <-p.release:
	case <-ctx.Done():
		return vhosts.CreateResult{}, ctx.Err()
	}
	return vhosts.CreateResult{Name: r.Name, SiteCreated: true, SSL: "failed"}, errors.New("ACME validation failed")
}

func TestVhostCreationAPIAuthenticationValidationAndEvents(t *testing.T) {
	s, _ := testServer(t)
	p := &fakeProvisioner{release: make(chan struct{})}
	s.VhostJobs = vhosts.NewJobs(context.Background(), p, nil)
	s.DNS = &vhosts.DNSChecker{Discover: func(context.Context) ([]string, []string) { return []string{"192.0.2.1"}, nil }, Lookup: func(context.Context, string) ([]net.IPAddr, error) {
		return []net.IPAddr{{IP: net.ParseIP("192.0.2.1")}}, nil
	}}
	h := s.Handler()
	id, session := s.Sessions.Create()
	cookie := &http.Cookie{Name: auth.CookieName, Value: id}
	for _, path := range []string{"/vhosts/options", "/vhosts/jobs", "/vhosts/jobs/missing", "/vhosts/jobs/missing/events"} {
		if w := request(h, "GET", "/api/v1"+path, "", nil, "", ""); w.Code != 401 {
			t.Fatal("private endpoint exposed", path, w.Code)
		}
	}
	for _, path := range []string{"/vhosts", "/vhosts/ssl/retry", "/vhosts/check-dns"} {
		if w := request(h, "POST", "/api/v1"+path, "{}", cookie, "", ""); w.Code != 403 {
			t.Fatal("CSRF missing", path, w.Code)
		}
	}
	if w := request(h, "GET", "/api/v1/vhosts/options", "", cookie, "", ""); w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	if w := request(h, "POST", "/api/v1/vhosts/check-dns", `{"domains":["example.com"]}`, cookie, session.CSRF, ""); w.Code != 200 || !strings.Contains(w.Body.String(), `"matched"`) {
		t.Fatal(w.Code, w.Body.String())
	}
	for _, body := range []string{`{"request_id":"request-1234","domain":"example.com;bad","ssl":{"mode":"none"}}`, `{"request_id":"request-1234","domain":"example.com","ssl":{"mode":"none"},"unknown":true}`, `{} {}`} {
		if w := request(h, "POST", "/api/v1/vhosts", body, cookie, session.CSRF, ""); w.Code != 400 {
			t.Fatal("invalid body accepted", body, w.Code)
		}
	}
	body := `{"request_id":"request-1234","domain":"example.com","php":"none","ssl":{"mode":"letsencrypt","email":"admin@example.com"}}`
	for range 2 {
		if w := request(h, "POST", "/api/v1/vhosts", body, cookie, session.CSRF, ""); w.Code != 202 {
			t.Fatal(w.Code, w.Body.String())
		}
	}
	if w := request(h, "POST", "/api/v1/vhosts", strings.Replace(body, "request-1234", "request-5678", 1), cookie, session.CSRF, ""); w.Code != 409 {
		t.Fatal("concurrent task accepted", w.Code)
	}
	close(p.release)
	deadline := time.Now().Add(time.Second)
	for {
		j, _ := s.VhostJobs.Get("request-1234")
		if j.Status != "running" {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("task did not finish")
		}
		time.Sleep(time.Millisecond)
	}
	if p.calls.Load() != 1 {
		t.Fatal("duplicate executed")
	}
	w := request(h, "GET", "/api/v1/vhosts/jobs/request-1234", "", cookie, "", "")
	var job vhosts.Job
	if json.Unmarshal(w.Body.Bytes(), &job) != nil || job.Status != "partial" || !job.Result.SiteCreated {
		t.Fatal(w.Body.String())
	}
	r := httptest.NewRequest("GET", "/api/v1/vhosts/jobs/request-1234/events", nil)
	r.AddCookie(cookie)
	r.Header.Set("Last-Event-ID", "1")
	w = httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != 200 || !strings.HasPrefix(w.Header().Get("Content-Type"), "text/event-stream") || strings.Contains(w.Body.String(), "first output") || !strings.Contains(w.Body.String(), "second output") || !strings.Contains(w.Body.String(), "event:complete") {
		t.Fatal("invalid replay", w.Code, w.Body.String())
	}
	if w = request(h, "GET", "/api/v1/vhosts/jobs/missing", "", cookie, "", ""); w.Code != 404 {
		t.Fatal(w.Code)
	}
}
