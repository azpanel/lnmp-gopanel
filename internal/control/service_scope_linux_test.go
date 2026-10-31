//go:build linux

package control

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/azpanel/lnmp-gopanel/internal/config"
)

func TestInitAndLNMPActionsUseIndependentScopes(t *testing.T) {
	services := []config.Service{
		{Name: "php-fpm", Manager: "init", Program: "/etc/init.d/php-fpm"},
		{Name: "nginx", Manager: "init", Program: "/etc/init.d/nginx", CheckProgram: "/usr/local/nginx/sbin/nginx"},
		{Name: "mysql", Manager: "init", Program: "/etc/init.d/mysql"},
		{Name: "mariadb", Manager: "init", Program: "/etc/init.d/mariadb"},
		{Name: "php-fpm", Manager: "lnmp", Program: "/usr/bin/lnmp", Target: "php-fpm"},
	}
	for _, s := range services {
		for _, action := range []string{"start", "stop", "restart"} {
			t.Run(s.Manager+"/"+s.Name+"/"+action, func(t *testing.T) {
				calls := 0
				scope := serviceScope{enabled: func() (bool, error) { return true, nil }, trust: func(path string) error {
					if path != serviceScopeProgram {
						t.Fatal(path)
					}
					return nil
				}}
				run := func(_ context.Context, program string, args ...string) (string, error) {
					calls++
					expected := []string{action}
					if s.Manager == "lnmp" {
						expected = []string{s.Target, action}
					}
					if program != serviceScopeProgram || len(args) != 6+len(expected) {
						t.Fatal(program, args)
					}
					if !reflect.DeepEqual(args[:3], []string{"--quiet", "--scope", "--collect"}) {
						t.Fatal(args)
					}
					if !regexp.MustCompile(`^--unit=` + serviceScopePrefix + s.Name + `-[0-9a-f]{24}\.scope$`).MatchString(args[3]) {
						t.Fatal(args[3])
					}
					if args[4] != "--" || args[5] != s.Program || !reflect.DeepEqual(args[6:], expected) {
						t.Fatal(args)
					}
					return "script output", nil
				}
				result, err := Execute(context.Background(), s, action, scope.runner(s, action, run))
				if err != nil || result.State != "unknown" || calls != 1 {
					t.Fatal(result, err, calls)
				}
			})
		}
	}
}
func TestServiceScopeLeavesNativeUnitsAndStatusUnchanged(t *testing.T) {
	for _, tc := range []struct {
		s      config.Service
		action string
		args   []string
	}{
		{config.Service{Name: "php-fpm", Manager: "systemd", Program: "/usr/bin/systemctl", Target: "php-fpm.service"}, "restart", []string{"restart", "php-fpm.service"}},
		{config.Service{Name: "php-fpm", Manager: "systemd", Program: "/usr/bin/systemctl", Target: "php-fpm.service"}, "status", []string{"is-active", "php-fpm.service"}},
		{config.Service{Name: "php-fpm", Manager: "init", Program: "/etc/init.d/php-fpm"}, "status", []string{"status"}},
	} {
		scope := serviceScope{enabled: func() (bool, error) { t.Fatal("unnecessary scope detection"); return false, nil }}
		run := func(_ context.Context, program string, args ...string) (string, error) {
			if program != tc.s.Program || !reflect.DeepEqual(args, tc.args) {
				t.Fatal(program, args)
			}
			return "active", nil
		}
		if _, err := Execute(context.Background(), tc.s, tc.action, scope.runner(tc.s, tc.action, run)); err != nil {
			t.Fatal(err)
		}
	}
}
func TestServiceScopeFailuresNeverRetryWithoutIsolation(t *testing.T) {
	s := config.Service{Name: "php-fpm", Manager: "init", Program: "/etc/init.d/php-fpm"}
	for _, failure := range []string{"environment", "trust", "command", "cancelled"} {
		t.Run(failure, func(t *testing.T) {
			calls := 0
			scope := serviceScope{enabled: func() (bool, error) {
				if failure == "environment" {
					return false, errors.New("stat failed")
				}
				return true, nil
			}, trust: func(string) error {
				if failure == "trust" {
					return errors.New("unsafe")
				}
				return nil
			}}
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			if failure == "cancelled" {
				cancel()
			}
			run := func(task context.Context, program string, args ...string) (string, error) {
				calls++
				if program != serviceScopeProgram {
					t.Fatal("unisolated fallback", program, args)
				}
				if task != ctx {
					t.Fatal("context was replaced")
				}
				if task.Err() != nil {
					return "", task.Err()
				}
				return "failed script", errors.New("script exited 7")
			}
			result, err := Execute(ctx, s, "restart", scope.runner(s, "restart", run))
			if err == nil || result.State != "unknown" {
				t.Fatal(result, err)
			}
			expected := 0
			if failure == "command" || failure == "cancelled" {
				expected = 1
			}
			if calls != expected {
				t.Fatal(calls)
			}
		})
	}
}
func TestServiceScopePreservesChecksAndNonSystemdCommands(t *testing.T) {
	for _, enabled := range []bool{false, true} {
		calls := [][]string{}
		scope := serviceScope{enabled: func() (bool, error) { return enabled, nil }, trust: func(string) error { return nil }}
		run := func(_ context.Context, program string, args ...string) (string, error) {
			calls = append(calls, append([]string{program}, args...))
			return "", nil
		}
		if _, err := Execute(context.Background(), nginx, "reload", scope.runner(nginx, "reload", run)); err != nil {
			t.Fatal(err)
		}
		if len(calls) != 2 || !reflect.DeepEqual(calls[0], []string{nginx.CheckProgram, "-t"}) {
			t.Fatal(calls)
		}
		if enabled && calls[1][0] != serviceScopeProgram {
			t.Fatal(calls)
		}
		if !enabled && !reflect.DeepEqual(calls[1], []string{nginx.Program, "reload"}) {
			t.Fatal(calls)
		}
	}
}

// Re-executed only by test-service-isolation.py inside temporary systemd units.
// The runner substitutes the real init script path with a disposable simulator;
// it must never execute a host service or accept arbitrary production commands.
func TestServiceScopeFixture(t *testing.T) {
	mode := os.Getenv("GOPANEL_TEST_SERVICE_SCOPE_MODE")
	if mode == "" {
		return
	}
	if mode != "raw" && mode != "scope" {
		t.Fatal("unknown fixture mode")
	}
	script := os.Getenv("GOPANEL_TEST_SERVICE_SCOPE_SCRIPT")
	if !filepath.IsAbs(script) || !strings.HasPrefix(filepath.Base(filepath.Dir(script)), "gopanel-service-isolation-") {
		t.Fatal("invalid fixture path")
	}
	action := os.Getenv("GOPANEL_TEST_SERVICE_SCOPE_ACTION")
	if action == "" {
		action = "start"
	}
	s := config.Service{Name: "php-fpm", Manager: "init", Program: "/etc/init.d/php-fpm"}
	run := func(ctx context.Context, program string, args ...string) (string, error) {
		copied := append([]string(nil), args...)
		if program == s.Program {
			program = script
		} else if program == serviceScopeProgram {
			if len(copied) < 7 || copied[4] != "--" || copied[5] != s.Program {
				return "", errors.New("unexpected scope command")
			}
			copied[5] = script
		} else {
			return "", fmt.Errorf("unexpected fixture command: %s", program)
		}
		return RunCommand(ctx, program, copied...)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	var err error
	if mode == "raw" {
		_, err = Execute(ctx, s, action, run)
	} else {
		_, err = ExecuteManaged(ctx, s, action, run)
	}
	if err != nil {
		t.Fatal(err)
	}
	if os.Getenv("GOPANEL_TEST_SERVICE_SCOPE_ONESHOT") == "1" {
		return
	}
	// The simulated panel stays running until the test restarts or stops its unit.
	select {}
}
