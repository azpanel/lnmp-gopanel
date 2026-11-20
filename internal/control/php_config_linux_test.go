//go:build linux

package control

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/azpanel/lnmp-gopanel/internal/config"
)

type phpConfigFixture struct {
	p               phpConfigController
	files           *loggingFixture
	reloads, checks int
	failCheck       int
	failReload      bool
	rejectValue     bool
	beforeReload    func()
	beforeCheck     func(int)
	scans           string
}

func newPHPConfigFixture(t *testing.T) *phpConfigFixture {
	t.Helper()
	files := newLoggingFixture(t)
	f := &phpConfigFixture{files: files, p: phpConfigController{
		loggingController: files.p,
		attest:            func(context.Context) (int, error) { return 100, nil },
	}}
	files.put(t, phpFPMPIDFile, "100")
	files.put(t, phpINIConfig, "; keep me\n[PHP]\nupload_max_filesize = 50M\npost_max_size = 50M\nmemory_limit = 128M\nmax_execution_time = 300\nmax_input_time = 60\ndate.timezone = PRC\ndisplay_errors = Off\ndisable_functions = exec,shell_exec\n[PATH=/example]\nmemory_limit = 32M\n")
	files.put(t, loggingPHPConfig, "[global]\npid = /run/php-fpm.pid\n[www]\nlisten=/tmp/php.sock\nphp_admin_value[disable_functions] = system,exec\nphp_admin_value[memory_limit] = 64M\n")
	f.p.run = func(_ context.Context, _ string, args ...string) (string, error) {
		if len(args) > 0 && args[0] == "-t" {
			f.checks++
			if f.beforeCheck != nil {
				f.beforeCheck(f.checks)
			}
			if f.checks == f.failCheck {
				return "", errors.New("bad config")
			}
			return "", nil
		}
		if !slices.Equal(args, []string{"-i", "-c", f.p.path(phpINIConfig)}) {
			t.Fatal("unexpected command", args)
		}
		data := files.data(t, phpINIConfig)
		values := phpINIValues(data)
		paths, _ := filepath.Glob(f.p.path(f.scans) + "/*.ini")
		if f.scans != "" {
			for _, path := range paths {
				data, e := os.ReadFile(path)
				if e != nil {
					t.Fatal(e)
				}
				for key, value := range phpINIValues(data) {
					values[key] = value
				}
			}
		}
		if f.rejectValue {
			values["memory_limit"] = "128M"
		}
		var out strings.Builder
		fmt.Fprintf(&out, "Loaded Configuration File => %s\nScan this dir for additional .ini files => %s\n", phpINIConfig, f.scans)
		for key, value := range values {
			fmt.Fprintf(&out, "%s => %s => %s\n", key, value, value)
		}
		return out.String(), nil
	}
	f.p.execute = func(_ context.Context, _ config.Service, action string) (Result, error) {
		if action == "reload" {
			f.reloads++
			if f.beforeReload != nil {
				f.beforeReload()
			}
			if f.failReload && f.reloads == 1 {
				return Result{}, errors.New("reload failed")
			}
			if err := os.Remove(f.p.path(phpFPMPIDFile)); err != nil {
				t.Fatal(err)
			}
			files.put(t, phpFPMPIDFile, "100")
		}
		state := "running"
		if files.stopped {
			state = "stopped"
		}
		return Result{State: state}, nil
	}
	return f
}

func TestPHPConfigCorrectsFunctionCase(t *testing.T) {
	f := newPHPConfigFixture(t)
	ini := strings.Replace(string(f.files.data(t, phpINIConfig)), "exec,shell_exec", "EXEC Shell_Exec", 1)
	f.files.put(t, phpINIConfig, ini)
	f.files.put(t, loggingPHPConfig, "[www]\nphp_admin_value[disable_functions] = SYSTEM\n")
	state, err := f.p.snapshot(context.Background())
	if err != nil || !slices.Equal(state.snapshot.DisableFunctions, []string{"EXEC", "Shell_Exec"}) ||
		len(state.snapshot.Pools) != 1 || !slices.Equal(state.snapshot.Pools[0].DisableFunctions, []string{"SYSTEM"}) {
		t.Fatal(state, err)
	}

	r := f.request(t)
	names := []string{"exec", "Shell_Exec"}
	r.DisableFunctions = &names
	result, err := f.p.update(context.Background(), r)
	if err != nil || f.reloads != 1 || !slices.Equal(result.DisableFunctions, []string{"Shell_Exec", "exec"}) {
		t.Fatal(result, err, f.reloads)
	}
	if data := f.files.data(t, phpINIConfig); !bytes.Contains(data, []byte(`disable_functions = "Shell_Exec,exec"`)) {
		t.Fatal(string(data))
	}
}

func TestPHPConfigWaitsForNewPIDFile(t *testing.T) {
	f := newPHPConfigFixture(t)
	before, err := f.p.masterPID(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	defer before.file.Close()
	service := config.Service{Name: "php-fpm"}
	ctx, cancel := context.WithTimeout(context.Background(), 350*time.Millisecond)
	defer cancel()
	if err := f.p.waitReload(ctx, service, before.info); err == nil {
		t.Fatal("status of the old master was accepted")
	}

	if _, err := f.p.execute(context.Background(), service, "reload"); err != nil {
		t.Fatal(err)
	}
	ctx, cancel = context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if err := f.p.waitReload(ctx, service, before.info); err != nil {
		t.Fatal("a new PID file with the same master PID must be accepted", err)
	}
}

func TestPHPConfigRejectsMismatchedPIDFile(t *testing.T) {
	f := newPHPConfigFixture(t)
	f.files.put(t, phpFPMPIDFile, "999")
	state, err := f.p.snapshot(context.Background())
	if err != nil || state.snapshot.Editable || !strings.Contains(state.snapshot.Message, "PID 文件与主进程不匹配") {
		t.Fatal(state, err)
	}
}

func (f *phpConfigFixture) request(t *testing.T) PHPConfigRequest {
	t.Helper()
	state, err := f.p.snapshot(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	return PHPConfigRequest{Revision: state.snapshot.Revision, Fields: map[string]string{}}
}
func TestPHPConfigListUpdateAndNoop(t *testing.T) {
	f := newPHPConfigFixture(t)
	state, err := f.p.snapshot(context.Background())
	if err != nil || !state.snapshot.Editable || state.snapshot.Fields["memory_limit"].Value != "128M" || strings.Join(state.snapshot.DisableFunctions, ",") != "exec,shell_exec" || len(state.snapshot.Pools) != 1 || strings.Join(state.snapshot.Pools[0].DisableFunctions, ",") != "exec,system" {
		t.Fatal(state, err)
	}
	r := f.request(t)
	r.Fields["memory_limit"] = "256M"
	names := []string{"EXEC", "system", "exec"}
	r.DisableFunctions = &names
	result, err := f.p.update(context.Background(), r)
	if err != nil || f.reloads != 1 || result.Fields["memory_limit"].Value != "256M" || strings.Join(result.DisableFunctions, ",") != "exec,system" {
		t.Fatal(result, err, f.reloads)
	}
	data := f.files.data(t, phpINIConfig)
	if !bytes.Contains(data, []byte("; keep me")) || !bytes.Contains(data, []byte("[PATH=/example]\nmemory_limit = 32M")) || !bytes.Contains(data, []byte("; memory_limit = 128M")) {
		t.Fatal(string(data))
	}
	backups, _ := filepath.Glob(f.p.path("/var/backups/lnmp-gopanel/php-config-*/php.ini"))
	if len(backups) != 1 {
		t.Fatal(backups)
	}
	info, _ := os.Stat(backups[0])
	if info.Mode().Perm() != 0600 {
		t.Fatal(info.Mode())
	}
	r = f.request(t)
	r.Fields["memory_limit"] = "256m"
	if _, err = f.p.update(context.Background(), r); err != nil || f.reloads != 1 || !bytes.Equal(data, f.files.data(t, phpINIConfig)) {
		t.Fatal(err, f.reloads)
	}
	r = f.request(t)
	names = []string{}
	r.DisableFunctions = &names
	result, err = f.p.update(context.Background(), r)
	if err != nil || len(result.DisableFunctions) != 0 || len(result.Pools[0].DisableFunctions) != 2 {
		t.Fatal(result, err)
	}
}
func TestPHPConfigFailuresAndRecovery(t *testing.T) {
	for _, scenario := range []string{"stale", "stopped", "busy", "old-check", "new-check", "rejected", "reload", "concurrent-before-write", "concurrent-after-write", "concurrent-recovery"} {
		t.Run(scenario, func(t *testing.T) {
			f := newPHPConfigFixture(t)
			r := f.request(t)
			r.Fields["memory_limit"] = "256M"
			original := f.files.data(t, phpINIConfig)
			switch scenario {
			case "stale":
				r.Revision = strings.Repeat("0", 64)
			case "stopped":
				f.files.stopped = true
			case "busy":
				f.p.lock = func(string) (func(), error) { return nil, ErrBusy }
			case "old-check":
				f.failCheck = 1
			case "new-check":
				f.failCheck = 2
			case "rejected":
				f.rejectValue = true
			case "reload":
				f.failReload = true
			case "concurrent-before-write":
				f.beforeCheck = func(n int) {
					if n == 1 {
						f.files.put(t, phpINIConfig, string(original)+"; external edit\n")
					}
				}
			case "concurrent-after-write":
				f.beforeCheck = func(n int) {
					if n == 2 {
						f.files.put(t, loggingPHPConfig, "[www]\nlisten = /changed.sock\n")
					}
				}
			case "concurrent-recovery":
				f.failReload = true
				f.beforeReload = func() { f.files.put(t, phpINIConfig, "; external edit\nmemory_limit=512M\n") }
			}
			_, err := f.p.update(context.Background(), r)
			if err == nil {
				t.Fatal("unexpected success")
			}
			current := f.files.data(t, phpINIConfig)
			if scenario == "concurrent-before-write" || scenario == "concurrent-recovery" {
				if !bytes.Contains(current, []byte("external edit")) {
					t.Fatal("overwrote external edit", err)
				}
			} else if !bytes.Equal(original, current) {
				t.Fatal("original not restored", string(current), err)
			}
			if scenario == "reload" {
				if f.reloads != 2 || !strings.Contains(err.Error(), "已恢复原配置") {
					t.Fatal(f.reloads, err)
				}
			} else if scenario != "concurrent-recovery" && f.reloads != 0 {
				t.Fatal(f.reloads, err)
			}
		})
	}
}
func TestPHPConfigSourceOverridesAndIncludes(t *testing.T) {
	f := newPHPConfigFixture(t)
	f.scans = "/usr/local/php/conf.d"
	f.files.put(t, f.scans+"/10-limit.ini", "memory_limit=64M\ndisable_functions=system\n")
	f.files.put(t, loggingPHPConfig, "[global]\ninclude=/usr/local/php/etc/pools/*.conf\n")
	f.files.put(t, "/usr/local/php/etc/pools/a.conf", "[first]\nlisten=/tmp/a.sock\nphp_value[disable_functions]=popen\n")
	f.files.put(t, "/usr/local/php/etc/pools/b.conf", "[second]\nlisten=/tmp/b.sock\n")
	state, err := f.p.snapshot(context.Background())
	if err != nil || state.snapshot.Fields["memory_limit"].Editable || state.snapshot.DisableFunctionsEditable || len(state.snapshot.Pools) != 2 || strings.Join(state.snapshot.DisableFunctions, ",") != "system" {
		t.Fatal(state, err)
	}
	r := f.request(t)
	r.Fields["memory_limit"] = "256M"
	if _, err = f.p.update(context.Background(), r); err == nil || f.reloads != 0 {
		t.Fatal(err)
	}
	r = f.request(t)
	names := []string{}
	r.DisableFunctions = &names
	if _, err = f.p.update(context.Background(), r); err == nil {
		t.Fatal("accepted overridden function list")
	}
	r = f.request(t)
	r.Fields["max_execution_time"] = "120"
	if _, err = f.p.update(context.Background(), r); err != nil {
		t.Fatal(err)
	}
	r = f.request(t)
	r.Fields["max_execution_time"] = "130"
	f.files.put(t, f.scans+"/99.ini", "max_input_time=45\n")
	if _, err = f.p.update(context.Background(), r); err == nil {
		t.Fatal("new source did not invalidate revision")
	}
}
func TestPHPConfigRejectsUnsafeSources(t *testing.T) {
	for _, scenario := range []string{"symlink", "hardlink", "writable", "include-cycle", "relative-include", "outside-include", "pool-include", "scan-outside"} {
		t.Run(scenario, func(t *testing.T) {
			f := newPHPConfigFixture(t)
			switch scenario {
			case "symlink":
				target := f.p.path(phpINIConfig)
				os.Rename(target, target+".real")
				os.Symlink(target+".real", target)
			case "hardlink":
				os.Link(f.p.path(phpINIConfig), f.p.path(phpINIConfig)+".link")
			case "writable":
				os.Chmod(f.p.path(phpINIConfig), 0666)
			case "include-cycle":
				f.files.put(t, loggingPHPConfig, "include="+loggingPHPConfig+"\n")
			case "relative-include":
				f.files.put(t, loggingPHPConfig, "include=pools/*.conf\n")
			case "outside-include":
				f.files.put(t, "/etc/outside.conf", "[www]\n")
				f.files.put(t, loggingPHPConfig, "include=/etc/outside.conf\n")
			case "pool-include":
				f.files.put(t, loggingPHPConfig, "[www]\ninclude=/usr/local/php/etc/*.conf\n")
			case "scan-outside":
				f.scans = "/etc/php.d"
			}
			if _, err := f.p.snapshot(context.Background()); err == nil {
				t.Fatal("unsafe source accepted")
			}
		})
	}
}
func TestPHPConfigPatcherWithRealFPM(t *testing.T) {
	binary, err := exec.LookPath("php-fpm")
	if err != nil {
		t.Skip("php-fpm not installed")
	}
	data := []byte("; preserve\r\n[PHP]\r\nmemory_limit=64M\r\nmemory_limit=128M\r\ndisable_functions=exec\r\n[Date]\r\ndate.timezone=UTC\r\n[PATH=/example]\r\nmemory_limit=16M\r\n")
	values := map[string]string{"memory_limit": "256M", "disable_functions": "shell_exec,system", "max_input_time": "-1", "display_errors": "Off", "date.timezone": "Asia/Shanghai"}
	path := filepath.Join(t.TempDir(), "php.ini")
	if err = os.WriteFile(path, patchPHPINI(data, values), 0600); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(binary, "-c", path, "-i")
	cmd.Env = append(os.Environ(), "PHP_INI_SCAN_DIR=")
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatal(err, string(output))
	}
	actual := phpInfoValues(string(output))
	keys := []string{}
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		if !phpEquivalent(key, actual[key], values[key]) {
			t.Fatal(key, actual[key], values[key])
		}
	}
}

func TestPHPConfigRuntimeAttestation(t *testing.T) {
	if os.Geteuid() != 0 {
		t.Skip("root master ownership")
	}
	for _, scenario := range []string{"standard", "unverified-arguments", "custom-config", "unreadable-descriptors", "multiple", "absent"} {
		t.Run(scenario, func(t *testing.T) {
			root := t.TempDir()
			program := filepath.Join(root, "php-fpm")
			if err := os.WriteFile(program, []byte("binary fixture"), 0700); err != nil {
				t.Fatal(err)
			}
			proc := filepath.Join(root, "proc")
			os.Mkdir(proc, 0700)
			count := 1
			if scenario == "multiple" {
				count = 2
			}
			if scenario == "absent" {
				count = 0
			}
			for i := 0; i < count; i++ {
				base := filepath.Join(proc, fmt.Sprint(100+i))
				os.Mkdir(base, 0700)
				os.Symlink(program, filepath.Join(base, "exe"))
				path := loggingPHPConfig
				if scenario == "custom-config" {
					path = "/other/php-fpm.conf"
				}
				os.WriteFile(filepath.Join(base, "cmdline"), []byte("php-fpm: master process ("+path+")\x00"), 0600)
				if scenario != "unreadable-descriptors" {
					dir := filepath.Join(base, "fd")
					if err := os.Mkdir(dir, 0700); err != nil {
						t.Fatal(err)
					}
					if scenario != "unverified-arguments" {
						if err := os.Symlink(phpFPMLaunchFDTarget, filepath.Join(dir, "3")); err != nil {
							t.Fatal(err)
						}
					}
				}
			}
			pid, err := attestPHPConfigRuntimeAt(context.Background(), proc, program)
			if scenario == "standard" && (err != nil || pid != 100) ||
				scenario != "standard" && (err == nil || pid != 0) {
				t.Fatal(scenario, pid, err)
			}
		})
	}
}

func TestPHPConfigLauncherRejectsCustomINIEnvironment(t *testing.T) {
	for _, key := range []string{"PHPRC", "PHP_INI_SCAN_DIR"} {
		t.Run(key, func(t *testing.T) {
			t.Setenv(key, "")
			if err := LaunchStandardPHPFPM("foreground"); err == nil || !strings.Contains(err.Error(), "自定义 INI 环境") {
				t.Fatal(err)
			}
		})
	}
}

func TestPHPConfigBlocksUnverifiedRuntimeAndNewInclude(t *testing.T) {
	f := newPHPConfigFixture(t)
	attest := f.p.attest
	f.p.attest = func(context.Context) (int, error) { return 0, errors.New("自定义配置来源") }
	state, err := f.p.snapshot(context.Background())
	if err != nil || state.snapshot.Editable || !strings.Contains(state.snapshot.Message, "自定义配置来源") {
		t.Fatal(state, err)
	}
	r := PHPConfigRequest{Revision: state.snapshot.Revision, Fields: map[string]string{"memory_limit": "256M"}}
	if _, err = f.p.update(context.Background(), r); err == nil || f.reloads != 0 {
		t.Fatal(err)
	}
	f.p.attest = attest
	f.scans = "/usr/local/php/conf.d"
	if err = os.MkdirAll(f.p.path(f.scans), 0755); err != nil {
		t.Fatal(err)
	}
	r = f.request(t)
	r.Fields["memory_limit"] = "256M"
	original := f.files.data(t, phpINIConfig)
	f.beforeCheck = func(n int) {
		if n == 2 {
			f.files.put(t, f.scans+"/added.ini", "max_input_time=45\n")
		}
	}
	if _, err = f.p.update(context.Background(), r); err == nil || f.reloads != 0 || !bytes.Equal(original, f.files.data(t, phpINIConfig)) {
		t.Fatal("new INI source not detected", err)
	}
}
