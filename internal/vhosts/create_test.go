package vhosts

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"encoding/pem"
	"errors"
	"github.com/azpanel/lnmp-gopanel/internal/logs"
	"math/big"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func createInput() CreateRequest {
	return CreateRequest{RequestID: "request-1234", Domain: "Example.COM.", PHP: "none", SSL: SSLInput{Mode: "none"}}
}
func TestCreateValidationAndEscapedConfig(t *testing.T) {
	if AccessLogDirectory != logs.Directory || !logs.ValidPath(AccessLogDirectory+"/site.log") {
		t.Fatal("default access logs are not readable by the log viewer")
	}
	r := createInput()
	r.Aliases = []string{"WWW.Example.COM", "example.com", "www.example.com"}
	r.Root = `/home/space "quoted"; folder`
	if err := r.Validate(); err != nil {
		t.Fatal(err)
	}
	if r.Domain != "example.com" || !reflect.DeepEqual(r.Aliases, []string{"www.example.com"}) || r.Name != "example.com.conf" {
		t.Fatal(r)
	}
	content := RenderSite(r, PHPRuntime{}, "", "")
	if !strings.Contains(content, `root "/home/space \"quoted\"; folder";`) || !strings.Contains(content, "location ^~ /.well-known/acme-challenge/") {
		t.Fatal(content)
	}
	r.AccessLog = true
	if content := RenderSite(r, PHPRuntime{}, "", ""); !strings.Contains(content, `access_log "/home/wwwlogs/example.com.log";`) {
		t.Fatal(content)
	}
	s := testStore(t)
	put(t, filepath.Join(s.Directory, r.Name), content)
	sites, err := s.List(context.Background())
	if err != nil || len(sites) != 1 || sites[0].Error != "" || sites[0].Roots[0].Configured != r.Root {
		t.Fatal(sites, err)
	}
	for _, domain := range []string{"https://example.com", "example.com:80", "*.example.com", "a;return 200;#.com", "-bad.example", "a\n.example", "127.0.0.1"} {
		bad := createInput()
		bad.Domain = domain
		if bad.Validate() == nil {
			t.Errorf("unsafe domain accepted: %q", domain)
		}
	}
	for _, path := range []string{"/", "relative", "/home/../etc", "/home/$host", "/home/a\nb", "/home/a\\b"} {
		bad := createInput()
		bad.Root = path
		if bad.Validate() == nil {
			t.Errorf("unsafe root accepted: %q", path)
		}
	}
	r.SSL = SSLInput{Mode: "files", CertFile: "/ssl/fullchain.pem", KeyFile: "/ssl/key.pem", Redirect: true}
	r.IPv6 = true
	content = RenderSite(r, PHPRuntime{}, r.SSL.CertFile, r.SSL.KeyFile)
	if !strings.Contains(content, "listen [::]:443 ssl;") || !strings.Contains(content, "location / { return 301 https://example.com$request_uri; }") || strings.Count(content, "location ^~ /.well-known/acme-challenge/") != 2 {
		t.Fatal(content)
	}
}

func TestInstallRollbackAndNoOverwrite(t *testing.T) {
	for _, failure := range []string{"none", "check", "reload"} {
		t.Run(failure, func(t *testing.T) {
			s := testStore(t)
			checks, reloads := 0, 0
			check := func(context.Context) error {
				checks++
				if failure == "check" {
					return errors.New("bad config")
				}
				return nil
			}
			reload := func(context.Context) error {
				reloads++
				if failure == "reload" && reloads == 1 {
					return errors.New("reload failed")
				}
				return nil
			}
			err := s.Install(context.Background(), "new.conf", []byte("server {}\n"), nil, check, reload)
			if failure == "none" {
				if err != nil || checks != 1 || reloads != 1 {
					t.Fatal(err, checks, reloads)
				}
				if err = s.Install(context.Background(), "new.conf", []byte("overwritten"), nil, check, reload); err == nil {
					t.Fatal("overwrote site")
				}
			} else {
				if err == nil {
					t.Fatal("failure ignored")
				}
				if _, err = os.Stat(filepath.Join(s.Directory, "new.conf")); !os.IsNotExist(err) {
					t.Fatal("failed config retained")
				}
				if failure == "check" && reloads != 0 {
					t.Fatal("reloaded invalid config")
				}
				if failure == "reload" && reloads != 2 {
					t.Fatal("original config was not reloaded")
				}
			}
		})
	}
	s := testStore(t)
	old := []byte("# original\nserver {}\n")
	put(t, filepath.Join(s.Directory, "old.conf"), string(old))
	ok := func(context.Context) error { return nil }
	fail := func(context.Context) error { return errors.New("invalid") }
	if s.Install(context.Background(), "old.conf", []byte("new"), old, fail, ok) == nil {
		t.Fatal("check failure ignored")
	}
	data, _ := os.ReadFile(filepath.Join(s.Directory, "old.conf"))
	if string(data) != string(old) {
		t.Fatal("original bytes not restored")
	}
	put(t, filepath.Join(s.Directory, "off.conf.disabled"), "disabled")
	if s.Install(context.Background(), "off.conf", []byte("new"), nil, ok, ok) == nil {
		t.Fatal("disabled collision ignored")
	}
}

func TestWildcardCertificateCoverage(t *testing.T) {
	key, _ := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	now := time.Now()
	template := &x509.Certificate{SerialNumber: big.NewInt(1), DNSNames: []string{"example.com", "*.example.com"}, NotBefore: now.Add(-time.Hour), NotAfter: now.Add(time.Hour)}
	der, err := x509.CreateCertificate(rand.Reader, template, template, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	c := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})
	kder, _ := x509.MarshalPKCS8PrivateKey(key)
	k := pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: kder})
	if _, err := CheckCertificate(c, k, []string{"example.com", "www.example.com"}, now); err != nil {
		t.Fatal(err)
	}
	for _, domain := range []string{"a.b.example.com", "other.example.net"} {
		if _, err := CheckCertificate(c, k, []string{domain}, now); err == nil {
			t.Fatal("wildcard overmatched", domain)
		}
	}
	if _, err := CheckCertificate(c, k, nil, now.Add(2*time.Hour)); err == nil {
		t.Fatal("expired accepted")
	}
	if _, err := CheckCertificate(c, []byte("invalid key"), nil, now); err == nil {
		t.Fatal("invalid key accepted")
	}
}

// Optional integration check: only nginx -t against an isolated prefix. Never
// reads the host nginx.conf, binds a port, or signals an existing nginx master.
func TestGeneratedNginxConfiguration(t *testing.T) {
	binary := os.Getenv("NGINX_TEST_BINARY")
	if binary == "" {
		t.Skip("NGINX_TEST_BINARY is not set")
	}
	dir := t.TempDir()
	key, _ := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	now := time.Now()
	template := &x509.Certificate{SerialNumber: big.NewInt(1), DNSNames: []string{"example.com"}, NotBefore: now.Add(-time.Hour), NotAfter: now.Add(time.Hour)}
	der, err := x509.CreateCertificate(rand.Reader, template, template, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	kder, _ := x509.MarshalPKCS8PrivateKey(key)
	certPath, keyPath := filepath.Join(dir, "cert.pem"), filepath.Join(dir, "key.pem")
	if err = os.WriteFile(certPath, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}), 0600); err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(keyPath, pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: kder}), 0600); err != nil {
		t.Fatal(err)
	}
	if err = os.Mkdir(filepath.Join(dir, "rewrite"), 0755); err != nil {
		t.Fatal(err)
	}
	put(t, filepath.Join(dir, "rewrite/wordpress.conf"), "location / { try_files $uri $uri/ /index.php?$args; }")
	put(t, filepath.Join(dir, "php.conf"), `location ~ \.php$ { return 204; }`)
	for _, mode := range []string{"http", "https", "redirect", "php"} {
		t.Run(mode, func(t *testing.T) {
			r := createInput()
			r.Root = filepath.Join(dir, "website with spaces")
			r.IPv6 = true
			if err := r.Validate(); err != nil {
				t.Fatal(err)
			}
			cert, key := "", ""
			php := PHPRuntime{}
			if mode != "http" {
				cert, key = certPath, keyPath
			}
			if mode == "redirect" {
				r.SSL.Redirect = true
			}
			if mode == "php" {
				r.PHP = "default"
				r.PathInfo = true
				r.Rewrite = "wordpress"
				php.PathInfoInclude = "php.conf"
			}
			put(t, filepath.Join(dir, "site.conf"), RenderSite(r, php, cert, key))
			main := filepath.Join(dir, "nginx.conf")
			put(t, main, "pid "+NginxQuote(filepath.Join(dir, "nginx.pid"))+";\nerror_log stderr;\nevents {}\nhttp { include site.conf; }\n")
			cmd := exec.Command(binary, "-t", "-e", "stderr", "-p", dir+"/", "-c", main)
			if out, err := cmd.CombinedOutput(); err != nil {
				t.Fatalf("nginx rejected generated %s configuration: %s (%v)", mode, out, err)
			}
		})
	}
}

func TestDNSChecksAllAddressesAndNormalizes(t *testing.T) {
	d := &DNSChecker{PublicIPs: []string{"2001:db8::1"}, Discover: func(context.Context) ([]string, []string) { return []string{"192.0.2.1"}, nil }, Lookup: func(_ context.Context, domain string) ([]net.IPAddr, error) {
		switch domain {
		case "all.example":
			return []net.IPAddr{{IP: net.ParseIP("192.0.2.1")}, {IP: net.ParseIP("2001:db8::1")}}, nil
		case "mixed.example":
			return []net.IPAddr{{IP: net.ParseIP("192.0.2.1")}, {IP: net.ParseIP("192.0.2.2")}}, nil
		case "missing.example":
			return nil, &net.DNSError{IsNotFound: true}
		default:
			return []net.IPAddr{{IP: net.ParseIP("192.0.2.3")}}, nil
		}
	}}
	out, err := d.Check(context.Background(), []string{"ALL.Example.", "mixed.example", "missing.example", "other.example"})
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"matched", "partial", "no_records", "mismatch"}
	for i, r := range out.Records {
		if r.Status != want[i] {
			t.Fatal(out)
		}
	}
	if _, err := d.Check(context.Background(), []string{"a;bad.example"}); err == nil {
		t.Fatal("injection accepted")
	}
}

type createBackend struct {
	calls   atomic.Int32
	release chan struct{}
}

func (*createBackend) VhostOptions(context.Context) (CreateOptions, error) {
	return CreateOptions{}, nil
}
func (b *createBackend) CreateVhost(ctx context.Context, r CreateRequest, emit func(CreateEvent)) (CreateResult, error) {
	b.calls.Add(1)
	emit(CreateEvent{Phase: "http", Line: r.Database.Password + " ready"})
	emit(CreateEvent{Line: "-----BEGIN PRIVATE KEY-----"})
	emit(CreateEvent{Line: "secret-key-body"})
	emit(CreateEvent{Line: "-----END PRIVATE KEY-----"})
	emit(CreateEvent{Line: "EAB hmac key: secret"})
	select {
	case <-b.release:
	case <-ctx.Done():
	}
	return CreateResult{Name: r.Name, SiteCreated: true, SSL: "failed"}, errors.New("issuance failed")
}
func TestJobsIdempotenceDisconnectAndRedaction(t *testing.T) {
	b := &createBackend{release: make(chan struct{})}
	m := NewJobs(context.Background(), b, nil)
	r := createInput()
	r.Database = DatabaseInput{Enabled: true, Name: "test", Password: "database-secret"}
	j, err := m.Start(r)
	if err != nil {
		t.Fatal(err)
	}
	if j.Status != "running" {
		t.Fatal(j)
	}
	if again, err := m.Start(r); err != nil || again.ID != j.ID {
		t.Fatal(again, err)
	}
	r.RequestID = "request-other"
	if _, err := m.Start(r); !errors.Is(err, ErrJobBusy) {
		t.Fatal("parallel creation accepted", err)
	}
	close(b.release)
	deadline := time.Now().Add(time.Second)
	for {
		j, _ = m.Get(j.ID)
		if j.Status != "running" {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("task stuck")
		}
		time.Sleep(time.Millisecond)
	}
	if j.Status != "partial" || b.calls.Load() != 1 || !j.Result.SiteCreated {
		t.Fatal(j)
	}
	for _, line := range j.Lines {
		if strings.Contains(line.Text, "database-secret") || strings.Contains(line.Text, "secret-key-body") || strings.Contains(line.Text, "hmac key") {
			t.Fatal("secret exposed", line)
		}
	}
	j.Lines[0].Text = "changed"
	copy, _ := m.Get(j.ID)
	if copy.Lines[0].Text == "changed" {
		t.Fatal("job aliases internal storage")
	}
}
