//go:build linux

package control

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"syscall"
	"testing"
	"time"

	"github.com/azpanel/lnmp-gopanel/internal/backups"
	"golang.org/x/sys/unix"
)

func readBackupMetadataTest(t *testing.T, stage string) []backupFileMetadata {
	t.Helper()
	f, err := os.Open(filepath.Join(stage, "metadata.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	decoder := json.NewDecoder(f)
	var records []backupFileMetadata
	for {
		var record backupFileMetadata
		err := decoder.Decode(&record)
		if err == io.EOF {
			return records
		}
		if err != nil {
			t.Fatal(err)
		}
		records = append(records, record)
	}
}

func TestBackupMetadataArchiveRestoreRoundTrip(t *testing.T) {
	if os.Geteuid() != 0 {
		t.Skip("root required for ownership round trip")
	}
	tool := backupArchiveTestTool(t)
	e, _, _ := backupExecutionFixture(t)
	site := filepath.Join(e.store.state, "site")
	stage := filepath.Join(e.store.state, "work", "stage")
	for _, path := range []string{site, stage, filepath.Join(site, "empty")} {
		if err := os.MkdirAll(path, 0700); err != nil {
			t.Fatal(err)
		}
	}
	name := "dash - file\nname"
	file := filepath.Join(site, name)
	if err := os.WriteFile(file, []byte("preserved content"), 0600); err != nil {
		t.Fatal(err)
	}
	modified := time.Unix(1700000000, 123456789)
	modes := map[string]os.FileMode{site: 0750 | os.ModeSetgid, filepath.Join(site, "empty"): 0751 | os.ModeSticky, file: 0640 | os.ModeSetuid | os.ModeSetgid}
	for path, mode := range modes {
		if err := os.Chown(path, 65534, 65533); err != nil {
			t.Fatal(err)
		}
		if err := os.Chmod(path, mode); err != nil {
			t.Fatal(err)
		}
		if err := os.Chtimes(path, modified, modified); err != nil {
			t.Fatal(err)
		}
	}
	manifest := backupManifest{Version: 1}
	if err := e.stageBackupFiles(context.Background(), []backups.FileSource{{Path: site}, {Path: file}}, stage, &manifest); err != nil {
		t.Fatal(err)
	}
	if manifest.Version != 2 || manifest.Metadata != "metadata.jsonl" {
		t.Fatal(manifest)
	}
	if err := writeBackupJSON(filepath.Join(stage, "manifest.json"), manifest); err != nil {
		t.Fatal(err)
	}
	records := readBackupMetadataTest(t, stage)
	if len(records) != 4 {
		t.Fatal(records)
	}
	expected := map[string]string{"source-001": site, "source-001/empty": filepath.Join(site, "empty"), "source-001/" + name: file, "source-002/" + name: file}
	for _, record := range records {
		source, ok := expected[record.ArchivePath]
		if !ok {
			t.Fatal(record)
		}
		info, err := os.Stat(source)
		if err != nil {
			t.Fatal(err)
		}
		stat := info.Sys().(*syscall.Stat_t)
		if record.UID != stat.Uid || record.GID != stat.Gid || record.Mode != stat.Mode&07777 || !record.ModifiedAt.Equal(info.ModTime()) {
			t.Fatal(record, stat)
		}
		staged, err := os.Stat(filepath.Join(stage, record.ArchivePath))
		if err != nil {
			t.Fatal(err)
		}
		st := staged.Sys().(*syscall.Stat_t)
		if st.Uid != 0 || st.Gid != uint32(os.Getegid()) || st.Mode&07000 != 0 {
			t.Fatal("staging must remain root-owned without special bits", st)
		}
	}
	for _, format := range []string{"zip", "7z"} {
		for _, password := range []string{"", "backup-secret"} {
			t.Run(format+"/encrypted="+strconv.FormatBool(password != ""), func(t *testing.T) {
				archive := filepath.Join(t.TempDir(), "archive."+format)
				if err := compressBackupArchive(context.Background(), tool, stage, archive, backups.Compression{Format: format, Level: 1}, []byte(password)); err != nil {
					t.Fatal(err)
				}
				output := t.TempDir()
				if err := runBackupArchiveCommand(context.Background(), tool, output, []string{"x", "-y", "--", archive}, []byte(password)); err != nil {
					t.Fatal(err)
				}
				recovered := readBackupMetadataTest(t, output)
				if len(recovered) != len(records) {
					t.Fatal(recovered)
				}
				// Restore files before parent directories, chown before chmod, timestamps last.
				for n := len(recovered) - 1; n >= 0; n-- {
					record := recovered[n]
					path := filepath.Join(output, record.ArchivePath)
					if err := os.Chown(path, int(record.UID), int(record.GID)); err != nil {
						t.Fatal(err)
					}
					if err := unix.Chmod(path, record.Mode); err != nil {
						t.Fatal(err)
					}
					if err := os.Chtimes(path, record.ModifiedAt, record.ModifiedAt); err != nil {
						t.Fatal(err)
					}
				}
				for _, record := range recovered {
					path := filepath.Join(output, record.ArchivePath)
					info, err := os.Stat(path)
					if err != nil {
						t.Fatal(err)
					}
					stat := info.Sys().(*syscall.Stat_t)
					source, err := os.Stat(expected[record.ArchivePath])
					if err != nil {
						t.Fatal(err)
					}
					original := source.Sys().(*syscall.Stat_t)
					if stat.Uid != original.Uid || stat.Gid != original.Gid || stat.Mode&07777 != original.Mode&07777 || !info.ModTime().Equal(source.ModTime()) || info.IsDir() != (record.Type == "directory") {
						t.Fatal(record, stat, original)
					}
					if record.Type == "file" {
						data, err := os.ReadFile(path)
						if err != nil || string(data) != "preserved content" {
							t.Fatal(string(data), err)
						}
					}
				}
			})
		}
	}
}

func TestBackupSpecialFilesSkippedAndReplacementRejected(t *testing.T) {
	e, _, _ := backupExecutionFixture(t)
	site := filepath.Join(e.store.state, "site")
	stage := filepath.Join(e.store.state, "work", "stage")
	for _, path := range []string{site, stage} {
		if err := os.MkdirAll(path, 0700); err != nil {
			t.Fatal(err)
		}
	}
	listener, err := net.Listen("unix", filepath.Join(site, "app.sock"))
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	if err := unix.Mkfifo(filepath.Join(site, "fifo"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(site, "data"), []byte("data"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("data", filepath.Join(site, "link")); err != nil {
		t.Fatal(err)
	}
	if err := e.stageBackupFiles(context.Background(), []backups.FileSource{{Path: site}}, stage, &backupManifest{}); err != nil {
		t.Fatal(err)
	}
	records := readBackupMetadataTest(t, stage)
	if len(records) != 2 {
		t.Fatal(records)
	}
	for _, name := range []string{"app.sock", "fifo", "link"} {
		if _, err := os.Lstat(filepath.Join(stage, "source-001", name)); !os.IsNotExist(err) {
			t.Fatal(name, err)
		}
	}
	if data, err := os.ReadFile(filepath.Join(stage, "source-001/data")); err != nil || string(data) != "data" {
		t.Fatal(string(data), err)
	}
	for _, kind := range []string{"socket", "fifo", "symlink", "file", "directory"} {
		t.Run(kind, func(t *testing.T) {
			// Unix socket paths have a small fixed limit; use the short fixture
			// root instead of the test name or a potentially long TMPDIR.
			dir := filepath.Join(e.store.state, "replacement-"+kind)
			if err := os.Mkdir(dir, 0700); err != nil {
				t.Fatal(err)
			}
			path := filepath.Join(dir, "entry")
			if err := os.WriteFile(path, []byte("old"), 0600); err != nil {
				t.Fatal(err)
			}
			original, err := os.Open(path)
			if err != nil {
				t.Fatal(err)
			}
			defer original.Close()
			expected, err := original.Stat()
			if err != nil {
				t.Fatal(err)
			}
			if err := os.Remove(path); err != nil {
				t.Fatal(err)
			}
			switch kind {
			case "socket":
				listener, err := net.Listen("unix", path)
				if err != nil {
					t.Fatal(err)
				}
				defer listener.Close()
			case "fifo":
				err = unix.Mkfifo(path, 0600)
			case "symlink":
				err = os.Symlink("missing", path)
			case "file":
				err = os.WriteFile(path, []byte("new"), 0600)
			case "directory":
				err = os.Mkdir(path, 0700)
			}
			if err != nil {
				t.Fatal(err)
			}
			base, err := os.Open(dir)
			if err != nil {
				t.Fatal(err)
			}
			defer base.Close()
			opened, _, err := openBackupEntry(base, "entry", expected)
			if err == nil {
				opened.Close()
				t.Fatal("replaced source accepted")
			}
		})
	}
}

type backupMetadataFailWriter struct{}

func (backupMetadataFailWriter) Write([]byte) (int, error) { return 0, io.ErrClosedPipe }

func TestBackupMetadataFailuresDoNotPublish(t *testing.T) {
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
	// A filename JSON cannot represent losslessly must fail rather than publish a
	// metadata mapping with replacement characters and no usable restore path.
	name := filepath.Join(site, string([]byte{0xff}))
	if err := os.WriteFile(name, []byte("data"), 0600); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(name)
	if err != nil {
		t.Fatal(err)
	}
	if err := writeBackupFileMetadata(json.NewEncoder(backupMetadataFailWriter{}), "valid", info); !errors.Is(err, io.ErrClosedPipe) {
		t.Fatal(err)
	}
	source, err := os.Open(name)
	if err != nil {
		t.Fatal(err)
	}
	defer source.Close()
	if err := copyBackupFile(context.Background(), source, filepath.Join(t.TempDir(), "copy"), func(info os.FileInfo) error {
		return writeBackupFileMetadata(json.NewEncoder(backupMetadataFailWriter{}), "valid", info)
	}); !errors.Is(err, io.ErrClosedPipe) {
		t.Fatal(err)
	}
	r := sampleBackupPolicy()
	r.Databases.Enabled = false
	r.Files = []backups.FileSource{{Path: site}}
	output := filepath.Join(e.store.state, "output")
	r.Destinations = []backups.Destination{{Type: "local", Path: output}}
	p, err := e.store.save(r)
	if err != nil {
		t.Fatal(err)
	}
	j, err := e.start(context.Background(), backups.PolicyIdentity{ID: p.ID, Revision: p.Revision})
	if err != nil {
		t.Fatal(err)
	}
	if err := e.worker(context.Background(), j.ID); err != nil {
		t.Fatal(err)
	}
	stored, err := e.loadRun(j.ID)
	if err != nil {
		t.Fatal(err)
	}
	if stored.Status != "failed" || len(stored.Categories) != 1 || stored.Categories[0].Status != "failed" || len(stored.Categories[0].Deliveries) != 0 {
		t.Fatal(stored)
	}
	if _, err := os.Stat(output); !os.IsNotExist(err) {
		t.Fatal("failed metadata published an archive", err)
	}
	if _, err := os.Stat(e.workPath(j.ID)); !os.IsNotExist(err) {
		t.Fatal("workspace retained", err)
	}
}

func TestBackupMetadataDetectsSourceChanges(t *testing.T) {
	for _, change := range []string{"permissions", "ownership", "content"} {
		t.Run(change, func(t *testing.T) {
			if change == "ownership" && os.Geteuid() != 0 {
				t.Skip("root required")
			}
			path := filepath.Join(t.TempDir(), "source")
			if err := os.WriteFile(path, []byte("old"), 0600); err != nil {
				t.Fatal(err)
			}
			f, err := os.Open(path)
			if err != nil {
				t.Fatal(err)
			}
			defer f.Close()
			before, err := f.Stat()
			if err != nil {
				t.Fatal(err)
			}
			// Filesystems can coalesce ctime updates within one clock tick.
			// Separate the mutation so this exercises a distinct ctime value.
			time.Sleep(20 * time.Millisecond)
			switch change {
			case "permissions":
				err = os.Chmod(path, 0644)
			case "ownership":
				err = os.Chown(path, 65534, 65533)
			case "content":
				err = os.WriteFile(path, []byte("new"), 0600)
			}
			if err != nil {
				t.Fatal(err)
			}
			if err := os.Chtimes(path, before.ModTime(), before.ModTime()); err != nil {
				t.Fatal(err)
			}
			after, err := f.Stat()
			if err != nil {
				t.Fatal(err)
			}
			if sameBackupFileMetadata(before, after) {
				t.Fatal("source change not detected")
			}
		})
	}
}
