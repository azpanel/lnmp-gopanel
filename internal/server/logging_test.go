package server

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"testing"

	"github.com/azpanel/lnmp-gopanel/internal/auth"
	"github.com/azpanel/lnmp-gopanel/internal/config"
	"github.com/azpanel/lnmp-gopanel/internal/control"
)

type loggingStub struct {
	enabled     []string
	unavailable bool
	failure     bool
}

func (b *loggingStub) Run(context.Context, string, string) (control.Result, error) {
	return control.Result{State: "running"}, nil
}
func (b *loggingStub) LoggingStatus(context.Context) (control.LoggingSnapshot, error) {
	if b.unavailable {
		return control.LoggingSnapshot{}, errors.New("请同步升级辅助程序和 sudoers 权限")
	}
	return control.LoggingSnapshot{PHP: control.LoggingView{Kind: "php", Service: "php-fpm", Available: true}, Database: control.LoggingView{Kind: "database", Service: "mysql", Available: true}}, nil
}
func (b *loggingStub) EnableLogging(_ context.Context, kind string) (control.Result, error) {
	b.enabled = append(b.enabled, kind)
	if b.failure {
		return control.Result{}, errors.New("重启失败；已恢复原配置")
	}
	return control.Result{State: "running", Message: "日志已开启，服务已重启"}, nil
}
func TestLoggingAPIAuthenticationValidationAndFailure(t *testing.T) {
	s, _ := testServer(t)
	b := &loggingStub{}
	s.Services = control.New([]config.Service{{Name: "php-fpm", Manager: "init", Program: "/etc/init.d/php-fpm"}, {Name: "mysql", Manager: "init", Program: "/etc/init.d/mysql"}}, b)
	h := s.Handler()
	path := "/api/v1/system/logging"
	id, session := s.Sessions.Create()
	cookie := &http.Cookie{Name: auth.CookieName, Value: id}
	if w := request(h, "GET", path, "", nil, "", ""); w.Code != 401 {
		t.Fatal(w.Code)
	}
	if w := request(h, "POST", path+"/php/enable", "", nil, "", ""); w.Code != 401 {
		t.Fatal(w.Code)
	}
	if w := request(h, "GET", path, "", cookie, "", ""); w.Code != 200 || !strings.Contains(w.Body.String(), `"kind":"php"`) || len(b.enabled) != 0 {
		t.Fatal(w.Code, w.Body.String())
	}
	for _, kind := range []string{"php", "database"} {
		target := path + "/" + kind + "/enable"
		for _, origin := range []string{"", "https://evil.example"} {
			csrf := ""
			if origin != "" {
				csrf = session.CSRF
			}
			if w := request(h, "POST", target, "", cookie, csrf, origin); w.Code != 403 || len(b.enabled) != 0 {
				t.Fatal(w.Code)
			}
		}
		for _, body := range []string{`{}`, `{"path":"/etc/passwd"}`, `{"threshold":1}`, `[]`} {
			if w := request(h, "POST", target, body, cookie, session.CSRF, ""); w.Code != 400 || len(b.enabled) != 0 {
				t.Fatal(w.Code, w.Body.String())
			}
		}
	}
	if w := request(h, "POST", path+"/other/enable", "", cookie, session.CSRF, ""); w.Code != 400 || len(b.enabled) != 0 {
		t.Fatal(w.Code)
	}
	for _, kind := range []string{"php", "database"} {
		if w := request(h, "POST", path+"/"+kind+"/enable", "", cookie, session.CSRF, ""); w.Code != 200 || !strings.Contains(w.Body.String(), "日志已开启") {
			t.Fatal(w.Code, w.Body.String())
		}
	}
	if strings.Join(b.enabled, ",") != "php,database" {
		t.Fatal(b.enabled)
	}
	b.failure = true
	if w := request(h, "POST", path+"/database/enable", "", cookie, session.CSRF, ""); w.Code != 409 || !strings.Contains(w.Body.String(), "已恢复原配置") {
		t.Fatal(w.Code, w.Body.String())
	}
	for _, v := range s.Services.List() {
		if v.Busy {
			t.Fatal(v)
		}
	}
	b.unavailable = true
	if w := request(h, "GET", path, "", cookie, "", ""); w.Code != 503 || !strings.Contains(w.Body.String(), "sudoers") {
		t.Fatal(w.Code, w.Body.String())
	}
}
