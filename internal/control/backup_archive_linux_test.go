//go:build linux

package control

import (
	"archive/zip"
	"context"
	"encoding/json"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/azpanel/lnmp-gopanel/internal/backups"
	"golang.org/x/sys/unix"
)

func backupArchiveTestTool(t *testing.T) string {
	t.Helper()
	tool := os.Getenv("GOPANEL_TEST_7ZIP")
	if tool == "" {
		tool = backupTool("7zip")
	}
	if tool == "" {
		t.Skip("install 7-Zip or set GOPANEL_TEST_7ZIP to run archive integrations")
	}
	if TrustedPath(tool) != nil {
		t.Fatal("test archive tool must be root-managed")
	}
	return tool
}
func TestBackupArchiveStagingExclusionsMetadataAndSafeReads(t *testing.T) {
	e, _, _ := backupExecutionFixture(t)
	site := filepath.Join(e.store.state, "site")
	stage := filepath.Join(e.store.state, "work", "stage")
	for _, dir := range []string{site, stage, filepath.Join(site, "cache"), filepath.Join(site, "nested/node_modules"), filepath.Join(site, "empty")} {
		if err := os.MkdirAll(dir, 0700); err != nil {
			t.Fatal(err)
		}
	}
	for name, content := range map[string]string{"index.php": "included", "cache/data": "excluded", "nested/node_modules/lib.js": "excluded", "nested/keep.txt": "keep", "dash - file\nname": "special"} {
		if err := os.WriteFile(filepath.Join(site, name), []byte(content), 0640); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.Symlink("/etc/passwd", filepath.Join(site, "outside")); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("nested", filepath.Join(site, "linked")); err != nil {
		t.Fatal(err)
	}
	if err := unix.Mkfifo(filepath.Join(site, "fifo"), 0600); err != nil {
		t.Fatal(err)
	}
	modified := time.Unix(1700000000, 0)
	if err := os.Chtimes(filepath.Join(site, "index.php"), modified, modified); err != nil {
		t.Fatal(err)
	}
	m := backupManifest{}
	var samples []backupProgressSample
	if err := e.stageBackupFiles(progressContext(&samples), []backups.FileSource{{Path: site, Excludes: []string{"cache/", "**/node_modules/**"}}}, stage, &m); err != nil {
		t.Fatal(err)
	}
	previous := int64(0)
	for i, sample := range samples {
		if sample.processed < previous || (i < len(samples)-1 && (sample.total != -1 || sample.percent != -1)) {
			t.Fatalf("staging progress regressed or fabricated a total: %+v", samples)
		}
		previous = sample.processed
	}
	if len(samples) < 4 || samples[len(samples)-1].total != int64(len("included"+"keep"+"special")) || previous != samples[len(samples)-1].total {
		t.Fatalf("staging did not count included files: %+v", samples)
	}
	for _, name := range []string{"source-001/index.php", "source-001/nested/keep.txt", "source-001/empty", "source-001/dash - file\nname"} {
		if _, err := os.Stat(filepath.Join(stage, name)); err != nil {
			t.Fatal(name, err)
		}
	}
	for _, name := range []string{"source-001/cache", "source-001/nested/node_modules/lib.js", "source-001/outside", "source-001/linked", "source-001/fifo"} {
		if _, err := os.Lstat(filepath.Join(stage, name)); !os.IsNotExist(err) {
			t.Fatal("excluded or unsafe path copied", name, err)
		}
	}
	info, err := os.Stat(filepath.Join(stage, "source-001/index.php"))
	if err != nil || info.Mode().Perm() != 0640 || !info.ModTime().Equal(modified) {
		t.Fatal(info, err)
	}
	if len(m.Sources) != 1 || m.Sources[0].Original != site {
		t.Fatal(m)
	}
	base, err := os.Open(site)
	if err != nil {
		t.Fatal(err)
	}
	defer base.Close()
	for _, name := range []string{"../escape", "linked/keep.txt", "outside"} {
		if f, err := openBackupRelative(base, name); err == nil {
			f.Close()
			t.Fatal("unsafe read allowed", name)
		}
	}
}

func TestBackupArchiveAllFormatsLevelsAndPasswordRoundTrip(t *testing.T) {
	tool := backupArchiveTestTool(t)
	ctx := context.Background()
	root := t.TempDir()
	stage := filepath.Join(root, "stage")
	if err := os.MkdirAll(filepath.Join(stage, "empty"), 0700); err != nil {
		t.Fatal(err)
	}
	content := strings.Repeat("backup 数据 \n", 300)
	if err := os.WriteFile(filepath.Join(stage, "data.txt"), []byte(content), 0640); err != nil {
		t.Fatal(err)
	}
	for _, format := range []string{"zip", "7z"} {
		levels := []int{0, 1, 2, 3, 4, 5, 6, 7, 8, 9}
		if format == "7z" {
			levels = []int{0, 1, 3, 5, 7, 9}
		}
		for _, level := range levels {
			for _, encrypted := range []bool{false, true} {
				t.Run(fmt.Sprintf("%s/%d/encrypted=%t", format, level, encrypted), func(t *testing.T) {
					password := []byte{}
					if encrypted {
						password = []byte("backup-$(literal)-\"secret")
						if format == "7z" {
							password = []byte("备份-$(literal)-\"secret")
						}
					}
					archive := filepath.Join(root, fmt.Sprintf("archive-%s-%d-%t.%s", format, level, encrypted, format))
					if err := compressBackupArchive(ctx, tool, stage, archive, backups.Compression{Format: format, Level: level}, password); err != nil {
						t.Fatal(err)
					}
					if encrypted {
						if err := runBackupArchiveCommand(ctx, tool, stage, []string{"t", "--", archive}, []byte("wrong-password")); err == nil {
							t.Fatal("wrong password accepted")
						}
					}
					extracted := filepath.Join(root, fmt.Sprintf("extract-%s-%d-%t", format, level, encrypted))
					if err := os.Mkdir(extracted, 0700); err != nil {
						t.Fatal(err)
					}
					args := []string{"x", "-y", "-bd"}
					args = append(args, "--", archive)
					if err := runBackupArchiveCommand(ctx, tool, extracted, args, password); err != nil {
						t.Fatal(err)
					}
					data, err := os.ReadFile(filepath.Join(extracted, "data.txt"))
					if err != nil || string(data) != content {
						t.Fatal("round trip failed", err)
					}
					if _, err = os.Stat(filepath.Join(extracted, "empty")); err != nil {
						t.Fatal("empty directory lost", err)
					}
					if format == "zip" {
						z, err := zip.OpenReader(archive)
						if err != nil {
							t.Fatal(err)
						}
						defer z.Close()
						for _, entry := range z.File {
							if entry.Name == "data.txt" && encrypted && (entry.Flags&1 == 0 || entry.Method != 99) {
								t.Fatal("ZIP is not AES encrypted", entry.FileHeader)
							}
						}
					}
				})
			}
		}
	}
}

func TestBackupFileExecutionPublishesCompleteArchiveAndHandlesPartialDelivery(t *testing.T) {
	tool := backupArchiveTestTool(t)
	e, _, _ := backupExecutionFixture(t)
	e.tools = func(name string) string {
		if name == "7zip" {
			return tool
		}
		return ""
	}
	site := filepath.Join(e.store.state, "site")
	if err := os.Mkdir(site, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(site, "file.txt"), []byte("backup-content"), 0640); err != nil {
		t.Fatal(err)
	}
	listener, err := net.Listen("unix", filepath.Join(site, "app.sock"))
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	r := sampleBackupPolicy()
	r.Databases.Enabled = false
	r.Files = []backups.FileSource{{Path: site}}
	r.Destinations = []backups.Destination{{Type: "local", Path: filepath.Join(e.store.state, "output")}}
	r.PasswordAction = "replace"
	r.Password = "never-log-this-password"
	p, err := e.store.save(r)
	if err != nil {
		t.Fatal(err)
	}
	j, err := e.start(context.Background(), backups.PolicyIdentity{ID: p.ID, Revision: p.Revision})
	if err != nil {
		t.Fatal(err)
	}
	stored, err := e.loadRun(j.ID)
	if err != nil {
		t.Fatal(err)
	}
	// Simulate a remote becoming unavailable after configuration was saved.
	stored.Snapshot.Destinations = append(stored.Snapshot.Destinations, backups.Destination{Type: "gdrive", Account: "gone", FolderID: "root"})
	stored.Steps = backupSteps(*stored.Snapshot, stored.CreatedAt)
	if err = e.update(&stored); err != nil {
		t.Fatal(err)
	}
	if err = e.worker(context.Background(), j.ID); err != nil {
		t.Fatal(err)
	}
	stored, err = e.loadRun(j.ID)
	if err != nil || stored.Status != "partial" || len(stored.Categories) != 1 {
		t.Fatal(stored, err)
	}
	for _, id := range []string{"files-prepare", "files-compress", "files-verify", "files-deliver-0"} {
		step := backupStep(&stored, id)
		if step == nil || step.Status != "succeeded" || step.Progress == nil || *step.Progress.Percent != 100 {
			t.Fatal("completed progress missing", id, step)
		}
	}
	if backupStep(&stored, "files-deliver-1").Status != "failed" || backupStep(&stored, "files-cleanup-1").Status != "skipped" {
		t.Fatal("failed delivery timeline incorrect")
	}
	category := stored.Categories[0]
	if category.Status != "partial" || category.Deliveries[0].Status != "succeeded" || category.Deliveries[1].Status != "failed" {
		t.Fatal(category)
	}
	if _, err = os.Stat(e.workPath(j.ID)); !os.IsNotExist(err) {
		t.Fatal("workspace leaked", err)
	}
	output := t.TempDir()
	args := []string{"x", "--", category.Deliveries[0].Object}
	if err = runBackupArchiveCommand(context.Background(), tool, output, args, []byte(r.Password)); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(output, "source-001/file.txt"))
	if err != nil || string(data) != "backup-content" {
		t.Fatal(string(data), err)
	}
	var manifest backupManifest
	data, err = os.ReadFile(filepath.Join(output, "manifest.json"))
	if err != nil || json.Unmarshal(data, &manifest) != nil || manifest.PolicyID != p.ID {
		t.Fatal(string(data), err)
	}
	if manifest.Version != 2 || manifest.Metadata != "metadata.jsonl" {
		t.Fatal(manifest)
	}
	if records := readBackupMetadataTest(t, output); len(records) != 2 {
		t.Fatal(records)
	}
	if _, err := os.Lstat(filepath.Join(output, "source-001/app.sock")); !os.IsNotExist(err) {
		t.Fatal("socket archived", err)
	}
	public, _ := json.Marshal(stored.Run)
	disk, _ := os.ReadFile(e.runPath(j.ID))
	if strings.Contains(string(public), r.Password) || strings.Contains(string(disk), r.Password) || strings.Contains(string(disk), "snapshot") {
		t.Fatal("execution credentials leaked")
	}
	// A database failure must still allow a complete file archive to be delivered.
	r.Policy = p
	r.PasswordAction = "keep"
	r.Password = ""
	r.Databases.Enabled = true
	p, err = e.store.save(r)
	if err != nil {
		t.Fatal(err)
	}
	j, err = e.start(context.Background(), backups.PolicyIdentity{ID: p.ID, Revision: p.Revision})
	if err != nil {
		t.Fatal(err)
	}
	if err = e.worker(context.Background(), j.ID); err != nil {
		t.Fatal(err)
	}
	stored, err = e.loadRun(j.ID)
	if err != nil || stored.Status != "partial" || len(stored.Categories) != 2 || stored.Categories[0].Status != "failed" || stored.Categories[1].Status != "succeeded" {
		t.Fatal("database failure prevented file backup", stored, err)
	}
}
