package logs

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

func TestRegistryKeepsLegacyPathsRemovable(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "state")
	if err := os.Mkdir(dir, 0700); err != nil {
		t.Fatal(err)
	}
	paths := []string{"/var/log/syslog", "/srv/app.log.1", "/srv/database.php", "/srv/app.txt"}
	data, err := json.Marshal(paths)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "custom-logs.json"), data, 0600); err != nil {
		t.Fatal(err)
	}
	r, err := OpenRegistry(dir)
	if err != nil || len(r.Paths()) != len(paths) {
		t.Fatal("legacy catalog could not load", err)
	}
	for _, path := range paths[:3] {
		if err := r.Add(path); !errors.Is(err, ErrInvalid) {
			t.Fatal("legacy path could be added again", path, err)
		}
		if err := r.Remove(path); err != nil {
			t.Fatal(err)
		}
	}
	if err := r.Add("/srv/another.LOG"); err != nil {
		t.Fatal(err)
	}
	r, err = OpenRegistry(dir)
	if err != nil || len(r.Paths()) != 2 || !r.Contains("/srv/app.txt") || !r.Contains("/srv/another.LOG") {
		t.Fatal("catalog removal or addition did not persist", err)
	}
}

func TestRegistryPersistsAndBoundsCustomFiles(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "state")
	r, err := OpenRegistry(dir)
	if err != nil {
		t.Fatal(err)
	}
	if err := r.Add("/etc/shadow"); !errors.Is(err, ErrInvalid) || len(r.Paths()) != 0 {
		t.Fatal("sensitive path stored", err)
	}
	for i := range MaxCustomFiles {
		if err := r.Add(fmt.Sprintf("/srv/app/logs/%d.log", i)); err != nil {
			t.Fatal(err)
		}
	}
	if err := r.Add("/srv/app/logs/0.log"); err != nil || len(r.Paths()) != MaxCustomFiles {
		t.Fatal("duplicate changed registry", err)
	}
	if err := r.Add("/srv/app/logs/overflow.log"); !errors.Is(err, ErrInvalid) {
		t.Fatal("registry limit not applied", err)
	}
	reloaded, err := OpenRegistry(dir)
	if err != nil || !reloaded.Contains("/srv/app/logs/0.log") {
		t.Fatal("registry did not survive reload", err)
	}
	if err := reloaded.Remove("/srv/app/logs/0.log"); err != nil {
		t.Fatal(err)
	}
	again, err := OpenRegistry(dir)
	if err != nil || again.Contains("/srv/app/logs/0.log") || len(again.Paths()) != MaxCustomFiles-1 {
		t.Fatal("removal not persisted", err)
	}
	info, err := os.Stat(filepath.Join(dir, "custom-logs.json"))
	if err != nil || info.Mode().Perm() != 0600 {
		t.Fatal("state file not private", err)
	}
}

func TestRegistryRejectsUnsafeStateAndPreservesMemoryOnFailure(t *testing.T) {
	for _, content := range []string{"invalid", "null", `["/etc/shadow"]`, `["/srv/app.log","/srv/app.log"]`} {
		dir := filepath.Join(t.TempDir(), "state")
		if err := os.Mkdir(dir, 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, "custom-logs.json"), []byte(content), 0600); err != nil {
			t.Fatal(err)
		}
		if _, err := OpenRegistry(dir); err == nil {
			t.Fatalf("invalid state accepted: %s", content)
		}
	}
	dir := filepath.Join(t.TempDir(), "state")
	r, err := OpenRegistry(dir)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(dir, "custom-logs.json"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := r.Add("/srv/app.log"); err == nil || r.Contains("/srv/app.log") {
		t.Fatal("failed save published an in-memory change")
	}
	linked := filepath.Join(t.TempDir(), "state-link")
	if err := os.Symlink(dir, linked); err != nil {
		t.Fatal(err)
	}
	if _, err := OpenRegistry(linked); err == nil {
		t.Fatal("linked state directory accepted")
	}
}
