package control

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/azpanel/lnmp-gopanel/internal/vhosts"
)

func authFixture(t *testing.T) (basicAuthController, vhosts.BasicAuthInput) {
	t.Helper()
	dir := t.TempDir()
	store := vhosts.Store{Prefix: dir, Directory: filepath.Join(dir, "conf", "vhost")}
	if err := os.MkdirAll(store.Directory, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(store.Directory, "site.conf"), []byte("# preserve\r\nserver { listen 80; root /test; }\r\n"), 0640); err != nil {
		t.Fatal(err)
	}
	p := basicAuthController{store: store, trust: func(string) error { return nil }, lock: func() (func(), error) { return func() {}, nil }, global: func(context.Context) (string, error) { return "http { server { auth_basic other; } }", nil }, hash: func(context.Context, string) (string, error) { return "$6$salt$" + strings.Repeat("a", 86), nil }, check: func(context.Context) error { return nil }, reload: func(context.Context) error { return nil }}
	return p, vhosts.BasicAuthInput{Name: "site.conf", Username: "user", Password: "test-password"}
}
func TestBasicAuthLifecycleAndRollback(t *testing.T) {
	ctx := context.Background()
	for _, failure := range []string{"none", "check", "reload"} {
		t.Run(failure, func(t *testing.T) {
			p, r := authFixture(t)
			before, _ := p.store.Read(r.Name)
			state, err := p.execute(ctx, "basic-auth-get", r)
			if err != nil || state.Status != "disabled" {
				t.Fatal(state, err)
			}
			state, err = p.execute(ctx, "basic-auth-set", r)
			if err != nil || state.Status != "enabled" || state.Username != r.Username {
				t.Fatal(state, err)
			}
			enabled, _ := p.store.Read(r.Name)
			_, oldFile, err := p.store.StripBasicAuth(r.Name, enabled.Content)
			if err != nil {
				t.Fatal(err)
			}
			oldBytes, _ := os.ReadFile(oldFile)
			info, _ := os.Stat(filepath.Join(p.store.Directory, r.Name))
			if info.Mode().Perm() != 0640 {
				t.Fatal("config permissions changed")
			}
			reloads := 0
			p.check = func(context.Context) error {
				if failure == "check" {
					return errors.New("invalid")
				}
				return nil
			}
			p.reload = func(context.Context) error {
				reloads++
				if failure == "reload" && reloads == 1 {
					return errors.New("reload failed")
				}
				return nil
			}
			r.Username = "replacement"
			state, err = p.execute(ctx, "basic-auth-set", r)
			view, _ := p.store.Read(r.Name)
			data, _ := os.ReadFile(oldFile)
			if string(data) != string(oldBytes) {
				t.Fatal("old credential overwritten")
			}
			if failure != "none" {
				if err == nil || view.Content != enabled.Content {
					t.Fatal("failed rotation not rolled back", err)
				}
				if failure == "check" && reloads != 0 {
					t.Fatal("invalid config reloaded")
				}
				if failure == "reload" && reloads != 2 {
					t.Fatal("old config not reloaded")
				}
				files, _ := os.ReadDir(p.store.BasicAuthDirectory())
				if len(files) != 1 {
					t.Fatal("failed credential not cleaned up")
				}
				// Failure disabling must preserve both original configuration and credentials.
				reloads = 0
				if _, err = p.execute(ctx, "basic-auth-disable", r); err == nil {
					t.Fatal("failure ignored")
				}
				view, _ = p.store.Read(r.Name)
				if view.Content != enabled.Content {
					t.Fatal("failed disable changed config")
				}
			} else if err != nil || state.Username != "replacement" {
				t.Fatal(state, err)
			}
			p.check = func(context.Context) error { return nil }
			p.reload = func(context.Context) error { return nil }
			state, err = p.execute(ctx, "basic-auth-disable", r)
			if err != nil || state.Status != "disabled" || state.Managed || state.Username != "" {
				t.Fatal(state, err)
			}
			view, _ = p.store.Read(r.Name)
			if view.Content != before.Content {
				t.Fatal("disable changed original bytes")
			}
			p.check = func(context.Context) error { t.Fatal("idempotent disable checked nginx"); return nil }
			if _, err = p.execute(ctx, "basic-auth-disable", r); err != nil {
				t.Fatal(err)
			}
		})
	}
}
func TestBasicAuthReadOnlyAndUnknown(t *testing.T) {
	ctx := context.Background()
	for _, test := range []string{"external", "global", "disabled", "unknown", "credential", "symlink", "unsafe"} {
		t.Run(test, func(t *testing.T) {
			p, r := authFixture(t)
			want := "unknown"
			switch test {
			case "external":
				os.WriteFile(filepath.Join(p.store.Directory, r.Name), []byte("server { auth_basic custom; }"), 0644)
				want = "external"
			case "global":
				p.global = func(context.Context) (string, error) { return "http { auth_basic inherited; server {} }", nil }
				want = "external"
			case "disabled":
				os.Rename(filepath.Join(p.store.Directory, r.Name), filepath.Join(p.store.Directory, r.Name+".disabled"))
				want = "disabled"
			case "unknown":
				p.global = func(context.Context) (string, error) { return "", errors.New("unavailable") }
			case "credential", "symlink":
				if _, err := p.execute(ctx, "basic-auth-set", r); err != nil {
					t.Fatal(err)
				}
				view, _ := p.store.Read(r.Name)
				_, file, _ := p.store.StripBasicAuth(r.Name, view.Content)
				if test == "credential" {
					os.Remove(file)
				} else {
					os.Remove(file)
					os.Symlink("/etc/passwd", file)
				}
			case "unsafe":
				p.trust = func(string) error { return errors.New("unsafe") }
			}
			state, err := p.execute(ctx, "basic-auth-get", r)
			if test == "unsafe" {
				if err == nil {
					t.Fatal("unsafe path accepted")
				}
				return
			}
			if err != nil || state.Status != want {
				t.Fatal(state, err)
			}
			if _, err = p.execute(ctx, "basic-auth-set", r); err == nil {
				t.Fatal("read only configuration modified")
			}
			if _, err = p.execute(ctx, "basic-auth-disable", r); err == nil {
				t.Fatal("read only configuration disabled")
			}
		})
	}
}
func TestBasicAuthConcurrentEditAndCancellation(t *testing.T) {
	p, r := authFixture(t)
	p.hash = func(context.Context, string) (string, error) {
		os.WriteFile(filepath.Join(p.store.Directory, r.Name), []byte("# external edit\nserver {}"), 0644)
		return "$6$salt$" + strings.Repeat("a", 86), nil
	}
	if _, err := p.execute(context.Background(), "basic-auth-set", r); err == nil {
		t.Fatal("external edit overwritten")
	}
	view, _ := p.store.Read(r.Name)
	if !strings.Contains(view.Content, "external edit") {
		t.Fatal("external edit lost")
	}
	files, _ := os.ReadDir(p.store.BasicAuthDirectory())
	if len(files) != 0 {
		t.Fatal("unused credential retained")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := p.execute(ctx, "basic-auth-set", r); err == nil {
		t.Fatal("cancelled mutation proceeded")
	}
}

func TestBasicAuthUncertainRollbackRetainsCredential(t *testing.T) {
	p, r := authFixture(t)
	p.check = func(context.Context) error {
		if err := os.WriteFile(filepath.Join(p.store.Directory, r.Name), []byte("# external configuration\nserver {}"), 0644); err != nil {
			t.Fatal(err)
		}
		return errors.New("check failed")
	}
	if _, err := p.execute(context.Background(), "basic-auth-set", r); !errors.Is(err, vhosts.ErrInstallUncertain) {
		t.Fatal("uncertain rollback not reported", err)
	}
	files, err := os.ReadDir(p.store.BasicAuthDirectory())
	if err != nil || len(files) != 1 {
		t.Fatal("credential potentially used by running workers removed", err)
	}
}
func TestBasicAuthRejectsSymlinkDirectory(t *testing.T) {
	p, r := authFixture(t)
	target := t.TempDir()
	if err := os.Symlink(target, p.store.BasicAuthDirectory()); err != nil {
		t.Fatal(err)
	}
	if _, err := p.execute(context.Background(), "basic-auth-set", r); err == nil {
		t.Fatal("symlink credential directory accepted")
	}
	entries, _ := os.ReadDir(target)
	if len(entries) != 0 {
		t.Fatal("wrote into symlink target")
	}
}
