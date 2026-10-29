package metricssetup

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/azpanel/lnmp-gopanel/internal/config"
	"golang.org/x/crypto/bcrypt"
)

type fakeAccount struct {
	exists, created, dropped bool
	failCreate, failDrop     bool
	createCalls              int
}

func (a *fakeAccount) Exists(context.Context, string, string) (bool, error) { return a.exists, nil }
func (a *fakeAccount) Create(context.Context, string, string, string) error {
	a.createCalls++
	if a.failCreate {
		return errors.New("create failed")
	}
	a.exists = true
	a.created = true
	return nil
}
func (a *fakeAccount) Drop(context.Context, string, string) error {
	if a.failDrop {
		return errors.New("drop failed")
	}
	a.dropped = true
	a.exists = false
	return nil
}
func (*fakeAccount) Close() error { return nil }

type fixture struct {
	e       *environment
	log     []string
	panel   bool
	account *fakeAccount
	config  string
}

func setupFixture(t *testing.T) *fixture {
	t.Helper()
	root := t.TempDir()
	f := &fixture{panel: true, account: &fakeAccount{}, config: "/etc/lnmp-gopanel/config.yaml"}
	e := &environment{root: root, fileGroup: os.Getgid(), write: func(path string, data []byte, mode os.FileMode, gid int) error {
		return atomicWriteOwned(path, data, mode, os.Geteuid(), gid)
	}}
	f.e = e
	// Trust fixtures relative to their private root, without trusting /tmp itself.
	e.trust = func(path string) error {
		if !strings.HasPrefix(path, root+"/") && path != root {
			return errors.New("outside fixture")
		}
		for current := path; ; current = filepath.Dir(current) {
			info, err := os.Lstat(current)
			if err != nil {
				return err
			}
			if info.Mode()&os.ModeSymlink != 0 || info.Mode().Perm()&0022 != 0 {
				return errors.New("untrusted fixture")
			}
			if current == root {
				return nil
			}
		}
	}
	e.run = func(_ context.Context, program string, args ...string) error {
		f.log = append(f.log, strings.TrimPrefix(program, root)+" "+strings.Join(args, " "))
		return nil
	}
	e.active = func(_ context.Context, name string) bool {
		if name == "panel" {
			return f.panel
		}
		return true
	}
	e.lock = func() (func(), error) { return func() {}, nil }
	e.portFree = func(int) bool { return true }
	e.admin = func(context.Context, config.MetricsMySQLConfig) (accountAdmin, error) { return f.account, nil }
	e.probe = func(context.Context, config.MetricsConfig, []string) error { return nil }
	e.userCheck = func(_ context.Context, _ string, ids []string) error {
		f.log = append(f.log, "check "+strings.Join(ids, ","))
		return nil
	}
	for _, directory := range []string{"/var/backups/lnmp-gopanel", "/etc/lnmp-gopanel", "/usr/local/nginx/conf", "/usr/local/php/etc/php-fpm.d"} {
		if err := os.MkdirAll(e.path(directory), 0755); err != nil {
			t.Fatal(err)
		}
	}
	for _, program := range []string{"/usr/bin/sudo", "/usr/bin/systemctl", "/usr/local/bin/lnmp-gopanel", nginxBinary, fpmBinary, "/etc/init.d/php-fpm"} {
		writeFixture(t, e, program, "test program", 0755)
	}
	hashed, _ := bcrypt.GenerateFromPassword([]byte("fixture-password"), bcrypt.MinCost)
	writeFixture(t, e, f.config, "# retain comment\nusername: admin\npassword_hash: "+string(hashed)+"\nlisten: 127.0.0.1:8090\ntls:\n  cert_file: /cert\n  key_file: /key\n", 0640)
	writeFixture(t, e, nginxConfig, "events {}\nhttp { server { listen 443 ssl; location /nginx_status { stub_status; } } }\n", 0644)
	writeFixture(t, e, fpmConfig, "[global]\n[www]\nlisten = /tmp/php-cgi.sock\npm = dynamic\npm.max_children = 20\n", 0644)
	return f
}
func writeFixture(t *testing.T, e *environment, path, data string, mode os.FileMode) {
	t.Helper()
	full := e.path(path)
	if err := os.MkdirAll(filepath.Dir(full), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(full, []byte(data), mode); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(full, mode); err != nil {
		t.Fatal(err)
	}
}
func fixtureConsole(input string) (*Console, *bytes.Buffer) {
	buffer := &bytes.Buffer{}
	reader := strings.NewReader(input)
	c := &Console{Input: reader, Output: buffer}
	c.Secret = func(string) ([]byte, error) { value, err := c.line("secret", ""); return []byte(value), err }
	return c, buffer
}
func collectNginx(t *testing.T, f *fixture) *proposal {
	t.Helper()
	c, _ := fixtureConsole("y\n1\nn\nn\n\n")
	p, err := f.e.collect(context.Background(), f.config, c)
	if err != nil {
		t.Fatal(err)
	}
	return p
}
func originalFiles(t *testing.T, f *fixture) map[string][]byte {
	t.Helper()
	out := map[string][]byte{}
	for _, path := range []string{f.config, nginxConfig, fpmConfig} {
		data, err := os.ReadFile(f.e.path(path))
		if err != nil {
			t.Fatal(err)
		}
		out[path] = data
	}
	return out
}
func assertRestored(t *testing.T, f *fixture, before map[string][]byte) {
	t.Helper()
	for path, data := range before {
		actual, err := os.ReadFile(f.e.path(path))
		if err != nil || !bytes.Equal(data, actual) {
			t.Fatal("file was not restored", path, err)
		}
	}
	for _, path := range []string{managedConfig, "/etc/lnmp-gopanel/metrics-setup.json"} {
		if _, err := os.Stat(f.e.path(path)); !os.IsNotExist(err) {
			t.Fatal("new file remains", path)
		}
	}
}

func TestWizardCancelAndEOFHaveNoSideEffects(t *testing.T) {
	for _, input := range []string{"y\n1\nn\nn\n\nn\n", "y\n1\nn\nn\n\n", "n\nn\nn\n"} {
		f := setupFixture(t)
		before := originalFiles(t, f)
		c, output := fixtureConsole(input)
		if err := f.e.configure(context.Background(), f.config, c); err != nil {
			t.Fatal(err)
		}
		assertRestored(t, f, before)
		for _, line := range f.log {
			if strings.Contains(line, "reload") || strings.Contains(line, "restart") {
				t.Fatal("service changed before confirmation")
			}
		}
		if strings.Contains(output.String(), "password_hash") {
			t.Fatal("configuration leaked")
		}
	}
}
func TestNginxSetupIsLocalPreservesPublicRouteAndIsIdempotent(t *testing.T) {
	f := setupFixture(t)
	original := originalFiles(t, f)
	p := collectNginx(t, f)
	if len(f.log) != 1 || !strings.Contains(f.log[0], "-t") {
		t.Fatal("collection changed services", f.log)
	}
	backup, err := f.e.apply(context.Background(), p)
	if err != nil {
		t.Fatal(err)
	}
	if backup == "" {
		t.Fatal("no backup")
	}
	main, _ := os.ReadFile(f.e.path(nginxConfig))
	if !bytes.Contains(main, []byte("listen 443 ssl; location /nginx_status { stub_status; }")) {
		t.Fatal("public status page changed")
	}
	generated, _ := os.ReadFile(f.e.path(managedConfig))
	if !bytes.Contains(generated, []byte("listen 127.0.0.1:18080")) {
		t.Fatal("monitor is not loopback-only")
	}
	for _, path := range []string{f.config, nginxConfig} {
		info, _ := os.Stat(f.e.path(path))
		if info.Mode().Perm() != map[string]os.FileMode{f.config: 0640, nginxConfig: 0644}[path] {
			t.Fatal("wrong permissions")
		}
	}
	backups, _ := filepath.Glob(f.e.path(backup) + "/*.before")
	if len(backups) < 2 {
		t.Fatal("missing configuration backups")
	}
	for _, path := range backups {
		info, _ := os.Stat(path)
		if info.Mode().Perm() != 0600 {
			t.Fatal("backup is not private")
		}
	}
	if reflect.DeepEqual(original[f.config], mustRead(t, f.e.path(f.config))) {
		t.Fatal("panel metrics not updated")
	}
	f.log = nil
	c, _ := fixtureConsole("y\nn\n1\nn\nn\n\n")
	again, err := f.e.collect(context.Background(), f.config, c)
	if err != nil {
		t.Fatal(err)
	}
	if len(again.changes) != 0 || again.nginxReload || again.phpReload {
		t.Fatal("repeat setup is not idempotent", len(again.changes))
	}
	if _, err = f.e.apply(context.Background(), again); err != nil {
		t.Fatal(err)
	}
	for _, line := range f.log {
		if strings.Contains(line, "reload") || strings.Contains(line, "restart") {
			t.Fatal("idempotent setup reloaded services")
		}
	}
}
func mustRead(t *testing.T, path string) []byte {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return data
}
func TestPHPSetupSelectsPoolFromIncludesAndReusesSocket(t *testing.T) {
	f := setupFixture(t)
	writeFixture(t, f.e, fpmConfig, "[global]\ninclude = /usr/local/php/etc/php-fpm.d/*.conf\n", 0644)
	poolPath := "/usr/local/php/etc/php-fpm.d/www.conf"
	writeFixture(t, f.e, poolPath, "; keep\n[www]\nlisten = /tmp/php-cgi.sock\n[other]\nlisten = /tmp/other.sock\n", 0644)
	c, _ := fixtureConsole("n\ny\n1\nn\n\n1\n")
	p, err := f.e.collect(context.Background(), f.config, c)
	if err != nil {
		t.Fatal(err)
	}
	if !p.phpReload || !p.nginxReload || p.options.NginxStatusURL != "" {
		t.Fatal("unselected metric enabled")
	}
	if _, err = f.e.apply(context.Background(), p); err != nil {
		t.Fatal(err)
	}
	changed := mustRead(t, f.e.path(poolPath))
	if bytes.Count(changed, []byte("pm.status_path = /fpm-status")) != 1 || !bytes.Contains(changed, []byte("[other]\nlisten = /tmp/other.sock")) {
		t.Fatal("wrong pool changed")
	}
	if !bytes.Contains(mustRead(t, f.e.path(managedConfig)), []byte("fastcgi_pass unix:/tmp/php-cgi.sock")) {
		t.Fatal("business socket not reused")
	}
	if bytes.Contains(mustRead(t, f.e.path(managedConfig)), []byte("stub_status")) {
		t.Fatal("nginx metric selected implicitly")
	}
}
func TestConcurrentEditsAndUnmanagedFilesArePreserved(t *testing.T) {
	f := setupFixture(t)
	p := collectNginx(t, f)
	writeFixture(t, f.e, nginxConfig, "http { # changed\n }", 0644)
	if _, err := f.e.apply(context.Background(), p); err == nil {
		t.Fatal("concurrent edit overwritten")
	}
	if !strings.Contains(string(mustRead(t, f.e.path(nginxConfig))), "changed") {
		t.Fatal("edit lost")
	}
	f = setupFixture(t)
	writeFixture(t, f.e, managedConfig, "# administrator file\n", 0644)
	c, _ := fixtureConsole("y\n1\nn\nn\n")
	if _, err := f.e.collect(context.Background(), f.config, c); err == nil {
		t.Fatal("unmanaged file overwritten")
	}
}
func TestFailuresRollBackOnlyThisTransaction(t *testing.T) {
	for _, stage := range []string{"write", "nginx-test", "nginx-reload", "probe", "panel-restart", "account-probe", "php-test", "php-reload"} {
		t.Run(stage, func(t *testing.T) {
			f := setupFixture(t)
			before := originalFiles(t, f)
			p := collectNginx(t, f)
			if strings.HasPrefix(stage, "php-") {
				console, _ := fixtureConsole("y\n1\ny\n1\nn\n\n1\n")
				var collectErr error
				p, collectErr = f.e.collect(context.Background(), f.config, console)
				if collectErr != nil {
					t.Fatal(collectErr)
				}
			}
			if stage == "account-probe" {
				p.account = f.account
				p.accountUser = "monitor"
				p.accountHost = "localhost"
				p.accountPassword = "private-generated"
				p.options.MySQL = config.MetricsMySQLConfig{Socket: "/tmp/mysql.sock", User: "monitor", Password: "private-generated"}
				f.e.probe = func(context.Context, config.MetricsConfig, []string) error { return errors.New("no access") }
			}
			if stage == "write" {
				original := f.e.write
				failed := false
				f.e.write = func(path string, data []byte, mode os.FileMode, gid int) error {
					if path == f.e.path(managedConfig) && !failed {
						failed = true
						return errors.New("disk failure")
					}
					return original(path, data, mode, gid)
				}
			}
			originalRun := f.e.run
			failed := false
			f.e.run = func(ctx context.Context, program string, args ...string) error {
				if !failed && (stage == "nginx-test" && strings.HasSuffix(program, nginxBinary) && len(args) > 0 && args[0] == "-t" || stage == "nginx-reload" && strings.Join(args, " ") == "-s reload" || stage == "panel-restart" && strings.Join(args, " ") == "restart lnmp-gopanel" ||
					stage == "php-test" && strings.HasSuffix(program, fpmBinary) && len(args) > 0 && args[0] == "-t" ||
					stage == "php-reload" && strings.HasSuffix(program, "/etc/init.d/php-fpm") && strings.Join(args, " ") == "reload") {
					failed = true
					return errors.New("injected failure")
				}
				return originalRun(ctx, program, args...)
			}
			ctx := context.Background()
			var cancel context.CancelFunc
			if stage == "probe" {
				ctx, cancel = context.WithTimeout(ctx, 20*time.Millisecond)
				defer cancel()
				f.e.userCheck = func(context.Context, string, []string) error { return errors.New("probe failure") }
			}
			backup, err := f.e.apply(ctx, p)
			if err == nil || backup == "" {
				t.Fatal("injected failure ignored", err)
			}
			assertRestored(t, f, before)
			if stage == "account-probe" && !f.account.dropped {
				t.Fatal("new account not removed")
			}
		})
	}
}
func TestExistingAndUncertainAccountsAreNeverDropped(t *testing.T) {
	for _, existing := range []bool{false, true} {
		f := setupFixture(t)
		p := collectNginx(t, f)
		p.account = f.account
		p.accountUser = "monitor"
		p.accountHost = "localhost"
		p.accountPassword = "secret"
		f.account.exists = existing
		f.account.failCreate = !existing
		if _, err := f.e.apply(context.Background(), p); err == nil {
			t.Fatal("account failure ignored")
		}
		if f.account.dropped {
			t.Fatal("account with unconfirmed ownership removed")
		}
	}
}
func TestStoppedPanelStaysStoppedAndRollbackFailureIsExplicit(t *testing.T) {
	f := setupFixture(t)
	f.panel = false
	p := collectNginx(t, f)
	if _, err := f.e.apply(context.Background(), p); err != nil {
		t.Fatal(err)
	}
	for _, line := range f.log {
		if strings.Contains(line, "restart") {
			t.Fatal("stopped panel started")
		}
	}
	f = setupFixture(t)
	p = collectNginx(t, f)
	p.account = f.account
	p.accountUser = "monitor"
	p.accountHost = "localhost"
	f.account.failDrop = true
	f.e.probe = func(context.Context, config.MetricsConfig, []string) error { return errors.New("probe") }
	_, err := f.e.apply(context.Background(), p)
	if err == nil || !strings.Contains(err.Error(), "恢复未完成") || !strings.Contains(err.Error(), "monitor@localhost") {
		t.Fatal("rollback failure not reported", err)
	}
}
func TestAccountCollectionReuseAndCollisionDoNotDiscloseSecrets(t *testing.T) {
	f := setupFixture(t)
	var received config.MetricsMySQLConfig
	f.e.probe = func(_ context.Context, c config.MetricsConfig, _ []string) error { received = c.MySQL; return nil }
	c, output := fixtureConsole("n\nn\ny\n1\n\n2\nexisting\nquote'\\复杂秘密\n")
	p, err := f.e.collect(context.Background(), f.config, c)
	if err != nil {
		t.Fatal(err)
	}
	if p.account != nil || received.User != "existing" || received.Password != "quote'\\复杂秘密" {
		t.Fatal("existing credentials not used")
	}
	if strings.Contains(output.String(), received.Password) {
		t.Fatal("password disclosed")
	}
	f = setupFixture(t)
	f.account.exists = true
	c, _ = fixtureConsole("n\nn\ny\n1\n\n1\n\n\n")
	if _, err = f.e.collect(context.Background(), f.config, c); err == nil || f.account.createCalls != 0 {
		t.Fatal("existing account modified")
	}
}
func TestPortConflictAndManagedRecordTampering(t *testing.T) {
	f := setupFixture(t)
	f.e.portFree = func(port int) bool { return port == 18081 }
	c, _ := fixtureConsole("y\n1\nn\nn\n\n18081\n")
	p, err := f.e.collect(context.Background(), f.config, c)
	if err != nil {
		t.Fatal(err)
	}
	if p.options.NginxStatusURL != "http://127.0.0.1:18081/nginx_status" {
		t.Fatal("port selection ignored")
	}
	if _, err = f.e.apply(context.Background(), p); err != nil {
		t.Fatal(err)
	}
	recordPath := f.e.path("/etc/lnmp-gopanel/metrics-setup.json")
	var state managedState
	if json.Unmarshal(mustRead(t, recordPath), &state) != nil || state.Hash == "" {
		t.Fatal("missing ownership record")
	}
	writeFixture(t, f.e, managedConfig, string(mustRead(t, f.e.path(managedConfig)))+"# hand edit\n", 0644)
	c, _ = fixtureConsole("y\nn\n1\nn\nn\n")
	if _, err = f.e.collect(context.Background(), f.config, c); err == nil {
		t.Fatal("hand-edited managed file overwritten")
	}
}
func TestFPMIncludesAndUnsafeDestinationsFailClosed(t *testing.T) {
	f := setupFixture(t)
	for _, content := range []string{"[global]\ninclude = /usr/local/php/etc/php-fpm.conf\n", "[www]\nlisten = /socket\n[www]\nlisten=/other\n", "[global]\ninclude = relative/*.conf\n"} {
		writeFixture(t, f.e, fpmConfig, content, 0644)
		if _, err := f.e.pools(); err == nil {
			t.Fatal("ambiguous FPM config accepted")
		}
	}
	path := f.e.path(managedConfig)
	if err := os.Symlink(f.e.path(nginxConfig), path); err != nil {
		t.Fatal(err)
	}
	if _, err := f.e.read(managedConfig, true); err == nil {
		t.Fatal("symlink accepted")
	}
}

func TestCompleteSetupCreatesAccountAfterConfirmationAndCanKeepAllSettings(t *testing.T) {
	f := setupFixture(t)
	c, output := fixtureConsole("y\n1\ny\n1\ny\n1\n\n1\n\n\n\n1\n")
	p, err := f.e.collect(context.Background(), f.config, c)
	if err != nil {
		t.Fatal(err)
	}
	if f.account.created || p.accountUser != "panel_monitor" || len(p.accountPassword) != 48 {
		t.Fatal("account created before confirmation or not securely generated")
	}
	if _, err = f.e.apply(context.Background(), p); err != nil {
		t.Fatal(err)
	}
	if !f.account.created || f.account.dropped {
		t.Fatal("account not kept after successful transaction")
	}
	if strings.Contains(output.String(), p.accountPassword) {
		t.Fatal("generated password displayed")
	}
	configData := mustRead(t, f.e.path(f.config))
	if !bytes.Contains(configData, []byte(p.accountPassword)) {
		t.Fatal("generated credentials not saved")
	}
	c, output = fixtureConsole("y\ny\ny\ny\ny\ny\n")
	again, err := f.e.collect(context.Background(), f.config, c)
	if err != nil {
		t.Fatal(err)
	}
	if len(again.changes) != 0 || again.account != nil {
		t.Fatal("kept settings were changed")
	}
	if strings.Contains(output.String(), p.accountPassword) {
		t.Fatal("existing password displayed")
	}
	f.log = nil
	if _, err = f.e.apply(context.Background(), again); err != nil {
		t.Fatal(err)
	}
	if len(f.log) != 1 || f.log[0] != "check nginx,php_fpm,database" {
		t.Fatal("unchanged services reloaded", f.log)
	}
	// Reconfigure only Nginx's listener. Existing PHP metric URLs must follow
	// that listener while database credentials and PHP worker config stay intact.
	c, _ = fixtureConsole("y\nn\n1\nn\nn\n18081\n")
	moved, err := f.e.collect(context.Background(), f.config, c)
	if err != nil {
		t.Fatal(err)
	}
	if moved.options.PHPFPMStatusURL != "http://127.0.0.1:18081/fpm-status" || moved.phpReload || !moved.selected["php_fpm"] {
		t.Fatal("port migration broke the other managed metric")
	}
	if moved.options.MySQL.Password != p.accountPassword {
		t.Fatal("unselected database credentials changed")
	}
}
func TestFPMEmptyStatusAndExistingStatusAreHandledWithoutDuplicateKeys(t *testing.T) {
	for _, status := range []string{"pm.status_path = \"\" ; disabled\n", "pm.status_path = /existing\n"} {
		f := setupFixture(t)
		writeFixture(t, f.e, fpmConfig, "[www]\nlisten=/tmp/php.sock\n"+status, 0644)
		pools, err := f.e.pools()
		if err != nil {
			t.Fatal(err)
		}
		updated := addFPMStatus(pools[0], "/fpm-status")
		if bytes.Count(updated, []byte("pm.status_path")) != 1 {
			t.Fatal("duplicate status key generated")
		}
		if pools[0].Status != "" && !bytes.Contains(updated, []byte("/existing")) {
			t.Fatal("existing status replaced")
		}
	}
}
func TestManagedURLPortChangesPreserveQueryAndIgnoreOtherServers(t *testing.T) {
	changed, ok := changeManagedPort("http://127.0.0.1:18080/fpm-status?json", 18080, 18081, "/fpm-status")
	if !ok || changed != "http://127.0.0.1:18081/fpm-status?json" {
		t.Fatal("query lost")
	}
	for _, raw := range []string{"http://127.0.0.1:8000/fpm-status", "http://127.0.0.2:18080/fpm-status", "http://127.0.0.1:18080/unrelated", "https://127.0.0.1:18080/fpm-status"} {
		if _, ok := changeManagedPort(raw, 18080, 18081, "/fpm-status"); ok {
			t.Fatal("unrelated URL changed")
		}
	}
}
func TestRecoveryDoesNotOverwriteConcurrentAdministratorChanges(t *testing.T) {
	f := setupFixture(t)
	p := collectNginx(t, f)
	f.e.userCheck = func(context.Context, string, []string) error {
		writeFixture(t, f.e, nginxConfig, "http { # administrator edit\n }", 0644)
		return errors.New("probe")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	_, err := f.e.apply(ctx, p)
	if err == nil || !strings.Contains(err.Error(), "并发修改文件") {
		t.Fatal("concurrent edit not reported", err)
	}
	if !bytes.Contains(mustRead(t, f.e.path(nginxConfig)), []byte("administrator edit")) {
		t.Fatal("administrator's edit overwritten by rollback")
	}
}

func TestInterruptedPromptAndBackupFailureLeaveServicesUntouched(t *testing.T) {
	f := setupFixture(t)
	before := originalFiles(t, f)
	reader, writer := io.Pipe()
	defer reader.Close()
	defer writer.Close()
	console := &Console{Input: reader, Output: io.Discard}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	if err := f.e.configure(ctx, f.config, console); err == nil {
		t.Fatal("prompt ignored cancellation")
	}
	assertRestored(t, f, before)
	p := collectNginx(t, f)
	original := f.e.write
	f.e.write = func(path string, data []byte, mode os.FileMode, gid int) error {
		if filepath.Base(path) == "manifest.json" {
			return errors.New("backup failed")
		}
		return original(path, data, mode, gid)
	}
	if _, err := f.e.apply(context.Background(), p); err == nil {
		t.Fatal("backup failure ignored")
	}
	assertRestored(t, f, before)
}
func TestInvalidOrMissingOwnedRecordIsNotOverwritten(t *testing.T) {
	for _, data := range []string{`null`, `{}`, `{"format":"unknown","port":18080}`, `{"format":"lnmp-gopanel-metrics-v1","port":18080,"hash":"invalid"}`} {
		f := setupFixture(t)
		path := "/etc/lnmp-gopanel/metrics-setup.json"
		writeFixture(t, f.e, path, data, 0600)
		if _, _, err := f.e.readState(f.config); err == nil {
			t.Fatal("invalid record accepted", data)
		}
		if string(mustRead(t, f.e.path(path))) != data {
			t.Fatal("invalid record changed")
		}
	}
}

func TestPanelHTTPSFailureRestoresPreviousFiles(t *testing.T) {
	f := setupFixture(t)
	before := originalFiles(t, f)
	p := collectNginx(t, f)
	checks := 0
	f.e.panelCheck = func(context.Context, string) error {
		checks++
		if checks == 1 {
			return errors.New("HTTPS not ready")
		}
		return nil
	}
	_, err := f.e.apply(context.Background(), p)
	if err == nil || checks != 2 {
		t.Fatal("panel HTTPS failure did not restore and check old service", err, checks)
	}
	assertRestored(t, f, before)
}
