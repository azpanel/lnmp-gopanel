//go:build linux

package control

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/azpanel/lnmp-gopanel/internal/lnmpinstall"
)

func TestLNMPInstallUmaskKeepsRuntimeAccessibleAndOutputsPrivate(t *testing.T) {
	dir := t.TempDir()
	logPath, sqlPath := filepath.Join(dir, "install.log"), filepath.Join(dir, "query.sql")
	for _, path := range []string{logPath, sqlPath} {
		if err := preparePrivateInstallFile(path); err != nil {
			t.Fatal(err)
		}
	}
	script := `#!/bin/bash
mkdir -p prefix/sbin
printf '#!/bin/sh\nexit 0\n' > prefix/sbin/daemon
chmod +x prefix/sbin/daemon
printf '%s\n' "$TEST_PASSWORD" >> "$TEST_INSTALL_LOG"
printf '%s\n' "$TEST_PASSWORD" > "$TEST_SQL_FILE"
`
	if err := os.WriteFile(filepath.Join(dir, "install.sh"), []byte(script), 0700); err != nil {
		t.Fatal(err)
	}
	env := []string{"TEST_INSTALL_LOG=" + logPath, "TEST_SQL_FILE=" + sqlPath, "TEST_PASSWORD=private-test-value"}
	// Set 0077 in the parent shell, exactly as systemd does for the worker.
	err := installerCommand(context.Background(), dir, "/bin/bash", env, func(string) {}, "-c", `umask 077; exec /bin/bash -c "$1"`, "test", lnmpInstallEntrypoint)
	if err != nil {
		t.Fatal(err)
	}
	for path, want := range map[string]os.FileMode{filepath.Join(dir, "prefix"): 0755, filepath.Join(dir, "prefix/sbin"): 0755, filepath.Join(dir, "prefix/sbin/daemon"): 0755, logPath: 0600, sqlPath: 0600} {
		info, err := os.Stat(path)
		if err != nil {
			t.Fatal(err)
		}
		if info.Mode().Perm() != want {
			t.Fatalf("%s mode=%o, want %o", path, info.Mode().Perm(), want)
		}
	}
}

func TestPrivateInstallFilesRejectLinksAndPreserveContents(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "target")
	if err := os.WriteFile(target, []byte("keep existing log"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := preparePrivateInstallFile(target); err != nil {
		t.Fatal(err)
	}
	if b, err := os.ReadFile(target); err != nil || string(b) != "keep existing log" {
		t.Fatal("existing output lost")
	}
	link := filepath.Join(dir, "symlink")
	if err := os.Symlink(target, link); err != nil {
		t.Fatal(err)
	}
	if err := preparePrivateInstallFile(link); err == nil {
		t.Fatal("symlink accepted")
	}
	hard := filepath.Join(dir, "hardlink")
	if err := os.Link(target, hard); err != nil {
		t.Fatal(err)
	}
	if err := preparePrivateInstallFile(hard); err == nil {
		t.Fatal("hardlink accepted")
	}
}

func TestInstallationStreamingAndRedaction(t *testing.T) {
	secret := "Secret-Pass#2026"
	var lines []string
	err := installerCommand(context.Background(), t.TempDir(), "/bin/bash", []string{"TEST_SECRET=" + secret}, func(s string) { lines = append(lines, sanitizeInstallLine(s, []string{secret})) }, "-c", `printf 'compile 1\rcompile 2\n'; printf '%s\n' "$TEST_SECRET" >&2; printf '\033[31mMySQL root password: %s\033[0m\n' "$TEST_SECRET"; exit 7`)
	if err == nil {
		t.Fatal("script failure ignored")
	}
	joined := strings.Join(lines, "\n")
	if strings.Contains(joined, secret) || !strings.Contains(joined, "compile 1") || !strings.Contains(joined, "compile 2") {
		t.Fatalf("unexpected output: %s", joined)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	started := time.Now()
	if installerCommand(ctx, t.TempDir(), "/bin/bash", nil, func(string) {}, "-c", "sleep 30") == nil || time.Since(started) > 3*time.Second {
		t.Fatal("script process group was not terminated")
	}
}
func TestInstallationOversizeLineCannotSplitSecret(t *testing.T) {
	var lines []string
	w := installOutput{emit: func(s string) { lines = append(lines, s) }}
	_, _ = w.Write([]byte(strings.Repeat("x", 64*1024-3) + "secret-value\nnext\n"))
	w.flush()
	if len(lines) != 2 || strings.Contains(strings.Join(lines, ""), "secret") || lines[1] != "next" {
		t.Fatalf("oversized line handling: %q", lines)
	}
}
func TestModuleConfig(t *testing.T) {
	original := "Download_Mirror='https://example.com'\nNginx_Modules_Options=''\nEnable_Ngx_FancyIndex='y'\n"
	for _, paths := range [][]string{{}, {"/root/lnmp/panel-modules/vts", "/root/lnmp/panel-modules/fancyindex"}} {
		out, err := moduleConfig(original, paths)
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(out, "Enable_Ngx_FancyIndex='n'") || !strings.Contains(out, "Download_Mirror='https://example.com'") {
			t.Fatal(out)
		}
		if strings.Count(out, "--add-module=") != len(paths) {
			t.Fatal("modules repeated or omitted")
		}
	}
	if _, err := moduleConfig("changed upstream", nil); err == nil {
		t.Fatal("unknown upstream config accepted")
	}
}

func TestMaxMindRPMRepositories(t *testing.T) {
	for _, c := range []struct{ release, repo string }{
		{"ID=rocky\nVERSION_ID=\"9.7\"", "--enablerepo=crb"},
		{"ID=almalinux\nVERSION_ID=\"8.10\"", "--enablerepo=powertools"},
		{"ID=centos\nVERSION_ID=\"10\"", "--enablerepo=crb"},
		{"ID=fedora\nVERSION_ID=\"43\"", ""},
	} {
		args := maxMindRPMArgs(c.release)
		if c.repo != "" && args[0] != c.repo {
			t.Fatal(args)
		}
		if c.repo == "" && args[0] != "install" {
			t.Fatal(args)
		}
		if !strings.Contains(strings.Join(args, " "), "libmaxminddb libmaxminddb-devel") {
			t.Fatal(args)
		}
	}
}

func TestInstallationRequiresCompletionAndSelectedComponents(t *testing.T) {
	r := lnmpinstall.Request{Database: 11}
	all := func(string) bool { return true }
	for _, flags := range [][3]bool{{false, false, true}, {true, true, true}, {true, false, false}} {
		if checkInstallResult(r, flags[0], flags[1], flags[2], all) == nil {
			t.Fatal("incomplete script result accepted")
		}
	}
	for _, missing := range []string{"/usr/local/php/etc/php.ini", "/usr/local/nginx/conf/nginx.conf", "/usr/local/mysql/bin/mysql", "/etc/my.cnf"} {
		if err := checkInstallResult(r, true, false, true, func(p string) bool { return p != missing }); err == nil {
			t.Fatalf("missing component accepted: %s", missing)
		}
	}
	r.Database = 12
	if err := checkInstallResult(r, true, false, true, func(p string) bool { return !strings.Contains(p, "/usr/local/mysql/") }); err != nil {
		t.Fatal(err)
	}
	r.Database = 0
	if err := checkInstallResult(r, true, false, true, func(p string) bool {
		return !strings.Contains(p, "mysql") && !strings.Contains(p, "mariadb") && !strings.HasSuffix(p, "my.cnf")
	}); err != nil {
		t.Fatal(err)
	}
}
func TestPersistentLogCursor(t *testing.T) {
	if os.Geteuid() != 0 {
		t.Skip("root-owned state trust check")
	}
	dir, err := os.MkdirTemp("/root", "lnmp-install-test-")
	if err != nil {
		t.Skip(err)
	}
	defer os.RemoveAll(dir)
	p := rootInstaller{state: dir}
	id := "11111111-1111-4111-8111-111111111111"
	if err = os.Mkdir(p.path(id, ""), 0700); err != nil {
		t.Fatal(err)
	}
	j := lnmpinstall.Job{ID: id, Status: "running"}
	if err = p.save(&j); err != nil {
		t.Fatal(err)
	}
	var data []byte
	for i := 0; i < 405; i++ {
		b, _ := json.Marshal(lnmpinstall.Line{Time: time.Now(), Text: "make source file"})
		data = append(data, append(b, '\n')...)
	}
	if err = os.WriteFile(p.path(id, "output.jsonl"), data, 0600); err != nil {
		t.Fatal(err)
	}
	var cursor int64
	count := 0
	for {
		page, e := p.read(lnmpinstall.ReadRequest{ID: id, After: cursor})
		if e != nil {
			t.Fatal(e)
		}
		for _, l := range page.Lines {
			if l.ID <= cursor {
				t.Fatal("non-monotonic cursor")
			}
			cursor = l.ID
			count++
		}
		if !page.More {
			break
		}
	}
	if count != 405 || cursor != int64(len(data)) {
		t.Fatalf("count=%d cursor=%d", count, cursor)
	}
	// Reopening the store preserves both metadata and byte cursors.
	reopened := rootInstaller{state: dir}
	if _, err = reopened.load(id); err != nil {
		t.Fatal(err)
	}
	if page, e := reopened.read(lnmpinstall.ReadRequest{ID: id, After: cursor}); e != nil || len(page.Lines) != 0 {
		t.Fatal("cursor replayed output")
	}
	for _, after := range []int64{-1, 1, int64(len(data) + 1)} {
		if _, err = p.read(lnmpinstall.ReadRequest{ID: id, After: after}); err == nil {
			t.Fatalf("invalid cursor accepted: %d", after)
		}
	}
	if err = os.Remove(p.path(id, "output.jsonl")); err != nil {
		t.Fatal(err)
	}
	if err = os.Symlink("/etc/shadow", p.path(id, "output.jsonl")); err != nil {
		t.Fatal(err)
	}
	// Even root-owned symlinks must not become arbitrary log read targets.
	if _, e := p.read(lnmpinstall.ReadRequest{ID: id}); e == nil {
		t.Fatal("symlink read allowed")
	}
	if _, e := p.read(lnmpinstall.ReadRequest{ID: id, Tail: true}); e == nil {
		t.Fatal("symlink tail read allowed")
	}
}

func TestInstallationLogTailAndResume(t *testing.T) {
	if os.Geteuid() != 0 {
		t.Skip("root-owned state trust check")
	}
	dir, err := os.MkdirTemp("/root", "lnmp-install-tail-test-")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(dir)
	p := rootInstaller{state: dir}
	id := "11111111-1111-4111-8111-111111111111"
	if err = os.Mkdir(p.path(id, ""), 0700); err != nil {
		t.Fatal(err)
	}
	if err = p.save(&lnmpinstall.Job{ID: id, Status: "running"}); err != nil {
		t.Fatal(err)
	}
	if page, err := p.read(lnmpinstall.ReadRequest{ID: id, Tail: true}); err != nil || len(page.Lines) != 0 || page.Cursor != 0 {
		t.Fatalf("missing log: %+v, %v", page, err)
	}
	for _, tc := range []struct {
		name    string
		count   int
		text    string
		partial bool
	}{
		{"empty", 0, "", false},
		{"single", 1, "first", false},
		{"under page", 199, "compile", false},
		{"full page", 200, "compile", false},
		{"over page", 201, "compile", false},
		{"many pages", 5000, "正在编译", false},
		{"byte limit", 40, strings.Repeat("x", 8000), false},
		{"large record", 3, strings.Repeat("\t", 65500), false},
		{"partial first", 0, "", true},
		{"partial last", 405, "compile", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var data []byte
			offsets := []int64{0}
			for i := 0; i < tc.count; i++ {
				b, err := json.Marshal(lnmpinstall.Line{Text: tc.text})
				if err != nil {
					t.Fatal(err)
				}
				data = append(data, append(b, '\n')...)
				offsets = append(offsets, int64(len(data)))
			}
			end := int64(len(data))
			if tc.partial {
				data = append(data, []byte(`{"text":"pending`)...)
			}
			path := p.path(id, "output.jsonl")
			if err := os.WriteFile(path, data, 0600); err != nil {
				t.Fatal(err)
			}
			page, err := p.read(lnmpinstall.ReadRequest{ID: id, Tail: true})
			if err != nil {
				t.Fatal(err)
			}
			start := max(0, tc.count-200)
			for start < tc.count-1 && end-offsets[start] > 128*1024 {
				start++
			}
			if len(page.Lines) != tc.count-start || page.Cursor != end || (page.More && !tc.partial) {
				t.Fatalf("lines=%d cursor=%d more=%v; want lines=%d cursor=%d", len(page.Lines), page.Cursor, page.More, tc.count-start, end)
			}
			for i, line := range page.Lines {
				if line.ID != offsets[start+i+1] || line.Text != tc.text {
					t.Fatalf("unexpected line %d: %+v", i, line)
				}
			}
			if page.More {
				next, err := p.read(lnmpinstall.ReadRequest{ID: id, After: page.Cursor})
				if err != nil || len(next.Lines) != 0 || next.More || next.Cursor != end {
					t.Fatalf("unfinished record: %+v, %v", next, err)
				}
			}
			// Resume from the returned byte cursor, including an unfinished record.
			if tc.partial {
				data = append(data, []byte("\"}\n")...)
			} else {
				data = append(data, []byte("{\"text\":\"pending\"}\n")...)
			}
			if err := os.WriteFile(path, data, 0600); err != nil {
				t.Fatal(err)
			}
			next, err := p.read(lnmpinstall.ReadRequest{ID: id, After: page.Cursor})
			if err != nil || len(next.Lines) != 1 || next.Lines[0].Text != "pending" || next.Cursor != int64(len(data)) {
				t.Fatalf("resume: %+v, %v", next, err)
			}
		})
	}
	if _, err := p.read(lnmpinstall.ReadRequest{ID: id, Tail: true, After: 1}); err == nil {
		t.Fatal("tail with cursor accepted")
	}
}

func TestInstallationStartTimePersistsAcrossUpdatesAndDuplicateRequests(t *testing.T) {
	if os.Geteuid() != 0 {
		t.Skip("root-owned state trust check")
	}
	dir, err := os.MkdirTemp("/root", "lnmp-install-time-test-")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(dir)
	p := rootInstaller{state: dir}
	id := "11111111-1111-4111-8111-111111111111"
	if err := os.Mkdir(p.path(id, ""), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "current"), []byte(id), 0600); err != nil {
		t.Fatal(err)
	}
	startedAt := time.Now().UTC().Add(-time.Minute).Truncate(time.Millisecond)
	r := lnmpinstall.Request{ID: id, Revision: strings.Repeat("a", 40), PHP: 14, Allocator: 1, Protection: "random", IPv4: "192.0.2.1", Modules: []string{}}
	j := lnmpinstall.Job{ID: id, Status: "running", Phase: "模块准备", CreatedAt: startedAt.Add(-time.Hour), StartedAt: &startedAt,
		Options: &lnmpinstall.Options{Revision: r.Revision, Databases: []lnmpinstall.Choice{{ID: 0, Name: "不安装数据库"}}, PHP: []lnmpinstall.Choice{{ID: 14, Name: "PHP 8.3.36"}}}}
	if err := p.save(&j); err != nil {
		t.Fatal(err)
	}
	for _, status := range []string{"running", "succeeded"} {
		j.Status, j.Phase = status, "Nginx 编译"
		if err := p.save(&j); err != nil {
			t.Fatal(err)
		}
		before, err := os.ReadFile(p.path(id, "job.json"))
		if err != nil {
			t.Fatal(err)
		}
		restored, err := (rootInstaller{state: dir}).start(context.Background(), r)
		if err != nil || restored.StartedAt == nil || !restored.StartedAt.Equal(startedAt) {
			t.Fatal(restored, err)
		}
		after, err := os.ReadFile(p.path(id, "job.json"))
		if err != nil || string(before) != string(after) {
			t.Fatal("duplicate request rewrote persisted task", err)
		}
	}
	j.Status, j.StartedAt = "ready", nil
	if err := p.save(&j); err != nil {
		t.Fatal(err)
	}
	r.PHP = 99
	if _, err := p.start(context.Background(), r); err == nil {
		t.Fatal("invalid installation request accepted")
	}
	restored, err := p.load(id)
	if err != nil || restored.StartedAt != nil || restored.Status != "ready" {
		t.Fatal("rejected request started the timer", restored, err)
	}
}
