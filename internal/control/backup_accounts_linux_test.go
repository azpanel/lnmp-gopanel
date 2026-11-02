//go:build linux

package control

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"golang.org/x/sys/unix"
)

func accountTestHome(t *testing.T) string {
	t.Helper()
	return filepath.Join(backupTestInstaller(t).state, "home")
}

func writeAccountFixture(t *testing.T, home, path, data string) string {
	t.Helper()
	path = filepath.Join(home, path)
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(data), 0600); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestBackupAccountsOnlyReturnMetadata(t *testing.T) {
	home := accountTestHome(t)
	for _, name := range []string{"gdrive", "rclone"} {
		got := inspectBackupAccounts(name, home)
		if got.Status != "missing" || got.Items == nil || len(got.Items) != 0 {
			t.Fatal(name, got)
		}
	}
	if _, err := os.Stat(home); !os.IsNotExist(err) {
		t.Fatal("account inspection must not create configuration directories")
	}
	const secret = "SECRET-MUST-NEVER-LEAVE-HELPER"
	writeAccountFixture(t, home, ".config/gdrive3/account.json", `{"current":"main@example.com","client_secret":"`+secret+`"}`)
	writeAccountFixture(t, home, ".config/gdrive3/main@example.com/tokens.json", secret)
	writeAccountFixture(t, home, ".config/gdrive3/main@example.com/secret.json", secret)
	writeAccountFixture(t, home, ".config/gdrive3/another@example.com/tokens.json", secret)
	writeAccountFixture(t, home, ".config/gdrive3/incomplete/secret.json", secret)
	writeAccountFixture(t, home, ".config/rclone/rclone.conf", "# comment\r\n[onedrive]\r\ntype = onedrive\r\ntoken = {\"access_token\":\""+secret+"\"}\r\ndrive_id = "+secret+"\n[archive]\ntype=s3\naccess_key_id="+secret+"\nsecret_access_key="+secret+"\npass="+secret+"\nclient_secret="+secret+"\n")
	gdrive := inspectBackupAccounts("gdrive", home)
	if gdrive.Status != "ready" || len(gdrive.Items) != 2 || gdrive.Items[0].Name != "another@example.com" || !gdrive.Items[1].Current || gdrive.Items[0].Current {
		t.Fatal(gdrive)
	}
	rclone := inspectBackupAccounts("rclone", home)
	if rclone.Status != "ready" || len(rclone.Items) != 2 || rclone.Items[0].Name != "archive" || rclone.Items[0].Type != "s3" || rclone.Items[1].Type != "onedrive" {
		t.Fatal(rclone)
	}
	for _, got := range []any{gdrive, rclone} {
		data, err := json.Marshal(got)
		if err != nil || strings.Contains(string(data), secret) || strings.Contains(string(data), "token") || strings.Contains(string(data), "secret") || strings.Contains(string(data), "drive_id") {
			t.Fatal("credentials escaped the helper")
		}
	}
}

func TestBackupAccountsEncrypted(t *testing.T) {
	home := accountTestHome(t)
	writeAccountFixture(t, home, ".config/rclone/rclone.conf", "# Encrypted rclone configuration File\n\nRCLONE_ENCRYPT_V0:\nprivate-ciphertext")
	got := inspectBackupAccounts("rclone", home)
	if got.Status != "encrypted" || len(got.Items) != 0 || got.Error != "" {
		t.Fatal(got)
	}
}

func TestBackupAccountsRejectUnsafeOrMalformedConfig(t *testing.T) {
	for _, input := range []string{
		"[remote]\ntoken=private\n", "[remote]\ntype=drive\n[remote]\ntype=s3", "[private-token\n", "private-token", "[remote]\ntype=drive\ntype=s3", "[remote]\ntype=drive\nprivate-token", strings.Repeat("x", backupConfigLimit+1),
	} {
		home := accountTestHome(t)
		writeAccountFixture(t, home, ".config/rclone/rclone.conf", input)
		got := inspectBackupAccounts("rclone", home)
		if got.Status != "failed" || len(got.Items) != 0 || got.Error != errBackupAccountConfig.Error() {
			t.Fatal("malformed config must fail without returning parser output")
		}
	}
	for _, kind := range []string{"symlink", "fifo", "writable", "directory"} {
		t.Run(kind, func(t *testing.T) {
			home := accountTestHome(t)
			path := writeAccountFixture(t, home, ".config/rclone/rclone.conf", "[remote]\ntype=drive\n")
			if kind == "writable" {
				if err := os.Chmod(path, 0666); err != nil {
					t.Fatal(err)
				}
			} else {
				if err := os.Remove(path); err != nil {
					t.Fatal(err)
				}
				var err error
				switch kind {
				case "symlink":
					err = os.Symlink("/etc/passwd", path)
				case "fifo":
					err = unix.Mkfifo(path, 0600)
				case "directory":
					err = os.Mkdir(path, 0700)
				}
				if err != nil {
					t.Fatal(err)
				}
			}
			got := inspectBackupAccounts("rclone", home)
			if got.Status != "failed" || len(got.Items) != 0 {
				t.Fatal(kind, got)
			}
		})
	}
	home := accountTestHome(t)
	writeAccountFixture(t, home, ".config/gdrive3/account.json", `{"current":"private-token`)
	got := inspectBackupAccounts("gdrive", home)
	if got.Status != "failed" || strings.Contains(got.Error, "private-token") {
		t.Fatal(got)
	}
}

func TestBackupAccountsCountLimit(t *testing.T) {
	home := accountTestHome(t)
	var config strings.Builder
	for i := 0; i < backupAccountLimit; i++ {
		fmt.Fprintf(&config, "[backup-%04d]\ntype=s3\ntoken=private\n", i)
	}
	writeAccountFixture(t, home, ".config/rclone/rclone.conf", config.String())
	got := inspectBackupAccounts("rclone", home)
	if got.Status != "ready" || len(got.Items) != backupAccountLimit {
		t.Fatal("account list below limit must remain readable")
	}
	config.WriteString("[one-too-many]\ntype=drive\n")
	writeAccountFixture(t, home, ".config/rclone/rclone.conf", config.String())
	got = inspectBackupAccounts("rclone", home)
	if got.Status != "failed" || len(got.Items) != 0 {
		t.Fatal("excessive account lists must fail with a bounded response")
	}
}
