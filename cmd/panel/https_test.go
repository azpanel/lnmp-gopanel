package main

import (
	"bytes"
	"crypto/tls"
	"crypto/x509"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/azpanel/lnmp-gopanel/internal/config"
	"github.com/azpanel/lnmp-gopanel/internal/paneltls"
	"golang.org/x/crypto/bcrypt"
	"gopkg.in/yaml.v3"
)

func TestPanelHTTPSProcess(t *testing.T) {
	dir := os.Getenv("GOPANEL_HTTPS_TEST_DIR")
	if dir == "" {
		return
	}
	os.Args = []string{"lnmp-gopanel", "--config", filepath.Join(dir, "config.yaml"), "--services", filepath.Join(dir, "services.yaml"), "--allow-root-for-development"}
	if err := run(); err != nil {
		t.Fatal(err)
	}
}

func TestPanelServesOnlyHTTPS(t *testing.T) {
	dir := t.TempDir()
	hash, err := bcrypt.GenerateFromPassword([]byte("https-test-password"), bcrypt.MinCost)
	if err != nil {
		t.Fatal(err)
	}
	c := config.Config{Listen: "127.0.0.1:0", Username: "admin", PasswordHash: string(hash)}
	if err := prepareHTTPS(&c, dir, "", ""); err != nil {
		t.Fatal(err)
	}
	data, err := yaml.Marshal(c)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "config.yaml"), data, 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "services.yaml"), []byte("services: []\n"), 0600); err != nil {
		t.Fatal(err)
	}
	cert, err := os.ReadFile(c.TLS.CertFile)
	if err != nil {
		t.Fatal(err)
	}
	roots := x509.NewCertPool()
	roots.AppendCertsFromPEM(cert)
	transport := &http.Transport{TLSClientConfig: &tls.Config{RootCAs: roots, MinVersion: tls.VersionTLS12}}
	client := &http.Client{Transport: transport, Timeout: time.Second}
	defer transport.CloseIdleConnections()
	var output bytes.Buffer
	child := exec.Command(os.Args[0], "-test.run=^TestPanelHTTPSProcess$")
	child.Env = append(os.Environ(), "GOPANEL_HTTPS_TEST_DIR="+dir)
	child.Stdout, child.Stderr = &output, &output
	if err := child.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = child.Process.Signal(syscall.SIGTERM)
		done := make(chan error, 1)
		go func() { done <- child.Wait() }()
		select {
		case err := <-done:
			if err != nil {
				t.Errorf("panel exited: %v\n%s", err, output.String())
			}
		case <-time.After(5 * time.Second):
			_ = child.Process.Kill()
			<-done
			t.Error("panel did not shut down")
		}
		if t.Failed() {
			t.Log(output.String())
		}
	})
	endpoint := "https://" + c.Listen
	deadline := time.Now().Add(10 * time.Second)
	for {
		response, err := client.Get(endpoint + "/api/v1/auth/session")
		if err == nil {
			response.Body.Close()
			if response.StatusCode != http.StatusUnauthorized {
				t.Fatal("unexpected HTTPS response", response.StatusCode)
			}
			break
		}
		if time.Now().After(deadline) {
			t.Fatal(err)
		}
		time.Sleep(25 * time.Millisecond)
	}
	response, err := client.Post(endpoint+"/api/v1/auth/login", "application/json", strings.NewReader(`{"username":"admin","password":"https-test-password"}`))
	if err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
	if response.StatusCode != http.StatusOK || len(response.Cookies()) != 1 || !response.Cookies()[0].Secure {
		t.Fatalf("HTTPS login failed or cookie is not secure: %d %+v", response.StatusCode, response.Cookies())
	}
	plain := &http.Client{Timeout: time.Second}
	response, err = plain.Get("http://" + c.Listen + "/api/v1/auth/session")
	if err == nil {
		body, _ := io.ReadAll(response.Body)
		response.Body.Close()
		if response.StatusCode != http.StatusBadRequest || !strings.Contains(string(body), "HTTPS") || len(response.Cookies()) != 0 {
			t.Fatalf("plaintext request reached the application: %d %s", response.StatusCode, body)
		}
	}
	oldTLS, err := tls.DialWithDialer(&net.Dialer{Timeout: time.Second}, "tcp", c.Listen, &tls.Config{RootCAs: roots, MinVersion: tls.VersionTLS10, MaxVersion: tls.VersionTLS11})
	if err == nil {
		oldTLS.Close()
		t.Fatal("TLS before 1.2 was accepted")
	}
}

func TestMigrateHTTPConfiguration(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	hash, _ := bcrypt.GenerateFromPassword([]byte("migration-password"), bcrypt.MinCost)
	before := config.Config{Listen: "127.0.0.1:8090", Username: "existing-admin", PasswordHash: string(hash), PublicOrigin: "http://old.example:8090", Screenshots: config.ScreenshotConfig{StateDir: "/keep-screenshot-state"}}
	data, _ := yaml.Marshal(before)
	if err := os.WriteFile(path, data, 0600); err != nil {
		t.Fatal(err)
	}
	if err := configureHTTPS([]string{"--config", path}); err != nil {
		t.Fatal(err)
	}
	after, err := config.Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if after.Username != before.Username || after.PasswordHash != before.PasswordHash || after.Screenshots != before.Screenshots || after.PublicOrigin != "" || !after.SecureCookie || !strings.HasPrefix(after.Listen, "0.0.0.0:") || after.Listen == "0.0.0.0:0" {
		t.Fatalf("bad migration: %+v", after)
	}
	if _, err := paneltls.Load(after.TLS.CertFile, after.TLS.KeyFile); err != nil {
		t.Fatal(err)
	}
}
