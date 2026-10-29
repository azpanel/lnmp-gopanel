//go:build linux

package logs

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"golang.org/x/sys/unix"
)

func TestLoginLogAppendReadAndRestart(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "state")
	l, err := OpenLoginLog(dir)
	if err != nil {
		t.Fatal(err)
	}
	password := "wrong\n\"password"
	event := LoginEvent{IP: "2001:db8::1", UA: "browser\nforged line", Username: "admin", Stage: "password", Result: "failure", Reason: "invalid_credentials", Password: &password}
	var workers sync.WaitGroup
	for range 32 {
		workers.Add(1)
		go func() {
			defer workers.Done()
			if err := l.Append(event); err != nil {
				t.Error(err)
			}
		}()
	}
	workers.Wait()
	data, err := os.ReadFile(l.Path())
	if err != nil || strings.Count(string(data), "\n") != 32 {
		t.Fatal("interleaved or injected lines", err)
	}
	for _, line := range strings.Split(strings.TrimSpace(string(data)), "\n") {
		var got LoginEvent
		if err := json.Unmarshal([]byte(line), &got); err != nil || got.Time.IsZero() || got.IP != event.IP || got.UA != event.UA || got.Password == nil || *got.Password != password {
			t.Fatalf("bad event: %s, %v", line, err)
		}
	}
	l, err = OpenLoginLog(dir)
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	window, err := l.Read(ctx, Request{Path: l.Path(), Mode: "tail"})
	if err != nil || len(window.Lines) != 32 {
		t.Fatal("restart lost events", err, len(window.Lines))
	}
	// Storage strips passwords from all other kinds of events.
	for _, item := range []LoginEvent{
		{Result: "success", Stage: "password", Reason: "authenticated", Password: &password},
		{Result: "failure", Stage: "totp", Reason: "invalid_second_factor", Password: &password},
		{Result: "failure", Stage: "password", Reason: "authentication_unavailable", Password: &password},
	} {
		if err := l.Append(item); err != nil {
			t.Fatal(err)
		}
	}
	after, err := l.Read(ctx, Request{Path: l.Path(), Mode: "after", End: window.End})
	if err != nil || len(after.Lines) != 3 {
		t.Fatal("incremental read failed", err, len(after.Lines))
	}
	for _, line := range after.Lines {
		if strings.Contains(line.Text, "password\":") {
			t.Fatal("password leaked", line.Text)
		}
	}
	filtered, err := l.Read(ctx, Request{Path: l.Path(), Mode: "tail", Rules: []Rule{{Kind: "keyword", Action: "show", Pattern: `"result":"success"`, Enabled: true}}})
	if err != nil || len(filtered.Lines) != 1 {
		t.Fatal("filter failed", err, len(filtered.Lines))
	}
	for _, req := range []Request{
		{Path: filepath.Join(dir, "auth.json"), Mode: "tail"},
		{Path: l.Path(), Mode: "invalid"},
		{Path: l.Path(), Mode: "before", Start: "invalid"},
		{Path: l.Path(), Mode: "tail", Rules: []Rule{{Kind: "regex", Action: "show", Pattern: "[", Enabled: true}}},
	} {
		if _, err := l.Read(ctx, req); !errors.Is(err, ErrInvalid) {
			t.Fatal("invalid read accepted", req, err)
		}
	}
	if err := os.Truncate(l.Path(), 0); err != nil {
		t.Fatal(err)
	}
	if _, err := l.Read(ctx, Request{Path: l.Path(), Mode: "after", End: after.End}); !errors.Is(err, ErrChanged) {
		t.Fatal("truncation not detected", err)
	}
	for path, mode := range map[string]os.FileMode{dir: 0700, l.Path(): 0600} {
		info, err := os.Stat(path)
		if err != nil || info.Mode().Perm() != mode {
			t.Fatal("incorrect permissions", path, err)
		}
	}
	if ValidatePath("/var/lib/lnmp-gopanel/login.log") == nil {
		t.Fatal("general path policy was relaxed")
	}
}

func TestLoginLogRejectsUnsafeFilesAndRechecks(t *testing.T) {
	for _, kind := range []string{"symlink", "hardlink", "fifo", "directory", "public-file", "public-directory", "symlink-directory"} {
		t.Run(kind, func(t *testing.T) {
			dir := filepath.Join(t.TempDir(), "state")
			l, err := OpenLoginLog(dir)
			if err != nil {
				t.Fatal(err)
			}
			check := func(err error) {
				t.Helper()
				if err != nil {
					t.Fatal(err)
				}
			}
			target := filepath.Join(t.TempDir(), "target")
			check(os.WriteFile(target, []byte("unchanged"), 0600))
			switch kind {
			case "symlink", "hardlink", "fifo", "directory":
				check(os.Remove(l.Path()))
				switch kind {
				case "symlink":
					check(os.Symlink(target, l.Path()))
				case "hardlink":
					check(os.Link(target, l.Path()))
				case "fifo":
					check(unix.Mkfifo(l.Path(), 0600))
				case "directory":
					check(os.Mkdir(l.Path(), 0700))
				}
			case "public-file":
				check(os.Chmod(l.Path(), 0644))
			case "public-directory":
				check(os.Chmod(dir, 0755))
			case "symlink-directory":
				check(os.Rename(dir, dir+"-real"))
				check(os.Symlink(dir+"-real", dir))
			}
			if err := l.Append(LoginEvent{Result: "success"}); err == nil {
				t.Fatal("unsafe write accepted")
			}
			if _, err := l.Read(context.Background(), Request{Path: l.Path(), Mode: "tail"}); err == nil {
				t.Fatal("unsafe read accepted")
			}
			if _, err := OpenLoginLog(dir); err == nil {
				t.Fatal("unsafe initialization accepted")
			}
			if data, err := os.ReadFile(target); err != nil || string(data) != "unchanged" {
				t.Fatal("target changed", err)
			}
		})
	}
}
