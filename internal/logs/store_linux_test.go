//go:build linux

package logs

import (
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
