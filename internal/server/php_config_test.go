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

type phpConfigStub struct {
	updates     int
	failure     bool
	unavailable bool
	busy        func()
}

func (b *phpConfigStub) Run(context.Context, string, string) (control.Result, error) {
	return control.Result{State: "running"}, nil
}
func (b *phpConfigStub) PHPConfig(context.Context) (control.PHPConfigSnapshot, error) {
	if b.unavailable {
		return control.PHPConfigSnapshot{}, errors.New("请同步升级辅助程序和 sudoers 权限")
	}
	return control.PHPConfigSnapshot{Revision: strings.Repeat("a", 64), Path: "/usr/local/php/etc/php.ini", DisableFunctions: []string{"exec"}}, nil
}
func (b *phpConfigStub) UpdatePHPConfig(ctx context.Context, r control.PHPConfigRequest) (control.PHPConfigResponse, error) {
	b.updates++
	if b.busy != nil {
		b.busy()
	}
	if b.failure {
		return control.PHPConfigResponse{}, errors.New("PHP-FPM 重载失败；已恢复原配置")
	}
	snapshot, _ := b.PHPConfig(ctx)
	return control.PHPConfigResponse{PHPConfigSnapshot: snapshot, Message: "PHP 配置已保存，PHP-FPM 已重载"}, nil
}
func TestPHPConfigAPI(t *testing.T) {
	s, _ := testServer(t)
	b := &phpConfigStub{}
	s.Services = control.New([]config.Service{{Name: "php-fpm", Manager: "init", Program: "/etc/init.d/php-fpm"}}, b)
	h := s.Handler()
	path := "/api/v1/services/php-fpm/config"
	id, session := s.Sessions.Create()
	cookie := &http.Cookie{Name: auth.CookieName, Value: id}
	body := `{"revision":"` + strings.Repeat("a", 64) + `","fields":{"memory_limit":"256M"},"disable_functions":[]}`
	for _, method := range []string{"GET", "PUT"} {
		if w := request(h, method, path, body, nil, "", ""); w.Code != 401 {
			t.Fatal(w.Code)
		}
	}
	for _, origin := range []string{"", "https://evil.example"} {
		csrf := ""
		if origin != "" {
			csrf = session.CSRF
		}
		if w := request(h, "PUT", path, body, cookie, csrf, origin); w.Code != 403 || b.updates != 0 {
			t.Fatal(w.Code, b.updates)
		}
	}
	for _, invalid := range []string{`{}`, strings.Replace(body, `"fields":`, `"path":"/etc/passwd","fields":`, 1), strings.Replace(body, "256M", "invalid", 1), body + `{}`} {
		if w := request(h, "PUT", path, invalid, cookie, session.CSRF, ""); w.Code != 400 || b.updates != 0 {
			t.Fatal(w.Code, w.Body.String())
		}
	}
	if w := request(h, "GET", path, "", cookie, "", ""); w.Code != 200 || b.updates != 0 || !strings.Contains(w.Body.String(), `"disable_functions":["exec"]`) {
		t.Fatal(w.Code, w.Body.String())
	}
	b.busy = func() {
		if _, err := s.Services.Action(context.Background(), "php-fpm", "restart"); !errors.Is(err, control.ErrBusy) {
			t.Fatal("service action did not share PHP configuration lock", err)
		}
		for _, v := range s.Services.List() {
			if v.Name == "php-fpm" && !v.Busy {
				t.Fatal("not marked busy")
			}
		}
	}
	if w := request(h, "PUT", path, body, cookie, session.CSRF, ""); w.Code != 200 || b.updates != 1 || !strings.Contains(w.Body.String(), "已重载") {
		t.Fatal(w.Code, w.Body.String(), b.updates)
	}
	// Large function lists use the feature's limit instead of the default 4 KiB.
	if w := request(h, "PUT", path, body+strings.Repeat(" ", 5000), cookie, session.CSRF, ""); w.Code != 200 || b.updates != 2 {
		t.Fatal(w.Code, w.Body.String(), b.updates)
	}
	if w := request(h, "PUT", path, body+strings.Repeat(" ", control.PHPConfigRequestLimit), cookie, session.CSRF, ""); w.Code != 400 || b.updates != 2 {
		t.Fatal(w.Code, w.Body.String(), b.updates)
	}
	b.failure = true
	if w := request(h, "PUT", path, body, cookie, session.CSRF, ""); w.Code != 409 || !strings.Contains(w.Body.String(), "已恢复原配置") {
		t.Fatal(w.Code, w.Body.String())
	}
	for _, v := range s.Services.List() {
		if v.Busy {
			t.Fatal("busy not cleared")
		}
	}
	b.unavailable = true
	if w := request(h, "GET", path, "", cookie, "", ""); w.Code != 503 || !strings.Contains(w.Body.String(), "sudoers") {
		t.Fatal(w.Code, w.Body.String())
	}
}
