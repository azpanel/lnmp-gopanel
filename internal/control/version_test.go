package control

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/azpanel/lnmp-gopanel/internal/config"
)

func TestVersionLookupDistinguishesPermissionsFromMissing(t *testing.T) {
	for _, tc := range []struct {
		err  error
		want string
	}{{os.ErrPermission, "权限不足"}, {os.ErrNotExist, "未找到服务可执行文件"}} {
		_, err := versionProgram([]string{"/restricted/sbin/nginx"}, func(path string) (os.FileInfo, error) { return nil, &os.PathError{Op: "stat", Path: path, Err: tc.err} })
		if err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Fatalf("error=%v, want %s", err, tc.want)
		}
	}
	path := filepath.Join(t.TempDir(), "daemon")
	if err := os.WriteFile(path, []byte("binary"), 0755); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	program, err := versionProgram([]string{"/missing", "/available"}, func(p string) (os.FileInfo, error) {
		if p == "/available" {
			return info, nil
		}
		return nil, os.ErrNotExist
	})
	if err != nil || program != "/available" {
		t.Fatal(program, err)
	}
}

func TestInstalledVersionParsing(t *testing.T) {
	for _, tc := range []struct{ name, output, want string }{
		{"nginx", "nginx version: nginx/1.28.0", "1.28.0"},
		{"php-fpm", "PHP 8.4.4 (fpm-fcgi) (built: Feb 2025)", "8.4.4"},
		{"mysql", "/usr/local/mysql/bin/mysqld  Ver 8.4.3 for Linux on x86_64", "8.4.3"},
		{"mariadb", "mariadbd  Ver 11.4.5-MariaDB for linux-systemd", "11.4.5"},
		{"redis", "Redis server v=7.4.2 sha=00000000:0 malloc=jemalloc-5.3.0 bits=64", "7.4.2"},
		{"redis", "Usage: redis-server", ""},
		{"mysql", "libssl version 3.4.1", ""},
		{"sshd", "OpenSSH_9.8", ""},
	} {
		t.Run(tc.name+tc.want, func(t *testing.T) {
			got, err := parseInstalledVersion(tc.name, tc.output)
			if got != tc.want || (err == nil) != (tc.want != "") {
				t.Fatalf("got %q, %v; want %q", got, err, tc.want)
			}
		})
	}
}

type versionExecutor struct {
	versions atomic.Int32
	fail     bool
}

func (e *versionExecutor) Run(context.Context, string, string) (Result, error) {
	return Result{State: "running"}, nil
}
func (e *versionExecutor) Version(context.Context, string) (string, error) {
	e.versions.Add(1)
	if e.fail {
		return "", errors.New("version unavailable")
	}
	return "7.4.2", nil
}

func TestVersionsCachedAndFailuresPreserveStatus(t *testing.T) {
	e := &versionExecutor{}
	m := New([]config.Service{{Name: "redis", Manager: "init", Program: "/etc/init.d/redis"}}, e)
	m.Refresh(context.Background())
	m.Refresh(context.Background())
	view := m.List()[0]
	if view.Version != "7.4.2" || view.State != "running" || e.versions.Load() != 1 || len(view.Actions) != 0 {
		t.Fatalf("unexpected Redis view: %+v", view)
	}
	if _, err := m.Action(context.Background(), "redis", "restart"); !errors.Is(err, ErrUnsupported) {
		t.Fatal("Redis must remain read-only")
	}
	m.mu.Lock()
	v := m.views["redis"]
	v.VersionCheckedAt = time.Now().Add(-2 * time.Hour)
	m.views["redis"] = v
	m.mu.Unlock()
	e.fail = true
	m.Refresh(context.Background())
	m.Refresh(context.Background())
	view = m.List()[0]
	if view.State != "running" || view.Version != "7.4.2" || view.VersionError == "" || e.versions.Load() != 2 {
		t.Fatalf("version failure must preserve status and cached version: %+v", view)
	}
}

func TestRedisCommandsStayReadOnly(t *testing.T) {
	s := config.Service{Name: "redis", Manager: "init", Program: "/etc/init.d/redis"}
	for _, action := range []string{"start", "stop", "restart", "reload", "version"} {
		_, err := Execute(context.Background(), s, action, func(context.Context, string, ...string) (string, error) {
			t.Fatal("Redis mutation or privileged version query executed")
			return "", nil
		})
		if !errors.Is(err, ErrUnsupported) {
			t.Fatalf("%s: %v", action, err)
		}
	}
}
