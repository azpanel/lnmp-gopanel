package server

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"testing"

	"github.com/azpanel/lnmp-gopanel/internal/auth"
	"github.com/azpanel/lnmp-gopanel/internal/cron"
)

type stubCron struct {
	err   error
	calls int
	raw   string
}

type writableCron struct {
	stubCron
	writes int
	view   cron.Snapshot
	err    error
}

func (s *writableCron) ToggleCron(_ context.Context, input cron.ToggleRequest) (cron.Snapshot, error) {
	s.writes++
	if s.err != nil {
		return cron.Snapshot{}, s.err
	}
	raw, err := cron.Toggle(s.view, input)
	if err != nil {
		return cron.Snapshot{}, err
	}
	s.view = cron.Parse(raw)
	return s.view, nil
}

func TestCronToggleAuthenticationValidationAndConflict(t *testing.T) {
	s, _ := testServer(t)
	writer := &writableCron{view: cron.Parse("@daily /root/backup\n")}
	s.Cron = writer
	h := s.Handler()
	path := "/api/v1/cron/toggle"
	id, session := s.Sessions.Create()
	cookie := &http.Cookie{Name: auth.CookieName, Value: id}
	body := `{"line":1,"enabled":false,"revision":"` + writer.view.Revision + `"}`
	if w := request(h, "POST", path, body, nil, "", ""); w.Code != 401 || writer.writes != 0 {
		t.Fatal("toggle must require authentication")
	}
	for _, origin := range []string{"", "https://evil.example"} {
		csrf := ""
		if origin != "" {
			csrf = session.CSRF
		}
		if w := request(h, "POST", path, body, cookie, csrf, origin); w.Code != 403 || writer.writes != 0 {
			t.Fatal("toggle must require CSRF protection")
		}
	}
	for _, invalid := range []string{
		`{}`, `{"line":1,"enabled":false}`, `{"line":0,"enabled":false,"revision":"` + writer.view.Revision + `"}`,
		strings.Replace(body, `,"enabled":false`, "", 1),
		strings.Replace(body, `"line":1`, `"line":1,"command":"/root/other"`, 1),
		body + `{}`, body + strings.Repeat(" ", 4096),
	} {
		if w := request(h, "POST", path, invalid, cookie, session.CSRF, ""); w.Code != 400 || writer.writes != 0 {
			t.Fatalf("invalid request accepted: %d %s", w.Code, w.Body.String())
		}
	}
	w := request(h, "POST", path, body, cookie, session.CSRF, "")
	var view cron.Snapshot
	if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &view) != nil || view.Tasks[0].Enabled || view.Raw != "#@daily /root/backup\n" || w.Header().Get("Cache-Control") != "no-store" {
		t.Fatalf("disable failed: %d %s", w.Code, w.Body.String())
	}
	if w := request(h, "POST", path, body, cookie, session.CSRF, ""); w.Code != 409 {
		t.Fatal("stale revision accepted", w.Code)
	}
	body = `{"line":1,"enabled":true,"revision":"` + view.Revision + `"}`
	if w := request(h, "POST", path, body, cookie, session.CSRF, ""); w.Code != 200 || writer.view.Raw != "@daily /root/backup\n" {
		t.Fatalf("enable failed: %d %s", w.Code, w.Body.String())
	}
	writer.err = errors.New("保存失败")
	if w := request(h, "POST", path, body, cookie, session.CSRF, ""); w.Code != 503 || !strings.Contains(w.Body.String(), "保存失败") {
		t.Fatal("write failure hidden", w.Code)
	}
	s.Cron = &stubCron{}
	if w := request(h, "POST", path, body, cookie, session.CSRF, ""); w.Code != 503 {
		t.Fatal("read-only provider accepted write", w.Code)
	}
}

func (s *stubCron) Cron(context.Context) (cron.Snapshot, error) {
	s.calls++
	if s.raw != "" {
		return cron.Parse(s.raw), s.err
	}
	return cron.Parse("@daily /root/backup\n"), s.err
}

func TestCronEndpoint(t *testing.T) {
	s, _ := testServer(t)
	reader := &stubCron{}
	s.Cron = reader
	h := s.Handler()
	w := request(h, "GET", "/api/v1/cron", "", nil, "", "")
	if w.Code != 401 || reader.calls != 0 {
		t.Fatal("cron must require authentication")
	}
	id, _ := s.Sessions.Create()
	cookie := &http.Cookie{Name: auth.CookieName, Value: id}
	w = request(h, "GET", "/api/v1/cron", "", cookie, "", "")
	var view cron.Snapshot
	if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &view) != nil || len(view.Tasks) != 1 || view.Raw != "@daily /root/backup\n" || view.UpdatedAt.IsZero() || w.Header().Get("Cache-Control") != "no-store" {
		t.Fatalf("invalid response: %d %s", w.Code, w.Body.String())
	}
	reader.err = errors.New("本机未安装 crontab")
	w = request(h, "GET", "/api/v1/cron", "", cookie, "", "")
	if w.Code != 503 || !strings.Contains(w.Body.String(), reader.err.Error()) {
		t.Fatalf("failure hidden: %d %s", w.Code, w.Body.String())
	}
	s.Cron = nil
	w = request(h, "GET", "/api/v1/cron", "", cookie, "", "")
	if w.Code != 503 {
		t.Fatal(w.Code)
	}
}

func TestCronPreviewEndpoint(t *testing.T) {
	s, _ := testServer(t)
	reader := &stubCron{}
	s.Cron = reader
	h := s.Handler()
	if w := request(h, "GET", "/api/v1/cron/preview?line=1", "", nil, "", ""); w.Code != 401 || reader.calls != 0 {
		t.Fatal("preview must require authentication")
	}
	id, _ := s.Sessions.Create()
	cookie := &http.Cookie{Name: auth.CookieName, Value: id}
	for _, line := range []string{"", "0", "-1", "abc", "9999999999999999999999"} {
		if w := request(h, "GET", "/api/v1/cron/preview?line="+line, "", cookie, "", ""); w.Code != 400 {
			t.Fatalf("invalid line accepted: %q %d", line, w.Code)
		}
	}
	w := request(h, "GET", "/api/v1/cron/preview?line=1", "", cookie, "", "")
	var preview cron.Preview
	if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &preview) != nil || len(preview.Times) != 5 || preview.Line != 1 || preview.Schedule != "@daily" || w.Header().Get("Cache-Control") != "no-store" {
		t.Fatalf("invalid preview: %d %s", w.Code, w.Body.String())
	}
	if w := request(h, "GET", "/api/v1/cron/preview?line=2", "", cookie, "", ""); w.Code != 404 {
		t.Fatal("missing task must require a refresh")
	}
	reader.raw = "#@daily /root/backup\n"
	w = request(h, "GET", "/api/v1/cron/preview?line=1", "", cookie, "", "")
	if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &preview) != nil || len(preview.Times) != 0 || preview.Message != "任务已关闭，不会执行。" {
		t.Fatalf("disabled task has execution times: %d %s", w.Code, w.Body.String())
	}
	reader.err = errors.New("读取失败")
	if w := request(h, "GET", "/api/v1/cron/preview?line=1", "", cookie, "", ""); w.Code != 503 {
		t.Fatal("read failure hidden")
	}
}
