//go:build linux

package control

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/azpanel/lnmp-gopanel/internal/backups"
)

func TestBackupLocalDeliveryRetentionAndUnrelatedFiles(t *testing.T) {
	e, now, _ := backupExecutionFixture(t)
	ctx := context.Background()
	destination := backups.Destination{Type: "local", Path: filepath.Join(e.store.state, "output")}
	policyID := strings.Repeat("a", 32)
	artifacts := []backupArtifact{}
	for n := 1; n <= 3; n++ {
		id := fmt.Sprintf("%032x", n)
		name := policyID + "-files-20261102T020000Z-" + id + ".zip"
		archive := filepath.Join(e.store.state, "archive.zip")
		if err := os.WriteFile(archive, []byte(fmt.Sprintf("archive-%d", n)), 0600); err != nil {
			t.Fatal(err)
		}
		object, err := e.deliver(ctx, id, destination, archive, name)
		if err != nil {
			t.Fatal(err)
		}
		size, digest, err := backupFileDigest(ctx, archive)
		if err != nil {
			t.Fatal(err)
		}
		*now = now.Add(time.Minute)
		a := backupArtifact{PolicyID: policyID, RunID: id, Category: "files", Destination: destination, Object: object, Name: name, Bytes: size, SHA256: digest, CreatedAt: *now}
		artifacts = append(artifacts, a)
		if err = e.recordArtifact(a); err != nil {
			t.Fatal(err)
		}
		if n == 1 {
			if err = os.WriteFile(filepath.Join(destination.Path, "unrelated.zip"), []byte("keep"), 0600); err != nil {
				t.Fatal(err)
			}
		}
		if err = e.retainArtifacts(ctx, id, a, 2); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := os.Stat(artifacts[0].Object); !os.IsNotExist(err) {
		t.Fatal("old artifact retained", err)
	}
	for _, name := range []string{artifacts[1].Object, artifacts[2].Object, filepath.Join(destination.Path, "unrelated.zip")} {
		if _, err := os.Stat(name); err != nil {
			t.Fatal("unexpected deletion", name, err)
		}
	}
	if _, err := e.deliver(ctx, artifacts[2].RunID, destination, filepath.Join(e.store.state, "archive.zip"), artifacts[2].Name); err == nil {
		t.Fatal("published over existing archive")
	}
	if err := os.WriteFile(artifacts[1].Object, []byte("modified!"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := e.retainArtifacts(ctx, artifacts[2].RunID, artifacts[2], 1); err == nil {
		t.Fatal("deleted externally modified archive")
	}
	if _, err := os.Stat(artifacts[1].Object); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(artifacts[1].Object); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(destination.Path, "unrelated.zip"), artifacts[1].Object); err != nil {
		t.Fatal(err)
	}
	if err := e.retainArtifacts(ctx, artifacts[2].RunID, artifacts[2], 1); err == nil {
		t.Fatal("followed modified symlink")
	}
}

func TestBackupGdriveIsolatesAccountSelection(t *testing.T) {
	e, _, _ := backupExecutionFixture(t)
	base := filepath.Join(e.accountHome(), ".config/gdrive3")
	for _, account := range []string{"first@example.com", "second@example.com"} {
		if err := os.MkdirAll(filepath.Join(base, account), 0700); err != nil {
			t.Fatal(err)
		}
		for _, file := range []string{"tokens.json", "secret.json"} {
			if err := os.WriteFile(filepath.Join(base, account, file), []byte(`{"private":"secret"}`), 0600); err != nil {
				t.Fatal(err)
			}
		}
	}
	if err := os.WriteFile(filepath.Join(base, "account.json"), []byte(`{"current":"first@example.com"}`), 0600); err != nil {
		t.Fatal(err)
	}
	if err := privateBackupDirectory(filepath.Join(e.store.state, "work")); err != nil {
		t.Fatal(err)
	}
	env, cleanup, err := e.gdriveEnvironment(strings.Repeat("b", 32), "second@example.com")
	if err != nil {
		t.Fatal(err)
	}
	defer cleanup()
	var home string
	for _, entry := range env {
		if strings.HasPrefix(entry, "HOME=") {
			home = strings.TrimPrefix(entry, "HOME=")
		}
	}
	if home == "" || home == e.accountHome() {
		t.Fatal("global account directory reused", env)
	}
	data, err := os.ReadFile(filepath.Join(home, ".config/gdrive3/account.json"))
	if err != nil || !strings.Contains(string(data), "second@example.com") {
		t.Fatal(string(data), err)
	}
	data, _ = os.ReadFile(filepath.Join(base, "account.json"))
	if !strings.Contains(string(data), "first@example.com") {
		t.Fatal("global selection changed")
	}
	if err = os.WriteFile(filepath.Join(home, ".config/gdrive3/second@example.com/tokens.json"), []byte("refreshed"), 0600); err != nil {
		t.Fatal(err)
	}
	data, _ = os.ReadFile(filepath.Join(base, "second@example.com/tokens.json"))
	if string(data) != "refreshed" {
		t.Fatal("token refresh lost")
	}
	if _, _, err = e.gdriveEnvironment(strings.Repeat("b", 32), "../escape"); err == nil {
		t.Fatal("invalid account accepted")
	}
}

func TestBackupLocalRetentionPreservesRacedReplacement(t *testing.T) {
	e, _, _ := backupExecutionFixture(t)
	dir := filepath.Join(e.store.state, "destination")
	if err := os.Mkdir(dir, 0700); err != nil {
		t.Fatal(err)
	}
	name := "backup.zip"
	full := filepath.Join(dir, name)
	if err := os.WriteFile(full, []byte("original"), 0600); err != nil {
		t.Fatal(err)
	}
	expected, err := os.Stat(full)
	if err != nil {
		t.Fatal(err)
	}
	if err = os.Rename(full, full+".saved"); err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(full, []byte("administrator replacement"), 0600); err != nil {
		t.Fatal(err)
	}
	root, err := os.OpenRoot(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()
	if err = removeVerifiedLocalBackup(root, name, expected); err == nil {
		t.Fatal("raced inode was deleted")
	}
	data, err := os.ReadFile(full)
	if err != nil || string(data) != "administrator replacement" {
		t.Fatal("replacement lost", string(data), err)
	}
	data, err = os.ReadFile(full + ".saved")
	if err != nil || string(data) != "original" {
		t.Fatal("original lost", string(data), err)
	}
}
