//go:build linux

package control

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/azpanel/lnmp-gopanel/internal/backups"
)

func TestBackupVersionAndLatestAsset(t *testing.T) {
	for _, tc := range []struct{ name, out, want string }{
		{"gdrive", "gdrive 3.9.1\nCommit: 8551556\nRust: 1.67.0\nArch: x86_64\nOS: linux\n", "3.9.1"}, {"rclone", "rclone v1.73.2\n- os/version: linux", "1.73.2"},
		{"gdrive", "some unrelated 3.9.1", ""}, {"rclone", "", ""},
	} {
		v, err := backupVersion(tc.name, tc.out)
		if v != tc.want || (err != nil) != (tc.want == "") {
			t.Fatal(tc, v, err)
		}
	}
	url := "https://github.com/glotlabs/gdrive/releases/download/99.1.0/gdrive_linux-x64.tar.gz"
	data := []byte(`{"assets":[{"name":"gdrive_linux-x64.tar.gz","browser_download_url":"` + url + `"}]}`)
	if got, err := selectGdriveAsset(data); err != nil || got != url {
		t.Fatal(got, err)
	}
	for _, bad := range []string{`{}`, `{"draft":true}`, `{"prerelease":true}`, strings.ReplaceAll(string(data), "https://github.com/", "https://evil.example/"), `invalid`} {
		if _, err := selectGdriveAsset([]byte(bad)); err == nil {
			t.Fatal("accepted invalid release", bad)
		}
	}
}
func TestBackupDownloadFailures(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/error":
			w.WriteHeader(503)
		case "/slow":
			<-r.Context().Done()
		default:
			_, _ = io.WriteString(w, "12345")
		}
	}))
	defer server.Close()
	for _, tc := range []struct {
		path    string
		limit   int64
		wantErr bool
	}{
		{"/ok", 5, false}, {"/large", 4, true}, {"/error", 5, true},
	} {
		var out bytes.Buffer
		err := fetchBackup(context.Background(), server.Client(), server.URL+tc.path, &out, tc.limit)
		if (err != nil) != tc.wantErr {
			t.Fatal(tc, err)
		}
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	if err := fetchBackup(ctx, server.Client(), server.URL+"/slow", io.Discard, 10); err == nil {
		t.Fatal("timeout ignored")
	}
}
func TestGdriveExtractOnlyRegularBinary(t *testing.T) {
	for _, tc := range []struct {
		name  string
		kind  byte
		valid bool
	}{
		{"gdrive", tar.TypeReg, true}, {"./gdrive", tar.TypeReg, true},
		{"../gdrive", tar.TypeReg, false}, {"/gdrive", tar.TypeReg, false}, {"gdrive", tar.TypeSymlink, false},
	} {
		var archive bytes.Buffer
		gz := gzip.NewWriter(&archive)
		tw := tar.NewWriter(gz)
		h := &tar.Header{Name: tc.name, Typeflag: tc.kind, Mode: 0755}
		if tc.kind == tar.TypeReg {
			h.Size = 6
		} else {
			h.Linkname = "/etc/passwd"
		}
		if err := tw.WriteHeader(h); err != nil {
			t.Fatal(err)
		}
		if h.Size > 0 {
			_, _ = tw.Write([]byte("binary"))
		}
		_ = tw.Close()
		_ = gz.Close()
		var out bytes.Buffer
		err := extractGdrive(&archive, &out)
		if (err == nil) != tc.valid {
			t.Fatal(tc, err)
		}
		if tc.valid && out.String() != "binary" {
			t.Fatal(out.String())
		}
	}
}
func TestRcloneScriptExitAndCleanup(t *testing.T) {
	for _, code := range []string{"0", "3", "4"} {
		dir := t.TempDir()
		path := filepath.Join(dir, "install.sh")
		if err := os.WriteFile(path, []byte("#!/bin/bash\necho test-installer\nexit "+code+"\n"), 0600); err != nil {
			t.Fatal(err)
		}
		err := runRcloneScript(context.Background(), dir, path)
		if (err != nil) != (code == "4") {
			t.Fatal(code, err)
		}
	}
	dir := t.TempDir()
	if err := installBackupTool(context.Background(), "unknown", dir); err == nil {
		t.Fatal("invalid tool accepted")
	}
	entries, err := os.ReadDir(dir)
	if err != nil || len(entries) != 0 {
		t.Fatal("temporary files leaked", entries, err)
	}
}
func backupTestInstaller(t *testing.T) backupInstaller {
	t.Helper()
	if os.Geteuid() != 0 {
		t.Skip("root-owned state path required")
	}
	dir, err := os.MkdirTemp("/root", "gopanel-backups-test-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	p := defaultBackupInstaller()
	p.state = dir
	p.helper = "/bin/true"
	p.arch = "amd64"
	p.run = func(context.Context, string, ...string) (string, error) { return "ActiveState=active\n", nil }
	p.probe = func(_ context.Context, name, arch string) backups.Item {
		return backups.Item{Name: name, Status: "missing", Supported: arch == "amd64" || name == "rclone"}
	}
	p.install = func(context.Context, string, string) error { return nil }
	return p
}
func TestBackupJobBatchContinuesAndRecovers(t *testing.T) {
	p := backupTestInstaller(t)
	installed := map[string]bool{}
	p.probe = func(_ context.Context, name, arch string) backups.Item {
		item := backups.Item{Name: name, Status: "missing", Supported: true}
		if installed[name] {
			item.Status = "ready"
			item.Version = "9.1.0"
		}
		return item
	}
	var calls []string
	p.install = func(_ context.Context, name, state string) error {
		calls = append(calls, name)
		if name == "gdrive" {
			return errors.New("网络不可用")
		}
		installed[name] = true
		return nil
	}
	ctx := context.Background()
	job, err := p.start(ctx, backups.Request{Components: []string{"rclone", "gdrive"}})
	if err != nil {
		t.Fatal(err)
	}
	// Simulate a new helper process inspecting the persisted state.
	recovered, err := p.inspect(ctx)
	if err != nil || recovered.Job.ID != job.ID {
		t.Fatal(recovered, err)
	}
	if _, err = p.start(ctx, backups.Request{Components: []string{"rclone"}}); !errors.Is(err, backups.ErrBusy) {
		t.Fatal(err)
	}
	if err = p.work(ctx, job.ID); err != nil {
		t.Fatal(err)
	}
	snap, err := p.inspect(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(calls, []string{"gdrive", "rclone"}) || snap.Job.Status != "failed" || snap.Job.Items[0].Error != "网络不可用" || snap.Job.Items[1].Version != "9.1.0" || snap.Items[1].Status != "ready" {
		t.Fatal(snap, calls)
	}
	job, err = p.start(ctx, backups.Request{Components: []string{"rclone"}})
	if err != nil {
		t.Fatal(err)
	}
	if err = p.work(ctx, job.ID); err != nil {
		t.Fatal(err)
	}
	snap, _ = p.inspect(ctx)
	if snap.Job.Status != "succeeded" || snap.Job.Items[0].Status != "skipped" || len(calls) != 2 {
		t.Fatal(snap, calls)
	}
	if err = p.work(ctx, job.ID); err == nil {
		t.Fatal("completed worker restarted")
	}
}
func TestBackupInterruptedAndFailedLaunch(t *testing.T) {
	p := backupTestInstaller(t)
	ctx := context.Background()
	job, err := p.start(ctx, backups.Request{Components: []string{"gdrive"}})
	if err != nil {
		t.Fatal(err)
	}
	job.UpdatedAt = time.Now().Add(-time.Minute)
	data, _ := json.Marshal(job)
	if err = os.WriteFile(filepath.Join(p.state, "current.json"), data, 0600); err != nil {
		t.Fatal(err)
	}
	p.run = func(context.Context, string, ...string) (string, error) {
		return "ActiveState=inactive\nLoadState=not-found\n", errors.New("unit not found")
	}
	snap, err := p.inspect(ctx)
	if err != nil || snap.Job.Status != "failed" || snap.Job.Items[0].Status != "failed" {
		t.Fatal(snap, err)
	}
	p.run = func(context.Context, string, ...string) (string, error) { return "", errors.New("systemd unavailable") }
	if _, err = p.start(ctx, backups.Request{Components: []string{"gdrive"}}); err == nil {
		t.Fatal("launch failure ignored")
	}
	persisted, err := p.load()
	if err != nil || persisted.Status != "failed" {
		t.Fatal(persisted, err)
	}
	p.arch = "arm64"
	if _, err = p.start(ctx, backups.Request{Components: []string{"gdrive"}}); !errors.Is(err, backups.ErrInvalid) {
		t.Fatal(err)
	}
}
func TestBackupInstallRequiresPostInstallVersion(t *testing.T) {
	p := backupTestInstaller(t)
	ctx := context.Background()
	job, err := p.start(ctx, backups.Request{Components: []string{"gdrive"}})
	if err != nil {
		t.Fatal(err)
	}
	if err = p.work(ctx, job.ID); err != nil {
		t.Fatal(err)
	}
	snap, err := p.inspect(ctx)
	if err != nil || snap.Job.Status != "failed" || !strings.Contains(snap.Job.Items[0].Error, "有效版本") {
		t.Fatal(snap, err)
	}
}

type backupRoundTripper func(*http.Request) (*http.Response, error)

func (f backupRoundTripper) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }
func TestBackupDownloadErrorCleansTemporaryFiles(t *testing.T) {
	dir := t.TempDir()
	client := &http.Client{Transport: backupRoundTripper(func(r *http.Request) (*http.Response, error) {
		if r.URL.String() != rcloneScriptURL {
			t.Fatal(r.URL.String())
		}
		return nil, errors.New("simulated network failure")
	})}
	if err := installBackupToolWithClient(context.Background(), "rclone", dir, client); err == nil {
		t.Fatal("download error ignored")
	}
	entries, err := os.ReadDir(dir)
	if err != nil || len(entries) != 0 {
		t.Fatal("download files leaked", entries, err)
	}
}
func TestBackupConcurrentSubmissionsAndBoundedDiagnostics(t *testing.T) {
	p := backupTestInstaller(t)
	ctx := context.Background()
	var wg sync.WaitGroup
	results := make(chan error, 2)
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := p.start(ctx, backups.Request{Components: []string{"rclone"}})
			results <- err
		}()
	}
	wg.Wait()
	close(results)
	succeeded, busy := 0, 0
	for err := range results {
		if err == nil {
			succeeded++
		} else if errors.Is(err, backups.ErrBusy) {
			busy++
		} else {
			t.Fatal(err)
		}
	}
	if succeeded != 1 || busy != 1 {
		t.Fatal(succeeded, busy)
	}
	j, err := p.load()
	if err != nil {
		t.Fatal(err)
	}
	p.install = func(context.Context, string, string) error { return errors.New(strings.Repeat("\t失败", 10000)) }
	if err = p.work(ctx, j.ID); err != nil {
		t.Fatal(err)
	}
	snap, err := p.inspect(ctx)
	if err != nil {
		t.Fatal(err)
	}
	data, err := json.Marshal(snap)
	if err != nil || len(data) > 16384 || snap.Job.Status != "failed" {
		t.Fatal(len(data), snap, err)
	}
}

func TestBackupVersionUsesRealCLISubcommand(t *testing.T) {
	for _, name := range backups.Names() {
		dir := t.TempDir()
		path := filepath.Join(dir, name)
		output := "gdrive 3.9.1\nCommit: 8551556\nRust: 1.67.0\nArch: x86_64\nOS: linux"
		want := "3.9.1"
		if name == "rclone" {
			output = "rclone v1.75.2\n- os/type: linux"
			want = "1.75.2"
		}
		script := "#!/bin/sh\nif [ \"$#\" -ne 1 ] || [ \"$1\" != version ]; then echo 'unsupported argument' >&2; exit 2; fi\nprintf '%s\\n' '" + output + "'\n"
		if err := os.WriteFile(path, []byte(script), 0755); err != nil {
			t.Fatal(err)
		}
		got, err := readBackupVersion(context.Background(), name, path, componentCommand)
		if err != nil || got != want {
			t.Fatal(name, got, err)
		}
	}
	_, err := readBackupVersion(context.Background(), "gdrive", "/missing", func(context.Context, string, ...string) (string, error) {
		return "loader diagnostic", errors.New("executable failed")
	})
	if err == nil || !strings.Contains(err.Error(), "loader diagnostic") {
		t.Fatal("execution diagnostic lost", err)
	}
}

func TestRcloneTemporaryFilesStayInJobAndAreCleanedOnFailure(t *testing.T) {
	state := t.TempDir()
	// The upstream script creates a nested mktemp directory and only cleans it
	// on success. Reproduce the failure path without using host disk space.
	script := `#!/bin/bash
set -eu
[ "$TMPDIR" = "$PWD" ]
tmp_dir=$(mktemp -d)
case "$tmp_dir" in "$PWD"/*) ;; *) exit 9 ;; esac
printf 'partial archive' > "$tmp_dir/partial.zip"
echo 'simulated extraction failure' >&2
exit 4
`
	client := &http.Client{Transport: backupRoundTripper(func(r *http.Request) (*http.Response, error) {
		if r.URL.String() != rcloneScriptURL {
			t.Fatal(r.URL.String())
		}
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(script)), Header: make(http.Header)}, nil
	})}
	err := installBackupToolWithClient(context.Background(), "rclone", state, client)
	if err == nil || !strings.Contains(err.Error(), "simulated extraction failure") {
		t.Fatal(err)
	}
	entries, err := os.ReadDir(state)
	if err != nil || len(entries) != 0 {
		t.Fatal("failed installer leaked temporary files", entries, err)
	}
}
