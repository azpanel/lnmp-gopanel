package vhosts

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"encoding/pem"
	"errors"
	"fmt"
	"math/big"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

func renewalFixture(t *testing.T) (Store, string) {
	t.Helper()
	s := testStore(t)
	key, _ := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	cert := &x509.Certificate{SerialNumber: big.NewInt(1), DNSNames: []string{"example.com", "www.example.com"}, NotBefore: time.Now().Add(-time.Hour), NotAfter: time.Now().Add(90 * 24 * time.Hour)}
	der, err := x509.CreateCertificate(rand.Reader, cert, cert, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	keyDer, err := x509.MarshalPKCS8PrivateKey(key)
	if err != nil {
		t.Fatal(err)
	}
	put(t, filepath.Join(s.Prefix, "key.pem"), string(pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: keyDer})))
	path := filepath.Join(s.Prefix, "cert.pem")
	put(t, path, string(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})))
	content := "# keep this comment\nserver { listen 80; server_name example.com www.example.com; return 301 https://$host$request_uri; }\nserver { listen 443 ssl; server_name example.com www.example.com; ssl_certificate " + NginxQuote(path) + "; ssl_certificate_key /old/key.pem; root /home/wwwroot/custom; location / { proxy_pass http://127.0.0.1:3000; } }\n"
	put(t, filepath.Join(s.Directory, "site.conf"), content)
	return s, content
}
func TestRenewalPreservesConfigAndAuthentication(t *testing.T) {
	s, original := renewalFixture(t)
	credential := authTestPath(s, "site.conf")
	protected, err := s.AttachBasicAuth("site.conf", original, credential)
	if err != nil {
		t.Fatal(err)
	}
	put(t, filepath.Join(s.Directory, "site.conf"), protected)
	plan, err := s.PlanRenewal("site.conf", func(string) error { return nil })
	if err != nil {
		t.Fatal(err)
	}
	newCert := filepath.Join(s.Prefix, "new-cert.pem")
	certData, _ := os.ReadFile(plan.CertPath)
	put(t, newCert, string(certData))
	content, base, err := s.RenewalContent(plan, newCert, "/new/key.pem")
	if err != nil {
		t.Fatal(err)
	}
	for _, part := range []string{"# keep this comment", "root /home/wwwroot/custom;", "proxy_pass http://127.0.0.1:3000;", "ssl_certificate " + NginxQuote(newCert) + ";", "location / {\n        return 301 https://$host$request_uri;\n    }", "auth_basic off;", ChallengeDirectory} {
		if !strings.Contains(content, part) {
			t.Fatalf("missing %q in %s", part, content)
		}
	}
	stripped, auth, err := s.StripBasicAuth("site.conf", content)
	if err != nil || stripped != base || auth != credential {
		t.Fatal("authentication markers broken", err)
	}
	put(t, filepath.Join(s.Directory, "site.conf"), content)
	if _, err = s.PlanRenewal("site.conf", func(string) error { return nil }); err != nil {
		t.Fatal("cannot renew a second time", err)
	}
}

func TestRenewalPreservesIndentationAndIsStable(t *testing.T) {
	for _, tc := range []struct {
		name, header, indent, closing, newline string
		legacyChallenge                        bool
	}{
		{"spaces", "server {", "    ", "}", "\n", false},
		{"opening comment", "server { # keep this server comment", "    ", "}", "\n", false},
		{"lnmp", "server\n    {", "        ", "    }", "\n", false},
		{"legacy challenge", "server\n    {", "        ", "    }", "\n", true},
		{"tabs", "server {", "\t", "}", "\n", false},
		{"crlf", "server\r\n    {", "        ", "    }", "\r\n", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s, original := renewalFixture(t)
			oldHTTP := "server { listen 80; server_name example.com www.example.com; return 301 https://$host$request_uri; }\n"
			prefix := tc.header + tc.newline
			tail := strings.Join([]string{
				tc.indent + "# preserve this comment and the original spacing",
				tc.indent + "listen   80;",
				tc.indent + "server_name example.com www.example.com;",
			}, tc.newline) + tc.newline
			redirect := tc.indent + "return 301 https://$host$request_uri; # keep this too" + tc.newline
			legacy := ""
			if tc.legacyChallenge {
				legacy = "    location ^~ /.well-known/acme-challenge/ { alias " + NginxQuote(ChallengeDirectory+"/.well-known/acme-challenge/") + "; default_type text/plain; auth_basic off; allow all; }" + tc.newline
			}
			http := prefix + legacy + tail + redirect + tc.closing + tc.newline
			original = strings.Replace(original, oldHTTP, http, 1)
			put(t, filepath.Join(s.Directory, "site.conf"), original)
			plan, err := s.PlanRenewal("site.conf", func(string) error { return nil })
			if err != nil {
				t.Fatal(err)
			}
			step := "    "
			if tc.indent == "\t" {
				step = "\t"
			}
			challenge := strings.Join([]string{
				tc.indent + "location ^~ /.well-known/acme-challenge/ {",
				tc.indent + step + "alias " + NginxQuote(ChallengeDirectory+"/.well-known/acme-challenge/") + ";",
				tc.indent + step + "default_type text/plain;",
				tc.indent + step + "auth_basic off;",
				tc.indent + step + "allow all;",
				tc.indent + "}",
			}, tc.newline) + tc.newline
			redirect = strings.Join([]string{
				tc.indent + "location / {",
				tc.indent + step + "return 301 https://$host$request_uri;",
				tc.indent + "} # keep this too",
			}, tc.newline) + tc.newline
			want := strings.Replace(original, http, prefix+challenge+tail+redirect+tc.closing+tc.newline, 1)
			if plan.Base != want {
				t.Fatalf("unrelated content or indentation changed:\nwant:\n%s\ngot:\n%s", want, plan.Base)
			}
			put(t, filepath.Join(s.Directory, "site.conf"), plan.Base)
			again, err := s.PlanRenewal("site.conf", func(string) error { return nil })
			if err != nil || again.Base != plan.Base {
				t.Fatalf("repeated renewal changed formatting: %v\n%s", err, again.Base)
			}
		})
	}
}

func TestRenewalRejectsUnsupportedConfigurations(t *testing.T) {
	cases := []struct {
		name   string
		change func(string) string
	}{
		{"wildcard", func(s string) string { return strings.ReplaceAll(s, "www.example.com", "*.example.com") }},
		{"dynamic certificate", func(s string) string { return strings.Replace(s, "/old/key.pem", "/$host/key.pem", 1) }},
		{"server rewrite", func(s string) string {
			return strings.Replace(s, "listen 80;", "listen 80; rewrite ^ /index.php last;", 1)
		}},
		{"custom challenge", func(s string) string {
			return strings.Replace(s, "listen 80;", "listen 80; location ^~ /.well-known/acme-challenge/ { proxy_pass http://other; }", 1)
		}},
		{"multiple pairs", func(s string) string {
			return strings.Replace(s, "ssl_certificate_key /old/key.pem;", "ssl_certificate_key /old/key.pem; ssl_certificate /second.pem; ssl_certificate_key /second.key;", 1)
		}},
		{"shared include", func(s string) string { return strings.Replace(s, "listen 80;", "listen 80; include shared.conf;", 1) }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			s, content := renewalFixture(t)
			put(t, filepath.Join(s.Prefix, "conf/shared.conf"), "ssl_certificate /shared.pem;")
			put(t, filepath.Join(s.Directory, "site.conf"), tc.change(content))
			if _, err := s.PlanRenewal("site.conf", func(string) error { return nil }); err == nil {
				t.Fatal("accepted unsupported config")
			}
		})
	}
	s, _ := renewalFixture(t)
	if err := os.Rename(filepath.Join(s.Directory, "site.conf"), filepath.Join(s.Directory, "site.conf.disabled")); err != nil {
		t.Fatal(err)
	}
	if _, err := s.PlanRenewal("site.conf", func(string) error { return nil }); err == nil {
		t.Fatal("accepted disabled site")
	}
}

type renewalBackend struct {
	createBackend
	mu      sync.Mutex
	order   []string
	entered chan string
	release chan struct{}
}

func (*renewalBackend) PreviewRenewal(context.Context, []string) ([]RenewPreview, error) {
	return []RenewPreview{}, nil
}
func (b *renewalBackend) RenewVhost(ctx context.Context, _ string, item RenewItem, emit func(CreateEvent)) error {
	b.mu.Lock()
	b.order = append(b.order, item.Name)
	b.mu.Unlock()
	emit(CreateEvent{Phase: "ssl", Line: "-----BEGIN PRIVATE KEY-----"})
	emit(CreateEvent{Line: "private-body"})
	emit(CreateEvent{Line: "-----END PRIVATE KEY-----"})
	b.entered <- item.Name
	select {
	case <-b.release:
	case <-ctx.Done():
		return ctx.Err()
	}
	if item.Name == "first.conf" {
		return errors.New("CA refused first")
	}
	emit(CreateEvent{Phase: "deploy", Line: "deployed"})
	return nil
}
func TestRenewalJobsSerializeContinueAndClone(t *testing.T) {
	backend := &renewalBackend{entered: make(chan string, 2), release: make(chan struct{})}
	jobs := NewJobs(context.Background(), backend, nil)
	r := RenewRequest{RequestID: "renew-test-123", Items: []RenewItem{{Name: "first.conf", Provider: "zerossl", Email: "a@example.com"}, {Name: "second.conf", Provider: "letsencrypt", Email: "b@example.com"}}}
	job, err := jobs.StartRenewal(r)
	if err != nil {
		t.Fatal(err)
	}
	<-backend.entered
	select {
	case <-backend.entered:
		t.Fatal("parallel issuance")
	default:
	}
	if _, err := jobs.Start(createInput()); !errors.Is(err, ErrJobBusy) {
		t.Fatal("creation not serialized", err)
	}
	if again, err := jobs.StartRenewal(r); err != nil || again.ID != job.ID {
		t.Fatal("not idempotent", err)
	}
	r.RequestID = "different-renew"
	if _, err := jobs.StartRenewal(r); !errors.Is(err, ErrJobBusy) {
		t.Fatal("batch not serialized", err)
	}
	close(backend.release)
	deadline := time.Now().Add(time.Second)
	for {
		job, _ = jobs.Get(job.ID)
		if job.Status != "running" {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("task stuck")
		}
		time.Sleep(time.Millisecond)
	}
	if job.Status != "partial" || job.Completed != 2 || job.Succeeded != 1 || job.Failed != 1 || job.Items[0].Status != "failed" || job.Items[1].Status != "succeeded" {
		t.Fatalf("%+v", job)
	}
	for i, line := range job.Lines {
		if strings.Contains(line.Text, "private-body") || line.Name == "" || line.ID != uint64(i+1) {
			t.Fatal("invalid log", line)
		}
	}
	job.Items[0].Status = "changed"
	copy, _ := jobs.Get(job.ID)
	if copy.Items[0].Status == "changed" {
		t.Fatal("items alias state")
	}
	listed := jobs.List()
	listed[0].Items[0].Status = "changed"
	copy, _ = jobs.Get(job.ID)
	if copy.Items[0].Status == "changed" {
		t.Fatal("list aliases state")
	}
}
func TestRenewalValidation(t *testing.T) {
	for _, r := range []RenewRequest{{RequestID: "valid-id-123"}, {RequestID: "valid-id-123", Items: []RenewItem{{Name: "../site.conf", Provider: "zerossl", Email: "a@example.com"}}}, {RequestID: "valid-id-123", Items: []RenewItem{{Name: "site.conf", Provider: "other", Email: "a@example.com"}}}, {RequestID: "valid-id-123", Items: []RenewItem{{Name: "site.conf", Provider: "zerossl", Email: "bad"}}}} {
		if r.Validate() == nil {
			t.Fatal("invalid request accepted", r)
		}
	}
}

func TestRenewedConfigurationAcceptedByNginx(t *testing.T) {
	binary := os.Getenv("NGINX_TEST_BINARY")
	if binary == "" {
		t.Skip("NGINX_TEST_BINARY is not set")
	}
	for _, protected := range []bool{false, true} {
		t.Run(fmt.Sprint(protected), func(t *testing.T) {
			s, original := renewalFixture(t)
			if protected {
				credential := authTestPath(s, "site.conf")
				var err error
				original, err = s.AttachBasicAuth("site.conf", original, credential)
				if err != nil {
					t.Fatal(err)
				}
				put(t, filepath.Join(s.Directory, "site.conf"), original)
			}
			plan, err := s.PlanRenewal("site.conf", func(string) error { return nil })
			if err != nil {
				t.Fatal(err)
			}
			content, base, err := s.RenewalContent(plan, plan.CertPath, filepath.Join(s.Prefix, "key.pem"))
			if err != nil {
				t.Fatal(err)
			}
			if conflict, err := s.InspectAuthDirectives(context.Background(), base, false, func(string) (string, error) { return "", nil }); err != nil || conflict != "" {
				t.Fatal("renewal broke authentication management", conflict, err)
			}
			put(t, filepath.Join(s.Directory, "site.conf"), content)
			main := filepath.Join(s.Prefix, "nginx.conf")
			put(t, main, "pid "+NginxQuote(filepath.Join(s.Prefix, "nginx.pid"))+"; error_log stderr; events {} http { access_log off; include "+NginxQuote(filepath.Join(s.Directory, "site.conf"))+"; }")
			if out, err := exec.Command(binary, "-t", "-e", "stderr", "-p", s.Prefix+"/", "-c", main).CombinedOutput(); err != nil {
				t.Fatal(string(out), err)
			}
		})
	}
}

func TestRenewalChallengeAuthExemptionIsNarrow(t *testing.T) {
	s := testStore(t)
	for _, content := range []string{
		"server {" + strings.Replace(challengeLocation("    "), "auth_basic off;", "auth_basic off; satisfy any;", 1) + "}",
		"server {" + strings.Replace(challengeLocation("    "), ChallengeDirectory, "/website", 1) + "}",
		"server { location / { auth_basic off; } }",
	} {
		if conflict, err := s.InspectAuthDirectives(context.Background(), content, false, func(string) (string, error) { return "", nil }); err == nil && conflict == "" {
			t.Fatal("unmanaged auth bypass accepted", content)
		}
	}
}

func TestRenewalDenseServerWithReturnFirst(t *testing.T) {
	s, original := renewalFixture(t)
	original = strings.Replace(original, "server { listen 80; server_name example.com www.example.com; return 301 https://$host$request_uri; }", "server{return 301 https://$host$request_uri;listen 80;server_name example.com www.example.com;}", 1)
	put(t, filepath.Join(s.Directory, "site.conf"), original)
	plan, err := s.PlanRenewal("site.conf", func(string) error { return nil })
	if err != nil {
		t.Fatal(err)
	}
	content, _, err := s.RenewalContent(plan, "", "")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := ParseAuthConfig(content); err != nil {
		t.Fatal("overlapping insert and replacement corrupted config", err)
	}
	if !strings.Contains(content, "location / {\n        return 301 https://$host$request_uri;\n    }") {
		t.Fatal("redirect changed", content)
	}
}
