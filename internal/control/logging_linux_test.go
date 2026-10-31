//go:build linux

package control

import (
	"bytes"
	"context"
	"errors"
	"os"
	"os/exec"
	"os/user"
	"path/filepath"
	"strings"
	"syscall"
	"testing"

	"github.com/azpanel/lnmp-gopanel/internal/config"
	"github.com/azpanel/lnmp-gopanel/internal/logs"
)

type loggingFixture struct {
	p           loggingController
	restarts    int
	checks      int
	failCheck   int
	failRestart bool
	stopped     bool
	beforeCheck func(int)
}

func newLoggingFixture(t *testing.T) *loggingFixture {
	t.Helper()
	if os.Geteuid() != 0 {
		t.Skip("root ownership/chown tests require root in a disposable directory")
	}
	f := &loggingFixture{}
	f.p = defaultLoggingController()
	f.p.root = t.TempDir()
	f.p.trust = func(string) error { return nil }
	f.p.lock = func(string) (func(), error) { return func() {}, nil }
	f.p.lookup = func(string) (int, int, error) { return 12345, 12345, nil }
	f.p.group = func(string) (int, error) { return 12345, nil }
	f.p.run = func(_ context.Context, _ string, _ ...string) (string, error) {
		f.checks++
		if f.beforeCheck != nil {
			f.beforeCheck(f.checks)
		}
		if f.checks == f.failCheck {
			return "", errors.New("invalid config")
		}
		return "", nil
	}
	f.p.execute = func(_ context.Context, _ config.Service, action string) (Result, error) {
		if action == "restart" {
			f.restarts++
			if f.failRestart && f.restarts == 1 {
				return Result{}, errors.New("restart failed")
			}
		}
		state := "running"
		if f.stopped {
			state = "stopped"
		}
		return Result{State: state}, nil
	}
	f.put(t, nginxManagementConfig, "services:\n  - name: php-fpm\n    manager: init\n    program: /etc/init.d/php-fpm\n  - name: mysql\n    manager: init\n    program: /etc/init.d/mysql\n")
	f.put(t, loggingPHPConfig, "; keep global\n[global]\npid = /run/php-fpm.pid\n[www]\nuser = www\ngroup = www\nlisten = /tmp/php.sock\n; keep pool\nphp_admin_flag[log_errors] = off\n[other]\nuser = nobody\n")
	f.put(t, loggingDBConfig, "# keep database\n[mysqld]\nuser = mysql\nslow-query-log = 0\nlog_output = TABLE\nmax_connections = 200\n[client]\nuser = root\n")
	return f
}
func (f *loggingFixture) put(t *testing.T, path, data string) {
	t.Helper()
	full := f.p.path(path)
	if err := os.MkdirAll(filepath.Dir(full), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(full, []byte(data), 0644); err != nil {
		t.Fatal(err)
	}
}
func (f *loggingFixture) data(t *testing.T, path string) []byte {
	t.Helper()
	data, err := os.ReadFile(f.p.path(path))
	if err != nil {
		t.Fatal(err)
	}
	return data
}
func TestEnableLoggingCreatesLogsAndIsIdempotent(t *testing.T) {
	for _, kind := range []string{"php", "database"} {
		t.Run(kind, func(t *testing.T) {
			f := newLoggingFixture(t)
			result, err := f.p.enable(context.Background(), kind)
			expectedChecks := 0
			if kind == "php" {
				expectedChecks = 2
			}
			if err != nil || result.State != "running" || f.restarts != 1 || f.checks != expectedChecks {
				t.Fatal(result, err, f.restarts, f.checks)
			}
			path := logs.PHP
			section := "www"
			configPath := loggingPHPConfig
			if kind == "database" {
				path = logs.MySQL
				section = "mysqld"
				configPath = loggingDBConfig
			}
			info, err := os.Stat(f.p.path(path))
			if err != nil {
				t.Fatal(err)
			}
			stat := info.Sys().(*syscall.Stat_t)
			if stat.Uid != 12345 || stat.Gid != 12345 || info.Mode().Perm() != 0640 {
				t.Fatal(info, stat)
			}
			data := f.data(t, configPath)
			opts := loggingOptions(data, section)
			if kind == "php" {
				if opts["php_admin_value[error_log]"] != path || opts["php_admin_flag[log_errors]"] != "on" || !bytes.Contains(data, []byte("; keep pool")) || loggingOptions(data, "other")["php_admin_flag[log_errors]"] != "" {
					t.Fatal(string(data))
				}
			} else if opts["long_query_time"] != "2" || opts["log_output"] != "FILE,TABLE" || opts["slow_query_log"] != "1" || opts["slow_query_log_file"] != path || opts["max_connections"] != "200" {
				t.Fatal(string(data))
			}
			if _, err = f.p.enable(context.Background(), kind); err != nil || f.restarts != 1 || !bytes.Equal(data, f.data(t, configPath)) {
				t.Fatal(err, f.restarts)
			}
			backups, _ := filepath.Glob(f.p.path("/var/backups/lnmp-gopanel/logging-*"))
			if len(backups) != 1 {
				t.Fatal(backups)
			}
		})
	}
}
func TestLoggingPreservesThresholdAndExistingLog(t *testing.T) {
	f := newLoggingFixture(t)
	f.put(t, loggingDBConfig, "[mysqld]\nlong_query_time = 0.75 # keep threshold\nlog_output = FILE\n")
	f.put(t, logs.MySQL, "existing slow query\n")
	if _, err := f.p.enable(context.Background(), "database"); err != nil {
		t.Fatal(err)
	}
	if string(f.data(t, logs.MySQL)) != "existing slow query\n" || !strings.Contains(string(f.data(t, loggingDBConfig)), "long_query_time = 0.75 # keep threshold") {
		t.Fatal("log or threshold overwritten")
	}
}
func TestLoggingPHPIncludesAndPoolScope(t *testing.T) {
	f := newLoggingFixture(t)
	main := "[global]\ninclude = /usr/local/php/etc/pools/*.conf\n"
	f.put(t, loggingPHPConfig, main)
	f.put(t, "/usr/local/php/etc/pools/www.conf", "[www]\nuser = www\ngroup = www\nlisten = /tmp/php.sock\n")
	f.put(t, "/usr/local/php/etc/pools/other.conf", "[other]\nuser = nobody\n")
	if _, err := f.p.enable(context.Background(), "php"); err != nil {
		t.Fatal(err)
	}
	if string(f.data(t, loggingPHPConfig)) != main || strings.Contains(string(f.data(t, "/usr/local/php/etc/pools/other.conf")), "php_admin") || !strings.Contains(string(f.data(t, "/usr/local/php/etc/pools/www.conf")), logs.PHP) {
		t.Fatal("wrong pool modified")
	}
}
func TestLoggingFailuresRestoreConfigurationAndPermissions(t *testing.T) {
	for _, kind := range []string{"php", "database"} {
		failures := []string{"restart"}
		if kind == "php" {
			failures = append(failures, "original-check", "new-check")
		}
		for _, failure := range failures {
			t.Run(kind+"/"+failure, func(t *testing.T) {
				f := newLoggingFixture(t)
				switch failure {
				case "original-check":
					f.failCheck = 1
				case "new-check":
					f.failCheck = 2
				case "restart":
					f.failRestart = true
				}
				configPath := loggingPHPConfig
				logPath := logs.PHP
				if kind == "database" {
					configPath = loggingDBConfig
					logPath = logs.MySQL
				}
				original := f.data(t, configPath)
				f.put(t, logPath, "existing log\n")
				_, err := f.p.enable(context.Background(), kind)
				if err == nil || !strings.Contains(err.Error(), "已恢复原配置") || !bytes.Equal(original, f.data(t, configPath)) || string(f.data(t, logPath)) != "existing log\n" {
					t.Fatal(err, string(f.data(t, configPath)))
				}
				info, _ := os.Stat(f.p.path(logPath))
				if info.Mode().Perm() != 0644 || info.Sys().(*syscall.Stat_t).Uid != 0 {
					t.Fatal("permissions not restored")
				}
				expected := 0
				if failure == "restart" {
					expected = 2
				}
				if f.restarts != expected {
					t.Fatal(f.restarts)
				}
			})
		}
	}
}
func TestLoggingRejectsConflictsAndUnsafeFiles(t *testing.T) {
	for _, scenario := range []string{"missing-pool", "wrong-user", "php-cycle", "db-alias", "db-group", "db-include", "db-duplicate", "multiple-databases", "missing-account", "symlink", "hardlink", "symlink-directory", "stopped", "locked", "untrusted-config", "fifo"} {
		t.Run(scenario, func(t *testing.T) {
			f := newLoggingFixture(t)
			kind := "php"
			switch scenario {
			case "missing-pool":
				f.put(t, loggingPHPConfig, "[other]\nuser=www\n")
			case "wrong-user":
				f.put(t, loggingPHPConfig, "[www]\nuser=nobody\n")
			case "php-cycle":
				f.put(t, loggingPHPConfig, "include=/usr/local/php/etc/php-fpm.conf\n")
			case "db-alias":
				kind = "database"
				f.put(t, loggingDBConfig, "[mysqld]\nlog_slow_query=0\n")
			case "db-group":
				kind = "database"
				f.put(t, loggingDBConfig, "[mysqld]\n[mariadb]\nslow_query_log=0\n")
			case "db-include":
				kind = "database"
				f.put(t, loggingDBConfig, "[mysqld]\n!includedir /etc/mysql/conf.d\n")
				f.put(t, "/etc/mysql/conf.d/log.cnf", "[mysqld]\nslow_query_log=0\n")
			case "db-duplicate":
				kind = "database"
				f.put(t, loggingDBConfig, "[mysqld]\n[mysqld]\n")
			case "multiple-databases":
				kind = "database"
				f.put(t, nginxManagementConfig, string(f.data(t, nginxManagementConfig))+"  - name: mariadb\n    manager: init\n    program: /etc/init.d/mariadb\n")
			case "missing-account":
				f.p.lookup = func(string) (int, int, error) { return 0, 0, errors.New("missing www") }
			case "symlink":
				f.put(t, logs.PHP, "log")
				os.Remove(f.p.path(logs.PHP))
				os.Symlink(f.p.path(loggingPHPConfig), f.p.path(logs.PHP))
			case "hardlink":
				f.put(t, logs.PHP, "log")
				os.Link(f.p.path(logs.PHP), f.p.path(logs.PHP)+".link")
			case "symlink-directory":
				f.put(t, logs.PHP, "log")
				os.Rename(f.p.path(filepath.Dir(logs.PHP)), f.p.path(filepath.Dir(logs.PHP))+".old")
				os.Symlink(f.p.path(filepath.Dir(logs.PHP))+".old", f.p.path(filepath.Dir(logs.PHP)))
			case "stopped":
				f.stopped = true
			case "locked":
				f.p.lock = func(string) (func(), error) { return nil, ErrBusy }
			case "untrusted-config":
				os.Chmod(f.p.path(loggingPHPConfig), 0666)
			case "fifo":
				f.put(t, logs.PHP, "log")
				os.Remove(f.p.path(logs.PHP))
				syscall.Mkfifo(f.p.path(logs.PHP), 0600)
			}
			if _, err := f.p.enable(context.Background(), kind); err == nil || f.restarts != 0 {
				t.Fatal(err, f.restarts)
			}
		})
	}
}
func TestLoggingConcurrentEditsAreNotOverwritten(t *testing.T) {
	for _, duringRollback := range []bool{false, true} {
		t.Run(strconvBool(duringRollback), func(t *testing.T) {
			f := newLoggingFixture(t)
			edit := "[www]\nuser=www\nlisten=/changed.sock\n"
			f.beforeCheck = func(n int) {
				if (!duringRollback && n == 1) || (duringRollback && n == 2) {
					f.put(t, loggingPHPConfig, edit)
				}
			}
			if duringRollback {
				f.failCheck = 2
			}
			if _, err := f.p.enable(context.Background(), "php"); err == nil || string(f.data(t, loggingPHPConfig)) != edit || f.restarts != 0 {
				t.Fatal(err)
			}
		})
	}
}
func strconvBool(value bool) string {
	if value {
		return "rollback"
	}
	return "before-write"
}

func TestGeneratedPHPLoggingConfiguration(t *testing.T) {
	binary, err := exec.LookPath("php-fpm")
	if err != nil {
		t.Skip("php-fpm is not installed")
	}
	nobody, err := user.Lookup("nobody")
	if err != nil {
		t.Skip("nobody user is not available")
	}
	dir := t.TempDir()
	original := []byte("[global]\nerror_log = " + filepath.Join(dir, "fpm.log") + "\n[www]\nuser = nobody\ngroup = " + nobody.Gid + "\nlisten = " + filepath.Join(dir, "fpm.sock") + "\npm = static\npm.max_children = 1\n")
	data, err := patchLoggingSection(original, "www", map[string]string{"php_admin_value[error_log]": filepath.Join(dir, "php-errors.log"), "php_admin_flag[log_errors]": "on"})
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "php-fpm.conf")
	if err = os.WriteFile(path, data, 0600); err != nil {
		t.Fatal(err)
	}
	if output, err := exec.CommandContext(t.Context(), binary, "-n", "-t", "-y", path).CombinedOutput(); err != nil {
		t.Fatalf("PHP-FPM rejected generated logging configuration: %v\n%s", err, output)
	}
}

func TestLoggingCancellationStillRestoresService(t *testing.T) {
	f := newLoggingFixture(t)
	original := f.data(t, loggingPHPConfig)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	execute := f.p.execute
	f.p.execute = func(task context.Context, s config.Service, action string) (Result, error) {
		result, err := execute(task, s, action)
		if action == "restart" && f.restarts == 1 {
			cancel()
			return Result{}, task.Err()
		}
		if action == "restart" && task.Err() != nil {
			t.Fatal("recovery inherited cancelled context")
		}
		return result, err
	}
	if _, err := f.p.enable(ctx, "php"); err == nil || !strings.Contains(err.Error(), "已恢复原配置") || f.restarts != 2 || !bytes.Equal(original, f.data(t, loggingPHPConfig)) {
		t.Fatal(err, f.restarts)
	}
}

// Database enablement must work without executing or trusting an LNMP-owned
// daemon, including the recovery path after a failed restart.
func TestDatabaseLoggingDoesNotExecuteDaemon(t *testing.T) {
	for _, service := range []string{"mysql", "mariadb"} {
		for _, layout := range []string{"missing", "database-owned"} {
			for _, failure := range []bool{false, true} {
				t.Run(service+"/"+layout+"/"+strconvBool(failure), func(t *testing.T) {
					f := newLoggingFixture(t)
					f.failRestart = failure
					f.put(t, nginxManagementConfig, "services:\n  - name: "+service+"\n    manager: init\n    program: /etc/init.d/"+service+"\n")
					prefix := f.p.path("/usr/local/" + service)
					binary := filepath.Join(prefix, "bin", "mysqld")
					if service == "mariadb" {
						binary = filepath.Join(prefix, "bin", "mariadbd")
					}
					if layout == "database-owned" {
						if err := os.MkdirAll(filepath.Dir(binary), 0755); err != nil {
							t.Fatal(err)
						}
						if err := os.WriteFile(binary, []byte("must never execute"), 0755); err != nil {
							t.Fatal(err)
						}
						for _, path := range []string{prefix, filepath.Dir(binary), binary} {
							if err := os.Chown(path, 12345, 12345); err != nil {
								t.Fatal(err)
							}
						}
					}
					f.p.trust = func(path string) error {
						if path == prefix || strings.HasPrefix(path, prefix+"/") {
							t.Errorf("database executable trust check must not run: %s", path)
							return errors.New("database-owned executable")
						}
						return nil
					}
					f.p.run = func(_ context.Context, program string, _ ...string) (string, error) {
						t.Errorf("database configuration check must not execute: %s", program)
						return "", errors.New("unexpected executable")
					}
					services, err := f.p.services()
					if err != nil {
						t.Fatal(err)
					}
					view := f.p.snapshot(context.Background(), services).Database
					if !view.Available || view.Configured {
						t.Fatal(view)
					}
					original := f.data(t, loggingDBConfig)
					_, err = f.p.enable(context.Background(), "database")
					if failure {
						if err == nil || f.restarts != 2 || !bytes.Equal(original, f.data(t, loggingDBConfig)) {
							t.Fatal(err, f.restarts)
						}
					} else {
						if err != nil || f.restarts != 1 {
							t.Fatal(err, f.restarts)
						}
						if !f.p.snapshot(context.Background(), services).Database.Configured {
							t.Fatal("database logging not configured")
						}
					}
				})
			}
		}
	}
}
