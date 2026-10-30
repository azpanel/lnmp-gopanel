//go:build linux

package control

import (
	"context"
	"database/sql"
	"errors"
	"os"
	"os/user"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"

	"github.com/azpanel/lnmp-gopanel/internal/vhosts"
)

func siteRootTestOwner(t *testing.T) (int, int) {
	t.Helper()
	if os.Geteuid() == 0 {
		if account, err := user.Lookup("www"); err == nil {
			uid, e1 := strconv.Atoi(account.Uid)
			gid, e2 := strconv.Atoi(account.Gid)
			if e1 == nil && e2 == nil {
				return uid, gid
			}
		}
	}
	return os.Geteuid(), os.Getegid()
}

func TestPrepareSiteRootRejectsOutsideBase(t *testing.T) {
	for _, path := range []string{"/etc/systemd/system/nginx.service.d", "/home/wwwroot", "/home/wwwroot-other/new", "/home/wwwroot/../outside"} {
		if p, err := prepareSiteRoot(path); err == nil || p != nil {
			t.Fatalf("unsafe root accepted: %q", path)
		}
	}
}

func TestSiteRootDoesNotFollowSymlinksOrChangeExistingDirectory(t *testing.T) {
	base := t.TempDir()
	target := filepath.Join(base, "existing")
	if err := os.Mkdir(target, 0700); err != nil {
		t.Fatal(err)
	}
	ini := filepath.Join(target, ".user.ini")
	if err := os.WriteFile(ini, []byte("existing=unchanged\n"), 0600); err != nil {
		t.Fatal(err)
	}
	before, _ := os.Stat(target)
	uid, gid := siteRootTestOwner(t)
	p, err := prepareSiteRootAt(base, "existing", uid, gid)
	if err != nil {
		t.Fatal(err)
	}
	defer p.Close()
	if err = p.Rollback(); err != nil {
		t.Fatal(err)
	}
	after, _ := os.Stat(target)
	data, _ := os.ReadFile(ini)
	iniInfo, _ := os.Stat(ini)
	if !os.SameFile(before, after) || before.Mode() != after.Mode() || before.Sys().(*syscall.Stat_t).Uid != after.Sys().(*syscall.Stat_t).Uid || before.Sys().(*syscall.Stat_t).Gid != after.Sys().(*syscall.Stat_t).Gid || string(data) != "existing=unchanged\n" || iniInfo.Mode().Perm() != 0600 {
		t.Fatal("existing directory or PHP settings changed")
	}
	if err = os.Symlink(target, filepath.Join(base, "link")); err != nil {
		t.Fatal(err)
	}
	for _, relative := range []string{"link", "link/child"} {
		if p, err := prepareSiteRootAt(base, relative, uid, gid); err == nil || p != nil {
			t.Fatal("symlink path followed", relative)
		}
	}
	if _, err = prepareSiteRootAt(filepath.Join(base, "link"), "child", uid, gid); err == nil {
		t.Fatal("symlink base accepted")
	}
	if _, err = os.Stat(filepath.Join(target, "child")); !os.IsNotExist(err) {
		t.Fatal("created directory through symlink")
	}
}

func TestSiteRootRollbackRemovesOnlyNewTree(t *testing.T) {
	base := t.TempDir()
	uid, gid := siteRootTestOwner(t)
	p, err := prepareSiteRootAt(base, "new/nested/site", uid, gid)
	if err != nil {
		t.Fatal(err)
	}
	defer p.Close()
	root := filepath.Join(base, "new/nested/site")
	info, _ := os.Stat(root)
	ini, _ := os.Stat(filepath.Join(root, ".user.ini"))
	if info.Mode().Perm() != 0755 || info.Sys().(*syscall.Stat_t).Uid != uint32(uid) || info.Sys().(*syscall.Stat_t).Gid != uint32(gid) || ini.Mode().Perm() != 0644 {
		t.Fatal("new site permissions incorrect")
	}
	if err = p.Rollback(); err != nil {
		t.Fatal(err)
	}
	entries, _ := os.ReadDir(base)
	if len(entries) != 0 {
		t.Fatal("new tree or staging directory retained", entries)
	}
}

func TestSiteRootPreparationFailureCleansIntermediateDirectories(t *testing.T) {
	base := t.TempDir()
	uid, gid := siteRootTestOwner(t)
	if p, err := prepareSiteRootAt(base, "new/"+strings.Repeat("x", 256), uid, gid); err == nil || p != nil {
		t.Fatal("invalid component accepted")
	}
	entries, err := os.ReadDir(base)
	if err != nil || len(entries) != 0 {
		t.Fatal("partial preparation left directories", entries, err)
	}
}

func TestSiteRootRollbackPreservesConcurrentChanges(t *testing.T) {
	for _, scenario := range []string{"new file", "modified ini", "replaced ini", "hardlinked ini", "replaced directory", "directory symlink", "writable parent"} {
		t.Run(scenario, func(t *testing.T) {
			base := t.TempDir()
			parent := filepath.Join(base, "parent")
			if err := os.Mkdir(parent, 0755); err != nil {
				t.Fatal(err)
			}
			if scenario == "writable parent" {
				if err := os.Chmod(parent, 0777); err != nil {
					t.Fatal(err)
				}
			}
			uid, gid := siteRootTestOwner(t)
			p, err := prepareSiteRootAt(base, "parent/site", uid, gid)
			if err != nil {
				t.Fatal(err)
			}
			defer p.Close()
			root := filepath.Join(parent, "site")
			ini := filepath.Join(root, ".user.ini")
			preserved := filepath.Join(root, "user.txt")
			switch scenario {
			case "new file":
				err = os.WriteFile(preserved, []byte("user data"), 0600)
			case "modified ini":
				preserved = ini
				err = os.WriteFile(ini, []byte("user data"), 0600)
			case "replaced ini":
				err = os.Rename(ini, filepath.Join(base, "original.ini"))
				if err == nil {
					preserved = ini
					err = os.WriteFile(ini, []byte("user data"), 0600)
				}
			case "hardlinked ini":
				preserved = filepath.Join(base, "linked.ini")
				err = os.Link(ini, preserved)
			case "replaced directory", "directory symlink":
				err = os.Rename(root, filepath.Join(base, "moved"))
				if err == nil && scenario == "directory symlink" {
					outside := t.TempDir()
					preserved = filepath.Join(outside, "user.txt")
					err = os.WriteFile(preserved, []byte("user data"), 0600)
					if err == nil {
						err = os.Symlink(outside, root)
					}
				} else if err == nil {
					err = os.Mkdir(root, 0755)
					if err == nil {
						err = os.WriteFile(preserved, []byte("user data"), 0600)
					}
				}
			}
			if err != nil {
				t.Fatal(err)
			}
			if err = p.Rollback(); err == nil || !strings.Contains(err.Error(), root) {
				t.Fatal("unsafe cleanup was not reported", err)
			}
			if scenario == "writable parent" {
				info, _ := os.Stat(root)
				parentInfo, _ := os.Stat(parent)
				if info.Mode().Perm() != 0700 || info.Sys().(*syscall.Stat_t).Uid != uint32(os.Geteuid()) || parentInfo.Mode().Perm() != 0777 {
					t.Fatal("new privileges not revoked or existing parent changed")
				}
			} else if data, err := os.ReadFile(preserved); err != nil || (scenario != "hardlinked ini" && string(data) != "user data") {
				t.Fatal("concurrent data lost", string(data), err)
			}
		})
	}
}

func TestProvisionDirectoryRollbackBoundary(t *testing.T) {
	for _, scenario := range []string{"check", "reload", "managed record", "uncertain reload", "concurrent data", "success", "ssl", "database", "existing root"} {
		t.Run(scenario, func(t *testing.T) {
			p, r, _ := provisionFixture(t)
			base := t.TempDir()
			uid, gid := siteRootTestOwner(t)
			root := filepath.Join(base, "new/nested/site")
			r.Root = "/home/wwwroot/new/nested/site"
			r.SSL.Mode = "none"
			p.prepareRoot = func(path string) (siteRootPreparation, error) {
				return prepareSiteRootAt(base, strings.TrimPrefix(path, vhosts.SiteRootDirectory+"/"), uid, gid)
			}
			if scenario == "existing root" {
				if err := os.MkdirAll(root, 0700); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(filepath.Join(root, ".user.ini"), []byte("user data"), 0600); err != nil {
					t.Fatal(err)
				}
			}
			if scenario == "managed record" {
				p.trust = func(path string) error {
					if path == p.stateDir {
						if _, err := os.Stat(root); err == nil {
							return errors.New("cannot save managed record")
						}
					}
					return nil
				}
			}
			reloads := 0
			p.run = func(_ context.Context, _ string, args ...string) (string, error) {
				if args[0] == "-t" && (scenario == "check" || scenario == "existing root" || scenario == "concurrent data") {
					if scenario == "concurrent data" {
						if err := os.WriteFile(filepath.Join(root, "user.txt"), []byte("user data"), 0600); err != nil {
							t.Fatal(err)
						}
					}
					return "", errors.New("invalid config")
				}
				if args[0] == "-s" {
					reloads++
					if scenario == "uncertain reload" || (scenario == "reload" && reloads == 1) {
						return "", errors.New("reload failed")
					}
				}
				return "", nil
			}
			if scenario == "ssl" {
				r.SSL.Mode = "letsencrypt"
				p.issue = func(context.Context, vhosts.CreateRequest, string, func(string)) (string, string, error) {
					return "", "", errors.New("ACME failed")
				}
			}
			if scenario == "database" {
				r.Database = vhosts.DatabaseInput{Enabled: true, Name: "site_db", Password: "secret-pass"}
				p.database = func(context.Context, string) (*sql.DB, error) {
					return sql.OpenDB(fixtureConnector{&sqlFixture{fail: "CREATE DATABASE"}}), nil
				}
			}
			result, err := p.create(context.Background(), r, "/trusted/nginx", func(vhosts.CreateEvent) {})
			if (err == nil) != (scenario == "success") {
				t.Fatal("unexpected result", result, err)
			}
			_, statErr := os.Stat(root)
			keep := scenario == "success" || scenario == "ssl" || scenario == "database" || scenario == "uncertain reload" || scenario == "concurrent data" || scenario == "existing root"
			if keep && statErr != nil || !keep && !os.IsNotExist(statErr) {
				t.Fatal("incorrect root cleanup", keep, statErr, err)
			}
			_, recordErr := os.Stat(filepath.Join(p.stateDir, "example.com.conf.json"))
			keepRecord := scenario == "success" || scenario == "ssl" || scenario == "database" || scenario == "uncertain reload"
			if keepRecord && recordErr != nil || !keepRecord && !os.IsNotExist(recordErr) {
				t.Fatal("incorrect record cleanup", recordErr, err)
			}
			if scenario == "uncertain reload" && !errors.Is(err, vhosts.ErrInstallUncertain) {
				t.Fatal("uncertain recovery not preserved", err)
			}
			if scenario == "concurrent data" && !strings.Contains(err.Error(), "清理失败") {
				t.Fatal("cleanup failure not returned", err)
			}
			if scenario == "existing root" {
				info, _ := os.Stat(root)
				data, _ := os.ReadFile(filepath.Join(root, ".user.ini"))
				if info.Mode().Perm() != 0700 || string(data) != "user data" {
					t.Fatal("existing root modified")
				}
			}
		})
	}
}
