//go:build linux

package control

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"golang.org/x/sys/unix"
)

func TestPHPMyAdminSavedLocations(t *testing.T) {
	base := "Installed_PhpMyAdmin_Mode=random\nInstalled_PhpMyAdmin_Dir=" + strings.Repeat("a", 32) + "\nInstalled_Website_Dir=/home/wwwroot/default\nInstalled_Server_IP=192.0.2.1\n"
	location, err := parsePHPMyAdminLocation([]byte(base))
	if err != nil || location.accessURL() != "https://192.0.2.1/"+strings.Repeat("a", 32)+"/" {
		t.Fatal(location, err)
	}
	basic := strings.ReplaceAll(strings.ReplaceAll(base, "Mode=random", "Mode=basic"), strings.Repeat("a", 32), "phpmyadmin")
	if location, err = parsePHPMyAdminLocation([]byte(basic)); err != nil || location.accessURL() != "https://192.0.2.1/phpmyadmin/" {
		t.Fatal(location, err)
	}
	escaped := strings.ReplaceAll(basic, "/home/wwwroot/default", `/home/wwwroot/my\ site`)
	if location, err = parsePHPMyAdminLocation([]byte(escaped)); err != nil || location.Root != "/home/wwwroot/my site" {
		t.Fatal(location, err)
	}
	for _, data := range []string{
		strings.ReplaceAll(base, "Mode=random", "Mode=basic"),
		strings.ReplaceAll(base, strings.Repeat("a", 32), "../phpmyadmin"),
		strings.ReplaceAll(base, "192.0.2.1", "0.0.0.0"),
		strings.ReplaceAll(base, "192.0.2.1", "192.00.2.1"),
		strings.ReplaceAll(base, "/home/wwwroot/default", "/home/wwwroot/../default"),
		strings.ReplaceAll(base, "/home/wwwroot/default", "$(touch /tmp/unexpected)"),
		base + "Installed_Server_IP=192.0.2.2\n",
		base + "echo untrusted\n",
		strings.ReplaceAll(base, "Installed_Server_IP=192.0.2.1\n", ""),
	} {
		if _, err := parsePHPMyAdminLocation([]byte(data)); err == nil {
			t.Fatal("accepted invalid configuration", data)
		}
	}
}

func TestPHPMyAdminProbeAndRestartRecovery(t *testing.T) {
	root := t.TempDir()
	security := filepath.Join(root, "security.conf")
	docroot := filepath.Join(root, "website")
	dir := strings.Repeat("b", 32)
	app := filepath.Join(docroot, dir)
	version := filepath.Join(app, "libraries/classes/Version.php")
	if err := os.MkdirAll(filepath.Dir(version), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(version, []byte("<?php\npublic const VERSION = '5.2.3' . VERSION_SUFFIX;\nfile_put_contents('/tmp/should-never-execute', 'bad');"), 0666); err != nil {
		t.Fatal(err)
	}
	config := "Installed_PhpMyAdmin_Mode=random\nInstalled_PhpMyAdmin_Dir=" + dir + "\nInstalled_Website_Dir=" + docroot + "\nInstalled_Server_IP=192.0.2.9\n"
	if err := os.WriteFile(security, []byte(config), 0600); err != nil {
		t.Fatal(err)
	}
	trust := func(string) error { return nil }
	for i := 0; i < 2; i++ {
		item := probePHPMyAdminAt(security, docroot, trust)
		if item.Status != "ready" || item.Version != "5.2.3" || item.Path != app || item.AccessURL != "https://192.0.2.9/"+dir+"/" {
			t.Fatal(item)
		}
	}
	item := probePHPMyAdminAt(security, docroot, func(string) error { return errors.New("unsafe") })
	if item.Status != "failed" || item.AccessURL != "" {
		t.Fatal(item)
	}
	if err := os.Remove(version); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("/etc/passwd", version); err != nil {
		t.Fatal(err)
	}
	if item = probePHPMyAdminAt(security, docroot, trust); item.Status != "failed" || item.AccessURL != "" {
		t.Fatal(item)
	}
	if err := os.Remove(version); err != nil {
		t.Fatal(err)
	}
	if err := unix.Mkfifo(version, 0600); err != nil {
		t.Fatal(err)
	}
	if item = probePHPMyAdminAt(security, docroot, trust); item.Status != "failed" {
		t.Fatal(item)
	}
	if err := os.RemoveAll(app); err != nil {
		t.Fatal(err)
	}
	if item = probePHPMyAdminAt(security, docroot, trust); item.Status != "failed" || !strings.Contains(item.Error, "目录不存在") {
		t.Fatal(item)
	}
	if err := os.Remove(security); err != nil {
		t.Fatal(err)
	}
	if item = probePHPMyAdminAt(security, docroot, trust); item.Status != "missing" {
		t.Fatal(item)
	}
	legacy := filepath.Join(docroot, "phpmyadmin/libraries/classes")
	if err := os.MkdirAll(legacy, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(legacy, "Version.php"), []byte("const VERSION = '5.2.2';"), 0644); err != nil {
		t.Fatal(err)
	}
	if item = probePHPMyAdminAt(security, docroot, trust); item.Status != "ready" || item.Version != "5.2.2" || item.AccessURL != "" {
		t.Fatal(item)
	}
}

func TestPHPMyAdminReadsStayInsideApp(t *testing.T) {
	root := t.TempDir()
	outside := t.TempDir()
	if err := os.WriteFile(filepath.Join(outside, "Version.php"), []byte("const VERSION = '5.2.3';"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(root, "libraries")); err != nil {
		t.Fatal(err)
	}
	if _, err := readPHPMyAdminData(root, "libraries/Version.php"); err == nil {
		t.Fatal("followed a symlink outside application")
	}
	if err := os.WriteFile(filepath.Join(root, "large"), make([]byte, 65537), 0644); err != nil {
		t.Fatal(err)
	}
	if _, err := readPHPMyAdminData(root, "large"); err == nil {
		t.Fatal("read oversized metadata")
	}
}
