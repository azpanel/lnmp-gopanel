package main

import (
	"errors"
	"flag"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"golang.org/x/crypto/bcrypt"
	"golang.org/x/sys/unix"
)

func TestMetricsHelpAndInvalidArgumentsDoNotConfigureServices(t *testing.T) {
	if err := configureMetrics([]string{"configure", "--help"}); !errors.Is(err, flag.ErrHelp) {
		t.Fatal(err)
	}
	for _, args := range [][]string{{}, {"unknown"}, {"configure", "extra"}, {"check", "--unknown"}} {
		if configureMetrics(args) == nil {
			t.Fatal("invalid arguments accepted")
		}
	}
	if configureMetrics([]string{"configure", "--config", filepath.Join(t.TempDir(), "missing")}) == nil {
		t.Fatal("noninteractive wizard accepted")
	}
}
func TestMetricsCheckIsReadOnlyAndOnlyProbesSelectedService(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("Active connections: 0\nserver accepts handled requests\n1 1 1\nReading: 0 Writing: 0 Waiting: 0\n"))
	}))
	defer server.Close()
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	hash, _ := bcrypt.GenerateFromPassword([]byte("test-password"), bcrypt.MinCost)
	data := "username: admin\npassword_hash: " + string(hash) + "\ntls:\n  cert_file: /unused/cert\n  key_file: /unused/key\nmetrics:\n  nginx_status_url: " + server.URL + "\n"
	if err := os.WriteFile(path, []byte(data), 0600); err != nil {
		t.Fatal(err)
	}
	if err := configureMetrics([]string{"check", "--config", path, "--only", "nginx"}); err != nil {
		t.Fatal(err)
	}
	for _, ids := range []string{"processes", "nginx,nginx", "nginx,database"} {
		if err := configureMetrics([]string{"check", "--config", path, "--only", ids}); err == nil {
			t.Fatal("invalid or unavailable probes accepted", ids)
		}
	}
	entries, err := os.ReadDir(dir)
	if err != nil || len(entries) != 1 {
		t.Fatal("check wrote state", err)
	}
	after, _ := os.ReadFile(path)
	if string(after) != data {
		t.Fatal("check changed config")
	}
}
func TestMetricsInstallLockRejectsConcurrentOperationsAndReusesInheritedLock(t *testing.T) {
	if os.Geteuid() != 0 {
		t.Skip("root-owned installer locks")
	}
	path := filepath.Join(t.TempDir(), "install.lock")
	held, err := unix.Open(path, unix.O_CREAT|unix.O_RDWR, 0600)
	if err != nil {
		t.Fatal(err)
	}
	defer unix.Close(held)
	if err = unix.Flock(held, unix.LOCK_EX|unix.LOCK_NB); err != nil {
		t.Fatal(err)
	}
	if _, err = metricsInstallLockAt(path, -1); err == nil {
		t.Fatal("concurrent operation accepted")
	}
	release, err := metricsInstallLockAt(path, held)
	if err != nil {
		t.Fatal("installer inherited lock not reused", err)
	}
	release()
	if _, err = metricsInstallLockAt(path, -1); err == nil {
		t.Fatal("child released parent's lock")
	}
	unix.Flock(held, unix.LOCK_UN)
	release, err = metricsInstallLockAt(path, -1)
	if err != nil {
		t.Fatal(err)
	}
	release()
	link := path + ".link"
	if err = os.Symlink(path, link); err != nil {
		t.Fatal(err)
	}
	if _, err = metricsInstallLockAt(link, -1); err == nil {
		t.Fatal("symlink lock accepted")
	}
	if err = os.Chmod(path, 0666); err != nil {
		t.Fatal(err)
	}
	if _, err = metricsInstallLockAt(path, -1); err == nil || !strings.Contains(err.Error(), "权限") {
		t.Fatal("unsafe lock accepted", err)
	}
}
