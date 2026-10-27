package control

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"encoding/pem"
	"errors"
	"math/big"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/azpanel/lnmp-gopanel/internal/vhosts"
)

func renewControllerFixture(t *testing.T) (siteProvisioner, vhosts.RenewItem, string, string, string) {
	t.Helper()
	p, r, _ := provisionFixture(t)
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	leaf := &x509.Certificate{SerialNumber: big.NewInt(1), DNSNames: []string{r.Domain}, NotBefore: time.Now().Add(-time.Hour), NotAfter: time.Now().Add(90 * 24 * time.Hour)}
	der, err := x509.CreateCertificate(rand.Reader, leaf, leaf, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	oldCert := filepath.Join(p.store.Prefix, "old-cert.pem")
	newCert := filepath.Join(p.store.Prefix, "new-cert.pem")
	for _, path := range []string{oldCert, newCert} {
		if err = os.WriteFile(path, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}), 0600); err != nil {
			t.Fatal(err)
		}
	}
	p.issue = func(context.Context, vhosts.CreateRequest, string, func(string)) (string, string, error) {
		return oldCert, "/old/key.pem", nil
	}
	result, err := p.create(context.Background(), r, "/trusted/nginx", func(vhosts.CreateEvent) {})
	if err != nil {
		t.Fatal(err)
	}
	view, err := p.store.Read(result.Name)
	if err != nil {
		t.Fatal(err)
	}
	return p, vhosts.RenewItem{Name: result.Name, Provider: "zerossl", Email: "new@example.com"}, view.Content, oldCert, newCert
}
func TestRenewalDeploymentAndRecovery(t *testing.T) {
	for _, scenario := range []string{"success", "issue", "check", "reload", "register", "external", "timeout"} {
		t.Run(scenario, func(t *testing.T) {
			p, item, original, oldCert, newCert := renewControllerFixture(t)
			active := false
			restored := false
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			p.issue = func(_ context.Context, r vhosts.CreateRequest, _ string, emit func(string)) (string, string, error) {
				if r.SSL.Mode != "zerossl" || r.SSL.Email != item.Email {
					t.Fatal("lost provider or email")
				}
				data, _ := os.ReadFile(oldCert)
				if len(data) == 0 {
					t.Fatal("old certificate overwritten")
				}
				if scenario == "issue" {
					return "", "", errors.New("CA failed")
				}
				if scenario == "timeout" {
					cancel()
					return "", "", ctx.Err()
				}
				if scenario == "external" {
					if err := os.WriteFile(filepath.Join(p.store.Directory, item.Name), []byte("# administrator edit\nserver {}\n"), 0644); err != nil {
						t.Fatal(err)
					}
				}
				emit("new order signed")
				return newCert, "/new/key.pem", nil
			}
			reloadFailed := false
			p.run = func(_ context.Context, _ string, args ...string) (string, error) {
				view, err := p.store.Read(item.Name)
				if err != nil {
					return "", err
				}
				if strings.Contains(view.Content, newCert) {
					if args[0] == "-t" && scenario == "check" {
						return "invalid new config", errors.New("invalid")
					}
					if args[0] == "-s" && scenario == "reload" && !reloadFailed {
						reloadFailed = true
						return "reload failed", errors.New("reload")
					}
				}
				return "", nil
			}
			err := p.renew(ctx, "renew-test-123", item, "/trusted/nginx", func(vhosts.CreateEvent) {}, func() error {
				if scenario == "register" {
					return errors.New("cron write failed")
				}
				active = true
				return nil
			}, func() error { restored = true; active = false; return nil })
			view, _ := p.store.Read(item.Name)
			if scenario == "success" {
				if err != nil || !active || !strings.Contains(view.Content, newCert) {
					t.Fatal(view, err)
				}
				state, e := p.loadManaged(item.Name)
				if e != nil || state.Request.SSL.Mode != "zerossl" || state.Request.SSL.Email != item.Email {
					t.Fatal("managed record not updated", state, e)
				}
				base, _, _ := p.store.StripBasicAuth(item.Name, view.Content)
				if configHash([]byte(base)) != state.Hash {
					t.Fatal("record hash not updated")
				}
			} else {
				if err == nil || active {
					t.Fatal("failed renewal activated", err)
				}
				if scenario == "external" {
					if !strings.Contains(view.Content, "administrator edit") {
						t.Fatal("external edit overwritten")
					}
				} else if view.Content != original {
					t.Fatal("original configuration not restored", view.Content)
				}
				if scenario == "register" && !restored {
					t.Fatal("cron not restored")
				}
			}
		})
	}
}
