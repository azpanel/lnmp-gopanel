//go:build linux

package control

import (
	"os"
	"path/filepath"
	"testing"

	"golang.org/x/sys/unix"
)

func rewriteTrustFixture(t *testing.T) string {
	t.Helper()
	if os.Geteuid() != 0 {
		t.Skip("requires root ownership checks")
	}
	// /tmp is deliberately not trusted by TrustedPath.
	dir, err := os.MkdirTemp("/root", "gopanel-rewrite-test-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(dir) })
	return dir
}

func TestRewriteTemplatesSecureAllNames(t *testing.T) {
	dir := rewriteTrustFixture(t)
	for _, name := range []string{"wordpress.conf", "discuz.conf", "typecho.conf", "custom-template.conf"} {
		t.Run(name, func(t *testing.T) {
			path := filepath.Join(dir, name)
			data := []byte("location / { try_files $uri $uri/ /index.php?$args; }\n")
			if err := os.WriteFile(path, data, 0644); err != nil {
				t.Fatal(err)
			}
			if err := os.Chmod(path, 0666); err != nil {
				t.Fatal(err)
			}
			if TrustedPath(path) == nil {
				t.Fatal("writable fixture unexpectedly trusted")
			}
			if err := trustedRewritePathAt(path, dir); err != nil {
				t.Fatal(err)
			}
			info, _ := os.Stat(path)
			got, _ := os.ReadFile(path)
			if info.Mode().Perm() != 0644 || string(got) != string(data) {
				t.Fatalf("unexpected permissions/content: %v %q", info.Mode(), got)
			}
			if err := TrustedPath(path); err != nil {
				t.Fatal("template still untrusted", err)
			}
		})
	}
}

func TestRewriteTemplatesDoNotSecureUnsafeFiles(t *testing.T) {
	for _, scenario := range []string{"symlink", "hardlink", "owner", "parent", "directory", "fifo", "outside", "extension"} {
		t.Run(scenario, func(t *testing.T) {
			base := rewriteTrustFixture(t)
			dir := filepath.Join(base, "rewrite")
			if err := os.Mkdir(dir, 0755); err != nil {
				t.Fatal(err)
			}
			path := filepath.Join(dir, "wordpress.conf")
			if err := os.WriteFile(path, []byte("original"), 0644); err != nil {
				t.Fatal(err)
			}
			if err := os.Chmod(path, 0666); err != nil {
				t.Fatal(err)
			}
			var err error
			switch scenario {
			case "symlink":
				target := filepath.Join(base, "target")
				err = os.Rename(path, target)
				if err == nil {
					err = os.Symlink(target, path)
				}
			case "hardlink":
				err = os.Link(path, filepath.Join(base, "linked"))
			case "owner":
				err = os.Chown(path, 65534, 65534)
			case "parent":
				err = os.Chmod(dir, 0777)
			case "directory", "fifo":
				err = os.Remove(path)
				if err == nil && scenario == "directory" {
					err = os.Mkdir(path, 0777)
				} else if err == nil {
					err = unix.Mkfifo(path, 0666)
				}
			case "outside":
				other := filepath.Join(base, "other.conf")
				err = os.Rename(path, other)
				path = other
			case "extension":
				other := filepath.Join(dir, "script.sh")
				err = os.Rename(path, other)
				path = other
			}
			if err != nil {
				t.Fatal(err)
			}
			before, err := os.Stat(path)
			if err != nil {
				t.Fatal(err)
			}
			if err = trustedRewritePathAt(path, dir); err == nil {
				t.Fatal("unsafe file accepted")
			}
			after, _ := os.Stat(path)
			if before.Mode() != after.Mode() {
				t.Fatal("unsafe file permissions changed")
			}
		})
	}
}
