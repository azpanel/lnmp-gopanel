//go:build linux

package control

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"
)

// All Nginx processes, ports, logs and configuration are confined to this fixture.
func TestBasicAuthNginxIntegration(t *testing.T) {
	binary, err := exec.LookPath("nginx")
	if err != nil {
		t.Skip("nginx not installed")
	}
	if _, err = exec.LookPath("openssl"); err != nil {
		t.Skip("openssl not installed")
	}
	p, r := authFixture(t)
	p.hash = hashBasicPassword
	dir := p.store.Prefix
	root := filepath.Join(dir, "www")
	tokens := filepath.Join(dir, "tokens")
	for _, path := range []string{root, tokens} {
		if err = os.MkdirAll(path, 0755); err != nil {
			t.Fatal(err)
		}
	}
	for _, name := range []string{"index.html", "style.css"} {
		if err = os.WriteFile(filepath.Join(root, name), []byte("protected-content"), 0644); err != nil {
			t.Fatal(err)
		}
	}
	if err = os.WriteFile(filepath.Join(tokens, "test-token"), []byte("acme-content"), 0644); err != nil {
		t.Fatal(err)
	}
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { fmt.Fprint(w, "upstream-content") }))
	defer upstream.Close()
	allocate := func() int {
		l, err := net.Listen("tcp", "127.0.0.1:0")
		if err != nil {
			t.Fatal(err)
		}
		defer l.Close()
		return l.Addr().(*net.TCPAddr).Port
	}
	ports := []int{allocate(), allocate()}
	original := fmt.Sprintf("# preserved\nserver { listen 127.0.0.1:%d; root %q; location ^~ /.well-known/acme-challenge/ { alias %q; } location /proxy/ { proxy_pass %s; } location ~ \\.php$ { proxy_pass %s; } }\nserver { listen 127.0.0.1:%d; root %q; }\n", ports[0], root, tokens+"/", upstream.URL, upstream.URL, ports[1], root)
	sitePath := filepath.Join(p.store.Directory, r.Name)
	if err = os.WriteFile(sitePath, []byte(original), 0644); err != nil {
		t.Fatal(err)
	}
	user := ""
	if os.Geteuid() == 0 {
		user = "user root;\n"
	}
	main := fmt.Sprintf("%sdaemon off; worker_processes 1; pid %q; error_log %q notice; events { worker_connections 32; } http { access_log off; include %q; }\n", user, filepath.Join(dir, "nginx.pid"), filepath.Join(dir, "error.log"), sitePath)
	config := filepath.Join(dir, "nginx.conf")
	if err = os.WriteFile(config, []byte(main), 0644); err != nil {
		t.Fatal(err)
	}
	run := func(ctx context.Context, args ...string) error {
		cmd := exec.CommandContext(ctx, binary, append([]string{"-p", dir + "/", "-c", config}, args...)...)
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Logf("fixture nginx: %s", out)
		}
		return err
	}
	p.check = func(ctx context.Context) error { return run(ctx, "-t") }
	p.reload = func(ctx context.Context) error { return run(ctx, "-s", "reload") }
	p.global = func(context.Context) (string, error) { return main, nil }
	cmd := exec.Command(binary, "-p", dir+"/", "-c", config)
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	if err = cmd.Start(); err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	t.Cleanup(func() {
		_ = cmd.Process.Signal(syscall.SIGQUIT)
		select {
		case <-done:
		case <-time.After(3 * time.Second):
			_ = syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
			<-done
		}
	})
	client := &http.Client{Timeout: time.Second, Transport: &http.Transport{DisableKeepAlives: true}}
	defer client.CloseIdleConnections()
	code := func(port int, path, username, password string) int {
		req, _ := http.NewRequest("GET", fmt.Sprintf("http://127.0.0.1:%d%s", port, path), nil)
		if username != "" {
			req.SetBasicAuth(username, password)
		}
		resp, err := client.Do(req)
		if err != nil {
			return 0
		}
		defer resp.Body.Close()
		return resp.StatusCode
	}
	wait := func(expected int, username, password string) {
		t.Helper()
		deadline := time.Now().Add(5 * time.Second)
		stable := 0
		for time.Now().Before(deadline) {
			if code(ports[0], "/", username, password) == expected {
				stable++
				if stable == 5 {
					return
				}
			} else {
				stable = 0
			}
			time.Sleep(20 * time.Millisecond)
		}
		log, _ := os.ReadFile(filepath.Join(dir, "error.log"))
		t.Fatalf("expected HTTP %d, nginx log: %s", expected, log)
	}
	wait(200, "", "")
	if _, err = p.execute(context.Background(), "basic-auth-set", r); err != nil {
		t.Fatal(err)
	}
	wait(401, "", "")
	for _, port := range ports {
		for _, path := range []string{"/", "/style.css", "/proxy/", "/index.php"} {
			if port == ports[1] && (path == "/proxy/" || path == "/index.php") {
				continue
			}
			if got := code(port, path, "", ""); got != 401 {
				t.Fatal("unprotected resource", port, path, got)
			}
			if got := code(port, path, r.Username, "wrong-password"); got != 401 {
				t.Fatal("wrong credentials accepted", port, path, got)
			}
			if got := code(port, path, r.Username, r.Password); got != 200 {
				t.Fatal("valid credentials rejected", port, path, got)
			}
		}
	}
	if got := code(ports[0], "/.well-known/acme-challenge/test-token", "", ""); got != 200 {
		t.Fatal("ACME requires authentication", got)
	}
	if got := code(ports[1], "/.well-known/acme-challenge/missing", "", ""); got != 404 {
		t.Fatal("fallback ACME requires authentication", got)
	}
	state, err := p.execute(context.Background(), "basic-auth-get", r)
	if err != nil || state.Username != r.Username || state.Status != "enabled" {
		t.Fatal(state, err)
	}
	oldUser, oldPass := r.Username, r.Password
	r.Username = "new-user"
	r.Password = "new-password-2026"
	if _, err = p.execute(context.Background(), "basic-auth-set", r); err != nil {
		t.Fatal(err)
	}
	wait(401, oldUser, oldPass)
	wait(200, r.Username, r.Password)
	if got := code(ports[0], "/.well-known/acme-challenge/test-token", "", ""); got != 200 {
		t.Fatal("rotation broke ACME", got)
	}
	if _, err = p.execute(context.Background(), "basic-auth-disable", r); err != nil {
		t.Fatal(err)
	}
	wait(200, "", "")
	view, _ := p.store.Read(r.Name)
	if view.Content != original {
		t.Fatal("original config was changed")
	}
	// Verify real -T output path detection using a harmless local wrapper.
	wrapper := filepath.Join(dir, "nginx-wrapper")
	script := fmt.Sprintf("#!/bin/sh\nexec '%s' -p '%s/' -c '%s' \"$@\"\n", binary, dir, config)
	if strings.ContainsAny(binary+dir+config, "'\n") {
		t.Fatal("fixture path cannot be shell quoted")
	}
	if err = os.WriteFile(wrapper, []byte(script), 0700); err != nil {
		t.Fatal(err)
	}
	mainPath, err := nginxMainConfig(context.Background(), wrapper)
	if err != nil || mainPath != config {
		t.Fatal("effective main config detection", mainPath, err)
	}
}
