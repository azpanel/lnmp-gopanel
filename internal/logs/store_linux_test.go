//go:build linux

package logs

import (
	"context"
	"errors"
	"golang.org/x/sys/unix"
	"os"
	"path/filepath"
	"testing"
)

func TestOpenRegularRejectsSymlinksAndSpecialFiles(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "real.log")
	if err := os.WriteFile(target, []byte("log"), 0600); err != nil {
		t.Fatal(err)
	}
	f, err := openRegular(target)
	if err != nil {
		t.Fatal(err)
	}
	f.Close()
	link := filepath.Join(dir, "link.log")
	if err := os.Symlink(target, link); err != nil {
		t.Fatal(err)
	}
	fifo := filepath.Join(dir, "fifo.log")
	if err := unix.Mkfifo(fifo, 0600); err != nil {
		t.Fatal(err)
	}
	linkedDir := filepath.Join(dir, "linked-dir")
	if err := os.Symlink(dir, linkedDir); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{link, fifo, dir, filepath.Join(linkedDir, "real.log")} {
		if f, err := openRegular(path); err == nil {
			f.Close()
			t.Fatalf("unsafe file opened: %s", path)
		}
	}
}

func TestCustomStoreReadsAndRejectsUnsafeFiles(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "server log.txt")
	if err := os.WriteFile(path, []byte("hello\nworld\n"), 0600); err != nil {
		t.Fatal(err)
	}
	store := Store{}
	info, err := store.Read(context.Background(), Request{Path: path, Mode: "stat"})
	if err != nil || info.Size != 12 || len(info.Lines) != 0 || info.Start == "" {
		t.Fatalf("stat: %+v, %v", info, err)
	}
	window, err := store.Read(context.Background(), Request{Path: path, Mode: "tail"})
	if err != nil || len(window.Lines) != 2 || window.Lines[0].Text != "hello" {
		t.Fatalf("read: %+v, %v", window, err)
	}
	for _, content := range [][]byte{[]byte("binary\x00content"), []byte("-----BEGIN RSA PRIVATE KEY-----\nsecret")} {
		if err := os.WriteFile(path, content, 0600); err != nil {
			t.Fatal(err)
		}
		if _, err := store.Read(context.Background(), Request{Path: path, Mode: "stat"}); !errors.Is(err, ErrInvalid) {
			t.Fatalf("unsafe content accepted: %v", err)
		}
	}
	if err := os.WriteFile(path, []byte("ordinary log\n"), 0600); err != nil {
		t.Fatal(err)
	}
	alias := filepath.Join(dir, "alias.log")
	if err := os.Link(path, alias); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Read(context.Background(), Request{Path: alias, Mode: "tail"}); !errors.Is(err, ErrInvalid) {
		t.Fatalf("hardlink accepted: %v", err)
	}
	link := filepath.Join(dir, "link.log")
	if err := os.Symlink(path, link); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Read(context.Background(), Request{Path: link, Mode: "stat"}); err == nil {
		t.Fatal("symlink accepted")
	}
}

func TestStoreRejectsNonLogExtensions(t *testing.T) {
	dir := t.TempDir()
	for _, name := range []string{"syslog", "database.php", "app.log.1", "app.txt.bak"} {
		path := filepath.Join(dir, name)
		if err := os.WriteFile(path, []byte("private content\n"), 0600); err != nil {
			t.Fatal(err)
		}
		for _, mode := range []string{"stat", "tail"} {
			if _, err := (Store{}).Read(context.Background(), Request{Path: path, Mode: mode}); !errors.Is(err, ErrInvalid) {
				t.Fatalf("disallowed extension read: %s, %s, %v", name, mode, err)
			}
		}
	}
}
