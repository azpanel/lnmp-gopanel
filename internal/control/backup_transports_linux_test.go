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

func TestBackupRcloneLocalTransportAndExactRetention(t *testing.T) {
	if backupTool("rclone") == "" {
		t.Skip("rclone required for local transport integration")
	}
	e, now, _ := backupExecutionFixture(t)
	config := filepath.Join(e.accountHome(), ".config/rclone")
	if err := os.MkdirAll(config, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(config, "rclone.conf"), []byte("[isolated]\ntype = local\n"), 0600); err != nil {
		t.Fatal(err)
	}
	d := backups.Destination{Type: "rclone", Remote: "isolated", Path: filepath.Join(e.store.state, "remote")}
	archive := filepath.Join(e.store.state, "archive.zip")
	if err := os.WriteFile(archive, []byte("transport-content"), 0600); err != nil {
		t.Fatal(err)
	}
	var first backupArtifact
	for n := 1; n <= 2; n++ {
		id := fmt.Sprintf("%032x", n)
		policy := strings.Repeat("e", 32)
		name := policy + "-files-20261102T020000Z-" + id + ".zip"
		object, err := e.deliver(context.Background(), id, d, archive, name)
		if err != nil {
			t.Fatal(err)
		}
		data, err := os.ReadFile(filepath.Join(d.Path, name))
		if err != nil || string(data) != "transport-content" {
			t.Fatal(string(data), err)
		}
		size, sha, _ := backupFileDigest(context.Background(), archive)
		*now = now.Add(time.Minute)
		a := backupArtifact{PolicyID: policy, RunID: id, Category: "files", Destination: d, Object: object, Name: name, Bytes: size, SHA256: sha, CreatedAt: *now}
		if n == 1 {
			first = a
			if err = os.WriteFile(filepath.Join(d.Path, "unrelated.zip"), []byte("keep"), 0600); err != nil {
				t.Fatal(err)
			}
		}
		if err = e.recordArtifact(a); err != nil {
			t.Fatal(err)
		}
		if err = e.retainArtifacts(context.Background(), id, a, 1); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := os.Stat(filepath.Join(d.Path, first.Name)); !os.IsNotExist(err) {
		t.Fatal("old remote file retained", err)
	}
	if data, err := os.ReadFile(filepath.Join(d.Path, "unrelated.zip")); err != nil || string(data) != "keep" {
		t.Fatal("unrelated remote file removed", err)
	}
}

func TestBackupGdriveTransportBindsAccountAndSanitizesFailures(t *testing.T) {
	e, _, _ := backupExecutionFixture(t)
	account := "backup@example.com"
	base := filepath.Join(e.accountHome(), ".config/gdrive3", account)
	if err := os.MkdirAll(base, 0700); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"tokens.json", "secret.json"} {
		if err := os.WriteFile(filepath.Join(base, name), []byte(`{"secret":"never-return"}`), 0600); err != nil {
			t.Fatal(err)
		}
	}
	tool := filepath.Join(e.store.state, "gdrive")
	calls := filepath.Join(e.store.state, "calls")
	fail := filepath.Join(e.store.state, "fail")
	script := fmt.Sprintf("#!/bin/sh\nif [ -f '%s' ]; then echo 'never-return' >&2; exit 3; fi\nprintf '%%s\\n' \"$@\" > '%s'\ncat \"$HOME/.config/gdrive3/account.json\" > '%s.account'\nif [ \"$2\" = upload ]; then echo object_id_123; fi\n", fail, calls, calls)
	if err := os.WriteFile(tool, []byte(script), 0700); err != nil {
		t.Fatal(err)
	}
	e.tools = func(name string) string {
		if name == "gdrive" {
			return tool
		}
		return ""
	}
	archive := filepath.Join(e.store.state, "archive.zip")
	if err := os.WriteFile(archive, []byte("content"), 0600); err != nil {
		t.Fatal(err)
	}
	id := strings.Repeat("d", 32)
	destination := backups.Destination{Type: "gdrive", Account: account, FolderID: "folder_123"}
	object, err := e.deliver(context.Background(), id, destination, archive, "archive.zip")
	if err != nil || object != "object_id_123" {
		t.Fatal(object, err)
	}
	args, _ := os.ReadFile(calls)
	if !strings.Contains(string(args), "--parent\nfolder_123\n--print-only-id\n") || strings.Contains(string(args), account) {
		t.Fatal("wrong account/target arguments", string(args))
	}
	selected, _ := os.ReadFile(calls + ".account")
	if !strings.Contains(string(selected), account) {
		t.Fatal("account not selected", string(selected))
	}
	a := backupArtifact{PolicyID: strings.Repeat("a", 32), RunID: id, Category: "files", Destination: destination, Object: object, Name: strings.Repeat("a", 32) + "-files-date-" + id + ".zip", SHA256: strings.Repeat("0", 64)}
	if err = e.removeArtifact(context.Background(), id, a); err != nil {
		t.Fatal(err)
	}
	args, _ = os.ReadFile(calls)
	if string(args) != "files\ndelete\nobject_id_123\n" {
		t.Fatal("unsafe remote deletion", string(args))
	}
	if err = os.WriteFile(fail, []byte("fail"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err = e.deliver(context.Background(), id, destination, archive, "archive.zip"); err == nil || strings.Contains(err.Error(), "never-return") {
		t.Fatal("transport diagnostic leaked secrets", err)
	}
	unlock, err := e.lockGdriveAccount(context.Background(), account)
	if err != nil {
		t.Fatal(err)
	}
	defer unlock()
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	if release, err := e.lockGdriveAccount(ctx, account); err == nil {
		release()
		t.Fatal("concurrent token writes allowed")
	}
}
