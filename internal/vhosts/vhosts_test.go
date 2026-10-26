package vhosts

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"errors"
	"math/big"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

func testStore(t *testing.T) Store {
	t.Helper()
	prefix := t.TempDir()
	dir := filepath.Join(prefix, "conf", "vhost")
	if e := os.MkdirAll(dir, 0755); e != nil {
		t.Fatal(e)
	}
	return Store{Directory: dir, Prefix: prefix}
}
func put(t *testing.T, path, content string) {
	t.Helper()
	if e := os.WriteFile(path, []byte(content), 0644); e != nil {
		t.Fatal(e)
	}
}
func TestCatalogParsesQuotesCommentsIncludesAndPublicRoots(t *testing.T) {
	s := testStore(t)
	put(t, filepath.Join(s.Prefix, "conf", "common.conf"), `root "/home/project with spaces/public/";`)
	put(t, filepath.Join(s.Directory, "example.conf"), `# root /ignored;
server { listen 80; server_name example.com www.example.com;
 include common.conf; location / { proxy_pass http://127.0.0.1:5000; }
}`)
	put(t, filepath.Join(s.Directory, "off.conf.disabled"), `server { listen 443 ssl; server_name off.example; root $host/public; ssl_certificate missing.pem; }`)
	sites, e := s.List(context.Background())
	if e != nil || len(sites) != 2 {
		t.Fatal(sites, e)
	}
	site := sites[0]
	if !site.Enabled || site.Error != "" || !reflect.DeepEqual(site.Domains, []string{"example.com", "www.example.com"}) || len(site.Roots) != 1 {
		t.Fatalf("%+v", site)
	}
	root := site.Roots[0]
	if root.Path != "/home/project with spaces" || !root.PublicParent {
		t.Fatal(root)
	}
	if sites[1].Enabled || sites[1].Roots[0].Error == "" || sites[1].Certificates[0].Status != "unavailable" {
		t.Fatal(sites[1])
	}
	view, e := s.Read("example.conf")
	if e != nil || !strings.Contains(view.Content, "# root /ignored;") {
		t.Fatal("raw config must be preserved", e)
	}
}
func TestCatalogReverseProxyAndMalformedConfig(t *testing.T) {
	s := testStore(t)
	put(t, filepath.Join(s.Directory, "proxy.conf"), `map $http_upgrade $connection_upgrade { default upgrade; '' close; } server { listen 443 ssl; server_name ssh.example; location / { proxy_pass http://127.0.0.1:5000; } }`)
	put(t, filepath.Join(s.Directory, "bad.conf.disabled"), `server { root /missing;`)
	sites, e := s.List(context.Background())
	if e != nil || len(sites) != 2 {
		t.Fatal(sites, e)
	}
	if sites[0].Error == "" || len(sites[1].Roots) != 0 || len(sites[1].Proxies) != 1 {
		t.Fatal(sites)
	}
}
func TestCertificateValidity(t *testing.T) {
	s := testStore(t)
	now := time.Now().UTC().Truncate(time.Second)
	key, e := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if e != nil {
		t.Fatal(e)
	}
	for _, tc := range []struct {
		name, status string
		from, to     time.Time
	}{{"valid", "valid", now.Add(-time.Hour), now.Add(90 * 24 * time.Hour)}, {"soon", "expiring", now.Add(-time.Hour), now.Add(5 * 24 * time.Hour)}, {"old", "expired", now.Add(-5 * 24 * time.Hour), now.Add(-time.Hour)}, {"future", "not_yet_valid", now.Add(time.Hour), now.Add(90 * 24 * time.Hour)}} {
		t.Run(tc.name, func(t *testing.T) {
			template := &x509.Certificate{SerialNumber: big.NewInt(42), Subject: pkix.Name{CommonName: "example.com", Organization: []string{"Test Issuer"}}, DNSNames: []string{"example.com", "www.example.com"}, NotBefore: tc.from, NotAfter: tc.to}
			der, e := x509.CreateCertificate(rand.Reader, template, template, &key.PublicKey, key)
			if e != nil {
				t.Fatal(e)
			}
			path := filepath.Join(s.Prefix, tc.name+".pem")
			if e := os.WriteFile(path, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}), 0600); e != nil {
				t.Fatal(e)
			}
			c := certificate(path, now)
			if c.Status != tc.status || !strings.Contains(c.Issuer, "Test Issuer") || c.Serial != "2a" || len(c.Fingerprint) != 64 || len(c.DNSNames) != 2 {
				t.Fatalf("%+v", c)
			}
		})
	}
}
func TestNamesCollisionsAndSymlinks(t *testing.T) {
	s := testStore(t)
	for _, name := range []string{"../nginx.conf", "a/b.conf", "/tmp/x.conf", "a.conf.disabled", ".hidden.conf", "a\n.conf", "x"} {
		if ValidName(name) {
			t.Fatalf("unsafe name accepted: %q", name)
		}
	}
	put(t, filepath.Join(s.Directory, "a.conf"), "server {}\n")
	put(t, filepath.Join(s.Directory, "a.conf.disabled"), "do not replace")
	if _, _, e := s.file("a.conf"); e == nil {
		t.Fatal("collision accepted")
	}
	if e := os.Symlink(filepath.Join(s.Directory, "a.conf"), filepath.Join(s.Directory, "link.conf")); e != nil {
		t.Fatal(e)
	}
	if _, e := s.Read("link.conf"); e == nil {
		t.Fatal("symlink exposed")
	}
	if _, e := s.Read("../nginx.conf"); e == nil {
		t.Fatal("traversal exposed")
	}
}
func TestToggleSuccessAndIdempotency(t *testing.T) {
	s := testStore(t)
	content := "# keep bytes\r\nserver { listen 80; }\r\n"
	put(t, filepath.Join(s.Directory, "site.conf"), content)
	calls := []string{}
	check := func(context.Context) error {
		calls = append(calls, "check")
		if _, e := os.Stat(filepath.Join(s.Directory, "site.conf.disabled")); e != nil {
			t.Fatal("check ran before rename")
		}
		return nil
	}
	reload := func(context.Context) error { calls = append(calls, "reload"); return nil }
	r, e := s.Toggle(context.Background(), "site.conf", false, check, reload)
	if e != nil || !r.Changed || r.Enabled || !reflect.DeepEqual(calls, []string{"check", "reload"}) {
		t.Fatal(r, e, calls)
	}
	b, _ := os.ReadFile(filepath.Join(s.Directory, "site.conf.disabled"))
	if string(b) != content {
		t.Fatal("configuration was rewritten")
	}
	r, e = s.Toggle(context.Background(), "site.conf", false, check, reload)
	if e != nil || r.Changed || len(calls) != 2 {
		t.Fatal("idempotent toggle called nginx")
	}
	r, e = s.Toggle(context.Background(), "site.conf", true, func(context.Context) error { return nil }, reload)
	if e != nil || !r.Enabled {
		t.Fatal(r, e)
	}
}
func TestCheckFailureRestoresFilenameWithoutReload(t *testing.T) {
	s := testStore(t)
	put(t, filepath.Join(s.Directory, "site.conf.disabled"), "broken config")
	_, e := s.Toggle(context.Background(), "site.conf", true, func(context.Context) error { return errors.New("bad config") }, func(context.Context) error { t.Fatal("reload after failed check"); return nil })
	if e == nil || !strings.Contains(e.Error(), "已恢复") {
		t.Fatal(e)
	}
	if _, e := os.Stat(filepath.Join(s.Directory, "site.conf.disabled")); e != nil {
		t.Fatal(e)
	}
	if _, e := os.Stat(filepath.Join(s.Directory, "site.conf")); !os.IsNotExist(e) {
		t.Fatal("enabled file left behind")
	}
}
func TestReloadFailureRestoresOriginalAndReloadsAgain(t *testing.T) {
	s := testStore(t)
	put(t, filepath.Join(s.Directory, "site.conf"), "server {}\n")
	checks, reloads := 0, 0
	_, e := s.Toggle(context.Background(), "site.conf", false, func(context.Context) error { checks++; return nil }, func(context.Context) error {
		reloads++
		if reloads == 1 {
			return errors.New("reload failed")
		}
		return nil
	})
	if e == nil || checks != 2 || reloads != 2 {
		t.Fatal(e, checks, reloads)
	}
	if _, e := os.Stat(filepath.Join(s.Directory, "site.conf")); e != nil {
		t.Fatal(e)
	}
}
func TestRollbackNeverOverwritesCollision(t *testing.T) {
	s := testStore(t)
	put(t, filepath.Join(s.Directory, "site.conf"), "original")
	_, e := s.Toggle(context.Background(), "site.conf", false, func(context.Context) error {
		put(t, filepath.Join(s.Directory, "site.conf"), "concurrent change")
		return errors.New("bad")
	}, func(context.Context) error { return nil })
	if e == nil || !strings.Contains(e.Error(), "无法恢复") {
		t.Fatal(e)
	}
	b, _ := os.ReadFile(filepath.Join(s.Directory, "site.conf"))
	if string(b) != "concurrent change" {
		t.Fatal("rollback overwrote another file")
	}
}
func TestUsageUsesPublicParentAndDeduplicatesRoots(t *testing.T) {
	s := testStore(t)
	project := filepath.Join(s.Prefix, "project with spaces")
	if e := os.MkdirAll(filepath.Join(project, "public"), 0755); e != nil {
		t.Fatal(e)
	}
	put(t, filepath.Join(project, "outside-public.bin"), strings.Repeat("x", 8192))
	put(t, filepath.Join(s.Directory, "site.conf"), `server { root "`+project+`/public"; } server { root "`+project+`/public"; }`)
	uses := []Usage{}
	if e := s.Usage(context.Background(), func(u Usage) { uses = append(uses, u) }); e != nil {
		t.Fatal(e)
	}
	if len(uses) != 1 || uses[0].Path != project || uses[0].Bytes == nil || *uses[0].Bytes < 8192 {
		t.Fatal(uses)
	}
}
