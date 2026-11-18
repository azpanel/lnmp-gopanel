package server

import (
	"context"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/azpanel/lnmp-gopanel/internal/auth"
	"github.com/azpanel/lnmp-gopanel/internal/metrics"
)

type trafficStub func(context.Context, string) (*metrics.Traffic, error)

func (f trafficStub) Read(ctx context.Context, period string) (*metrics.Traffic, error) {
	return f(ctx, period)
}

func TestDashboardTrafficAuthPeriodsAndUnavailable(t *testing.T) {
	s, _ := testServer(t)
	var period string
	s.Traffic = trafficStub(func(_ context.Context, value string) (*metrics.Traffic, error) {
		period = value
		return &metrics.Traffic{Interface: "eth0", Period: value, Points: []metrics.TrafficPoint{}}, nil
	})
	h := s.Handler()
	path := "/api/v1/dashboard/traffic"
	if w := request(h, "GET", path, "", nil, "", ""); w.Code != 401 {
		t.Fatal("missing auth", w.Code)
	}
	id, _ := s.Sessions.Create()
	cookie := &http.Cookie{Name: auth.CookieName, Value: id}
	for _, value := range []string{"", "day", "hour"} {
		w := request(h, "GET", path+"?period="+value, "", cookie, "", "")
		if value == "" {
			if w.Code != 400 {
				t.Fatal("explicit empty period accepted", w.Code)
			}
			continue
		}
		if w.Code != 200 || period != value || !strings.Contains(w.Body.String(), `"interface":"eth0"`) {
			t.Fatal(w.Code, w.Body.String(), period)
		}
	}
	if w := request(h, "GET", path, "", cookie, "", ""); w.Code != 200 || period != "day" {
		t.Fatal("default", w.Code, period)
	}
	if w := request(h, "GET", path+"?period=month", "", cookie, "", ""); w.Code != 400 {
		t.Fatal(w.Code)
	}
	s.Traffic = trafficStub(func(context.Context, string) (*metrics.Traffic, error) { return nil, errors.New("vnstat 未安装") })
	if w := request(h, "GET", path, "", cookie, "", ""); w.Code != 503 || !strings.Contains(w.Body.String(), "vnstat 未安装") {
		t.Fatal(w.Code, w.Body.String())
	}
	s.Traffic = nil
	if w := request(h, "GET", path, "", cookie, "", ""); w.Code != 503 {
		t.Fatal(w.Code)
	}
}

func TestDashboardWidgetsAuthPersistenceAndErrors(t *testing.T) {
	s, _ := testServer(t)
	dir := filepath.Join(t.TempDir(), "state")
	var err error
	s.Widgets, err = metrics.OpenWidgetStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	h := s.Handler()
	path := "/api/v1/dashboard/widgets"
	id, session := s.Sessions.Create()
	cookie := &http.Cookie{Name: auth.CookieName, Value: id}
	for _, method := range []string{"GET", "PUT"} {
		if w := request(h, method, path, `{"enabled":[]}`, nil, "", ""); w.Code != 401 {
			t.Fatal("missing auth", w.Code)
		}
	}
	for _, token := range []string{"", "wrong"} {
		if w := request(h, "PUT", path, `{"enabled":[]}`, cookie, token, ""); w.Code != 403 {
			t.Fatal("missing CSRF", w.Code)
		}
	}
	if w := request(h, "PUT", path, `{"enabled":[]}`, cookie, session.CSRF, "https://evil.example"); w.Code != 403 {
		t.Fatal("cross origin accepted")
	}
	for _, body := range []string{`{}`, `null`, `{"enabled":null}`, `{"enabled":["cpu"]}`, `{"enabled":["load","load"]}`, `{"enabled":[],"password":"secret"}`, `{"enabled":[]} {}`} {
		if w := request(h, "PUT", path, body, cookie, session.CSRF, ""); w.Code != 400 {
			t.Fatal("bad selection accepted", body, w.Code)
		}
	}
	if w := request(h, "PUT", path, `{"enabled":["nginx","load"]}`, cookie, session.CSRF, ""); w.Code != 200 {
		t.Fatal(w.Body.String())
	}
	if w := request(h, "GET", path, "", cookie, "", ""); w.Code != 200 || !strings.Contains(w.Body.String(), `["load","nginx"]`) {
		t.Fatal(w.Body.String())
	}
	restored, e := metrics.OpenWidgetStore(dir)
	if e != nil || len(restored.Get().Enabled) != 2 {
		t.Fatal("restart did not restore selection", e)
	}
	if w := request(h, "PUT", path, `{"enabled":[]}`, cookie, session.CSRF, ""); w.Code != 200 {
		t.Fatal(w.Body.String())
	}
	if e = os.Remove(filepath.Join(dir, "dashboard-widgets.json")); e != nil {
		t.Fatal(e)
	}
	if e = os.Mkdir(filepath.Join(dir, "dashboard-widgets.json"), 0700); e != nil {
		t.Fatal(e)
	}
	if w := request(h, "PUT", path, `{"enabled":["load"]}`, cookie, session.CSRF, ""); w.Code != 503 || len(s.Widgets.Get().Enabled) != 0 {
		t.Fatal("save failure changed state", w.Code)
	}
	s.Widgets = nil
	if w := request(h, "GET", path, "", cookie, "", ""); w.Code != 503 {
		t.Fatal("unavailable store not reported")
	}
}
