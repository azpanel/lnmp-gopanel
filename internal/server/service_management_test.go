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

type serviceRepairStub struct {
	previews, repairs, actions int
	fail                       bool
	fixed                      bool
}

func (b *serviceRepairStub) Run(_ context.Context, _, action string) (control.Result, error) {
	if action != "status" {
		b.actions++
	}
	if b.fixed {
		return control.Result{State: "running", Manager: "init"}, nil
	}
	return control.Result{State: "unmanaged", Message: "管理状态异常", Manager: "systemd"}, nil
}
func (b *serviceRepairStub) PreviewManagementRepair(context.Context) (control.ManagementRepairPlan, error) {
	b.previews++
	return control.ManagementRepairPlan{Revision: strings.Repeat("a", 64), MasterPID: 123, CurrentManager: "systemd", ProposedManager: "init", RestorePID: true}, nil
}
func (b *serviceRepairStub) RepairManagement(_ context.Context, r control.ManagementRepairRequest) (control.Result, error) {
	b.repairs++
	if b.fail || r.Revision != strings.Repeat("a", 64) {
		return control.Result{}, errors.New("修复失败，已恢复原配置和 PID 记录")
	}
	b.fixed = true
	return control.Result{State: "running", Manager: "init", Message: "管理方式已修复"}, nil
}

func TestManagementRepairAuthenticationConfirmationAndRollback(t *testing.T) {
	s, _ := testServer(t)
	b := &serviceRepairStub{}
	s.Services = control.New([]config.Service{{Name: "nginx", Manager: "systemd", Program: "/usr/bin/systemctl", Target: "nginx.service", CheckProgram: "/usr/local/nginx/sbin/nginx"}}, b)
	s.Services.Refresh(context.Background())
	h := s.Handler()
	path := "/api/v1/services/nginx/management-repair"
	id, session := s.Sessions.Create()
	cookie := &http.Cookie{Name: auth.CookieName, Value: id}
	body := `{"revision":"` + strings.Repeat("a", 64) + `"}`
	for _, method := range []string{"GET", "POST"} {
		if w := request(h, method, path, body, nil, "", ""); w.Code != 401 {
			t.Fatal(w.Code)
		}
	}
	if w := request(h, "POST", path, body, cookie, "", ""); w.Code != 403 || b.repairs != 0 {
		t.Fatal(w.Code)
	}
	if w := request(h, "POST", path, body, cookie, session.CSRF, "https://evil.example"); w.Code != 403 || b.repairs != 0 {
		t.Fatal(w.Code)
	}
	if w := request(h, "GET", path, "", cookie, "", ""); w.Code != 200 || b.previews != 1 || b.repairs != 0 || !strings.Contains(w.Body.String(), "master_pid") {
		t.Fatal(w.Code, w.Body.String())
	}
	for _, action := range []string{"start", "stop", "restart", "reload"} {
		if w := request(h, "POST", "/api/v1/services/nginx/actions/"+action, "", cookie, session.CSRF, ""); w.Code != 409 || b.actions != 0 {
			t.Fatal(action, w.Code, w.Body.String())
		}
	}
	for _, invalid := range []string{`{}`, `{"revision":"short"}`, `{"revision":123}`, `{"revision":"` + strings.Repeat("z", 64) + `"}`, body + `{}`, `{"revision":"` + strings.Repeat("a", 64) + `","program":"/bin/sh"}`} {
		if w := request(h, "POST", path, invalid, cookie, session.CSRF, ""); w.Code != 400 || b.repairs != 0 {
			t.Fatal(w.Code, w.Body.String())
		}
	}
	if w := request(h, "GET", "/api/v1/services/mysql/management-repair", "", cookie, "", ""); w.Code != 400 {
		t.Fatal(w.Code)
	}
	b.fail = true
	if w := request(h, "POST", path, body, cookie, session.CSRF, ""); w.Code != 409 || !strings.Contains(w.Body.String(), "已恢复原配置") {
		t.Fatal(w.Code, w.Body.String())
	}
	if views := s.Services.List(); views[0].State != "unmanaged" || views[0].Busy {
		t.Fatal(views)
	}
	b.fail = false
	if w := request(h, "POST", path, body, cookie, session.CSRF, ""); w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	if w := request(h, "GET", "/api/v1/services", "", cookie, "", ""); w.Code != 200 || !strings.Contains(w.Body.String(), `"manager":"init"`) || !strings.Contains(w.Body.String(), `"state":"running"`) {
		t.Fatal(w.Code, w.Body.String())
	}
}
