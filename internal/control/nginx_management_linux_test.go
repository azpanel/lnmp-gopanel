//go:build linux

package control

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/azpanel/lnmp-gopanel/internal/config"
	"gopkg.in/yaml.v3"
)

func managementFixture(t *testing.T) nginxManagementController {
	t.Helper()
	if os.Geteuid() != 0 {
		t.Skip("root-owned state verification")
	}
	root := t.TempDir()
	p := nginxManagementController{
		root: root, services: nginxManagementConfig, proc: filepath.Join(root, "proc"),
		trust: func(string) error { return nil }, lock: func() (func(), error) { return func() {}, nil },
		write: writeManagementFile,
	}
	for _, dir := range []string{"etc/lnmp-gopanel", "etc/init.d", "usr/local/nginx/conf", "usr/local/nginx/logs", "usr/local/nginx/sbin", "usr/bin", "proc/123"} {
		if err := os.MkdirAll(filepath.Join(root, dir), 0700); err != nil {
			t.Fatal(err)
		}
	}
	write := func(path, text string, mode os.FileMode) {
		t.Helper()
		if err := os.WriteFile(p.path(path), []byte(text), mode); err != nil {
			t.Fatal(err)
		}
	}
	write(lnmpNginxBinary, "fixture binary", 0755)
	write("/usr/bin/systemctl", "fixture systemctl", 0755)
	write(lnmpNginxInit, "#!/bin/sh\nNGINX_BIN='"+lnmpNginxBinary+"'\nCONFIG='"+lnmpNginxConfig+"'\n", 0700)
	write(lnmpNginxConfig, "pid "+lnmpNginxPID+";\n", 0600)
	write("/proc/123/cmdline", "nginx: master process "+lnmpNginxBinary+" -c "+lnmpNginxConfig+"\x00", 0600)
	write("/proc/123/stat", "123 (nginx) S "+strings.Repeat("0 ", 18)+"1000 0\n", 0600)
	write("/proc/123/cgroup", "0::/user.slice/user-0.slice/session-1.scope\n", 0600)
	if err := os.Symlink(p.path(lnmpNginxBinary), filepath.Join(p.proc, "123/exe")); err != nil {
		t.Fatal(err)
	}
	services := config.Services{Services: []config.Service{
		{Name: "nginx", Manager: "systemd", Program: "/usr/bin/systemctl", Target: "nginx.service", CheckProgram: lnmpNginxBinary},
		{Name: "php-fpm", Manager: "init", Program: "/etc/init.d/php-fpm"},
	}}
	b, err := yaml.Marshal(services)
	if err != nil {
		t.Fatal(err)
	}
	write(nginxManagementConfig, string(b), 0640)
	p.run = func(_ context.Context, program string, args ...string) (string, error) {
		t.Helper()
		if program == "/usr/bin/systemctl" && reflect.DeepEqual(args, []string{"show", "--property=ActiveState,MainPID,ControlGroup", "nginx.service"}) {
			return "ActiveState=failed\nMainPID=0\nControlGroup=\n", nil
		}
		if program == lnmpNginxBinary && reflect.DeepEqual(args, []string{"-t"}) {
			return "configuration test successful", nil
		}
		t.Fatalf("unexpected command: %s %v", program, args)
		return "", errors.New("unexpected command")
	}
	return p
}

func TestNginxManagementStatusVerifiesUnitOwnership(t *testing.T) {
	p := managementFixture(t)
	s := config.Service{Name: "nginx", Manager: "systemd", Program: "/usr/bin/systemctl", Target: "nginx.service", CheckProgram: lnmpNginxBinary}
	for _, tc := range []struct {
		name, state, pid, group, processGroup, want string
	}{
		{"failed with live daemon", "failed", "0", "", "/user.slice/session.scope", "unmanaged"},
		{"inactive with live daemon", "inactive", "0", "", "/user.slice/session.scope", "unmanaged"},
		{"active wrong pid", "active", "999", "/system.slice/nginx.service", "/user.slice/session.scope", "unmanaged"},
		{"active wrong group", "active", "123", "/system.slice/nginx.service", "/user.slice/session.scope", "unmanaged"},
		{"wrong group prefix", "active", "123", "/system.slice/nginx.service", "/system.slice/nginx.service-other", "unmanaged"},
		{"active owns daemon", "active", "123", "/system.slice/nginx.service", "/system.slice/nginx.service", ""},
		{"active descendant group", "active", "123", "/system.slice/nginx.service", "/system.slice/nginx.service/child", ""},
		{"unit transitioning", "activating", "0", "", "/user.slice/session.scope", "transitioning"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			p.run = func(context.Context, string, ...string) (string, error) {
				return "ActiveState=" + tc.state + "\nMainPID=" + tc.pid + "\nControlGroup=" + tc.group, nil
			}
			if err := os.WriteFile(filepath.Join(p.proc, "123/cgroup"), []byte("0::"+tc.processGroup+"\n"), 0600); err != nil {
				t.Fatal(err)
			}
			result, handled := p.status(context.Background(), s)
			if handled != (tc.want != "") || result.State != tc.want {
				t.Fatalf("result=%+v handled=%v", result, handled)
			}
		})
	}
	p.run = func(context.Context, string, ...string) (string, error) { return "", errors.New("unavailable") }
	if result, handled := p.status(context.Background(), s); !handled || result.State != "unknown" {
		t.Fatal(result, handled)
	}
}

func TestNginxManagementRepairRestoresPIDAndPreservesOtherServices(t *testing.T) {
	for _, pid := range []string{"missing", "", "123\n"} {
		t.Run(pid, func(t *testing.T) {
			p := managementFixture(t)
			if pid != "missing" {
				if err := os.WriteFile(p.path(lnmpNginxPID), []byte(pid), 0600); err != nil {
					t.Fatal(err)
				}
			}
			before, err := p.prepare(context.Background())
			if err != nil {
				t.Fatal(err)
			}
			if before.Plan.RestorePID != (pid != "123\n") {
				t.Fatal(before.Plan)
			}
			// A preview must not create PID files or change service configuration.
			current, err := p.readFile(p.services, false)
			if err != nil || string(current.Data) != string(before.Services.Data) {
				t.Fatal("preview changed configuration", err)
			}
			if _, err := os.Stat(p.path(lnmpNginxPID)); (pid == "missing") != os.IsNotExist(err) {
				t.Fatal("preview changed PID file", err)
			}
			result, err := p.repair(context.Background(), ManagementRepairRequest{before.Plan.Revision})
			if err != nil || result.State != "running" || result.Manager != "init" {
				t.Fatal(result, err)
			}
			services, err := config.LoadServices(p.path(p.services))
			if err != nil || services[0].Manager != "init" || services[0].Program != lnmpNginxInit || services[1].Name != "php-fpm" || services[1].Manager != "init" {
				t.Fatal(services, err)
			}
			pidFile, err := p.readFile(lnmpNginxPID, false)
			if err != nil || strings.TrimSpace(string(pidFile.Data)) != "123" {
				t.Fatal(pidFile, err)
			}
			current, err = p.readFile(p.services, false)
			if err != nil || current.Mode != before.Services.Mode || current.GID != before.Services.GID {
				t.Fatal("configuration permissions changed", err)
			}
			backups, _ := filepath.Glob(p.path(p.services) + ".nginx-repair-*.json")
			if len(backups) != 1 {
				t.Fatal(backups)
			}
			info, err := os.Stat(backups[0])
			if err != nil || info.Mode().Perm() != 0600 {
				t.Fatal("backup is not private", err)
			}
		})
	}
}

func TestNginxManagementRepairRejectsStalePreview(t *testing.T) {
	for _, change := range []string{"configuration", "pid reuse", "script", "pid file", "unit", "custom args"} {
		t.Run(change, func(t *testing.T) {
			p := managementFixture(t)
			before, err := p.prepare(context.Background())
			if err != nil {
				t.Fatal(err)
			}
			var path, data string
			switch change {
			case "configuration":
				path, data = p.services, string(before.Services.Data)+"\n"
			case "pid reuse":
				path, data = "/proc/123/stat", "123 (nginx) S "+strings.Repeat("0 ", 18)+"2000 0\n"
			case "script":
				path, data = lnmpNginxInit, string(before.Script.Data)+"# changed\n"
			case "pid file":
				path, data = lnmpNginxPID, "123\n"
			case "custom args":
				path, data = "/proc/123/cmdline", "nginx: master process "+lnmpNginxBinary+" -c /etc/custom.conf\x00"
			case "unit":
				p.run = func(context.Context, string, ...string) (string, error) {
					return "ActiveState=active\nMainPID=999\nControlGroup=/other", nil
				}
			}
			if path != "" {
				if err := os.WriteFile(p.path(path), []byte(data), 0600); err != nil {
					t.Fatal(err)
				}
			}
			p.write = func(string, []byte, os.FileMode, int) error { t.Fatal("stale preview wrote files"); return nil }
			if _, err := p.repair(context.Background(), ManagementRepairRequest{before.Plan.Revision}); err == nil {
				t.Fatal("stale preview accepted")
			}
		})
	}
}

func TestNginxManagementRepairRollsBackFailures(t *testing.T) {
	for _, fail := range []string{"config check", "write config", "verify config", "process changed", "rollback failure"} {
		t.Run(fail, func(t *testing.T) {
			p := managementFixture(t)
			before, err := p.prepare(context.Background())
			if err != nil {
				t.Fatal(err)
			}
			run := p.run
			p.run = func(ctx context.Context, program string, args ...string) (string, error) {
				if program == lnmpNginxBinary {
					// Reproduce nginx -t creating an empty PID file.
					if err := os.WriteFile(p.path(lnmpNginxPID), nil, 0600); err != nil {
						t.Fatal(err)
					}
					if fail == "config check" || fail == "rollback failure" {
						return "", errors.New("bad config")
					}
				}
				return run(ctx, program, args...)
			}
			write := p.write
			p.write = func(path string, data []byte, mode os.FileMode, gid int) error {
				if path == p.path(p.services) && string(data) == string(before.Candidate) {
					if fail == "write config" {
						return errors.New("write failed")
					}
					if fail == "verify config" {
						data = []byte("invalid: configuration\n")
					}
					if fail == "process changed" {
						if err := os.WriteFile(filepath.Join(p.proc, "123/stat"), []byte("123 (nginx) S "+strings.Repeat("0 ", 18)+"2000 0\n"), 0600); err != nil {
							t.Fatal(err)
						}
					}
				}
				return write(path, data, mode, gid)
			}
			if fail == "rollback failure" {
				// Make the rollback of the test-created PID file fail visibly.
				p.run = func(ctx context.Context, program string, args ...string) (string, error) {
					if program == lnmpNginxBinary {
						if err := os.Mkdir(p.path(lnmpNginxPID), 0700); err != nil {
							t.Fatal(err)
						}
						if err := os.WriteFile(filepath.Join(p.path(lnmpNginxPID), "keep"), nil, 0600); err != nil {
							t.Fatal(err)
						}
						return "", errors.New("bad config")
					}
					return run(ctx, program, args...)
				}
			}
			_, err = p.repair(context.Background(), ManagementRepairRequest{before.Plan.Revision})
			if err == nil {
				t.Fatal("failure ignored")
			}
			if fail == "rollback failure" {
				if !strings.Contains(err.Error(), "自动恢复未完成") {
					t.Fatal(err)
				}
				return
			}
			if fail == "process changed" {
				if !strings.Contains(err.Error(), "自动恢复未完成") {
					t.Fatal(err)
				}
				after, readErr := p.readFile(p.services, false)
				if readErr != nil || !reflect.DeepEqual(before.Services, after) {
					t.Fatal("configuration not restored", readErr)
				}
				pid, readErr := os.ReadFile(p.path(lnmpNginxPID))
				if readErr != nil || string(pid) != "123\n" {
					t.Fatal("new master PID record was removed", readErr)
				}
				return
			}
			if !strings.Contains(err.Error(), "已恢复原配置和 PID") {
				t.Fatal(err)
			}
			after, readErr := p.readFile(p.services, false)
			if readErr != nil || !reflect.DeepEqual(before.Services, after) {
				t.Fatal("configuration not restored", readErr)
			}
			if _, statErr := os.Stat(p.path(lnmpNginxPID)); !os.IsNotExist(statErr) {
				t.Fatal("test-created PID not removed", statErr)
			}
		})
	}
}

func TestNginxManagementRepairRejectsUnsafePID(t *testing.T) {
	for _, kind := range []string{"symlink", "hardlink", "writable", "wrong pid", "directory"} {
		t.Run(kind, func(t *testing.T) {
			p := managementFixture(t)
			path := p.path(lnmpNginxPID)
			var err error
			switch kind {
			case "symlink":
				err = os.Symlink(p.path(p.services), path)
			case "hardlink":
				err = os.Link(p.path(p.services), path)
			case "writable":
				err = os.WriteFile(path, nil, 0600)
				if err == nil {
					err = os.Chmod(path, 0666)
				}
			case "wrong pid":
				err = os.WriteFile(path, []byte("999\n"), 0600)
			case "directory":
				err = os.Mkdir(path, 0700)
			}
			if err != nil {
				t.Fatal(err)
			}
			if _, err := p.prepare(context.Background()); err == nil {
				t.Fatal("unsafe PID file accepted")
			}
		})
	}
}

func TestNginxManagementRollbackPreservesExistingPID(t *testing.T) {
	for _, pid := range []string{"", "123\n"} {
		t.Run(pid, func(t *testing.T) {
			p := managementFixture(t)
			if err := os.WriteFile(p.path(lnmpNginxPID), []byte(pid), 0640); err != nil {
				t.Fatal(err)
			}
			before, err := p.prepare(context.Background())
			if err != nil {
				t.Fatal(err)
			}
			write := p.write
			p.write = func(path string, data []byte, mode os.FileMode, gid int) error {
				if path == p.path(p.services) && string(data) == string(before.Candidate) {
					return errors.New("write failure")
				}
				return write(path, data, mode, gid)
			}
			if _, err = p.repair(context.Background(), ManagementRepairRequest{before.Plan.Revision}); err == nil {
				t.Fatal("failed write was ignored")
			}
			after, err := p.readFile(lnmpNginxPID, false)
			if err != nil || !reflect.DeepEqual(before.PID, after) {
				t.Fatal("PID content or metadata not restored", after, err)
			}
		})
	}
}

func TestNginxManagementRollbackPreservesConcurrentAdministratorChanges(t *testing.T) {
	p := managementFixture(t)
	before, err := p.prepare(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	run := p.run
	p.run = func(ctx context.Context, program string, args ...string) (string, error) {
		if program == "/usr/bin/systemctl" {
			content, err := os.ReadFile(p.path(p.services))
			if err != nil {
				t.Fatal(err)
			}
			if strings.Contains(string(content), "manager: init") && !strings.Contains(string(content), "manager: systemd") {
				if err := os.WriteFile(p.path(p.services), append(content, []byte("# administrator update\n")...), 0640); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(p.path(lnmpNginxPID), []byte("999\n"), 0644); err != nil {
					t.Fatal(err)
				}
				return "ActiveState=active\nMainPID=999\nControlGroup=/system.slice/nginx.service", nil
			}
		}
		return run(ctx, program, args...)
	}
	if _, err := p.repair(context.Background(), ManagementRepairRequest{before.Plan.Revision}); err == nil || !strings.Contains(err.Error(), "自动恢复未完成") {
		t.Fatal(err)
	}
	content, err := os.ReadFile(p.path(p.services))
	if err != nil || !strings.Contains(string(content), "# administrator update") {
		t.Fatal("administrator configuration overwritten", err)
	}
	pid, err := os.ReadFile(p.path(lnmpNginxPID))
	if err != nil || string(pid) != "999\n" {
		t.Fatal("administrator PID overwritten", err)
	}
}
