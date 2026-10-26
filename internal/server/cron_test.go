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
}

func (s *stubCron) Cron(context.Context) (cron.Snapshot, error) {
	s.calls++
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
	reader.err = errors.New("读取失败")
	if w := request(h, "GET", "/api/v1/cron/preview?line=1", "", cookie, "", ""); w.Code != 503 {
		t.Fatal("read failure hidden")
	}
}
