package control

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/azpanel/lnmp-gopanel/internal/vhosts"
)

func provisionFixture(t *testing.T) (siteProvisioner, vhosts.CreateRequest, *bool) {
	t.Helper()
	dir := t.TempDir()
	vhostDir := filepath.Join(dir, "vhost")
	if err := os.Mkdir(vhostDir, 0755); err != nil {
		t.Fatal(err)
	}
	root := filepath.Join(dir, "website")
	if err := os.Mkdir(root, 0750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, ".user.ini"), []byte("existing=unchanged\n"), 0600); err != nil {
		t.Fatal(err)
	}
	locked := false
	p := siteProvisioner{store: vhosts.Store{Directory: vhostDir, Prefix: dir}, stateDir: filepath.Join(dir, "state"), trust: func(string) error { return nil }, options: func(context.Context) (vhosts.CreateOptions, error) {
		return vhosts.CreateOptions{Rewrites: []string{"none"}, ACMEAvailable: true}, nil
	}, run: func(context.Context, string, ...string) (string, error) { return "", nil }, lock: func() (func(), error) {
		if locked {
			t.Fatal("nested nginx lock")
		}
		locked = true
		return func() { locked = false }, nil
	}}
	r := vhosts.CreateRequest{RequestID: "request-1234", Domain: "example.com", PHP: "none", Root: root, SSL: vhosts.SSLInput{Mode: "letsencrypt", Email: "admin@example.com", Redirect: true}}
	return p, r, &locked
}
func TestProvisionKeepsHTTPOnSSLFailureAndRetriesWithoutRecreating(t *testing.T) {
	p, r, locked := provisionFixture(t)
	calls := 0
	p.issue = func(_ context.Context, _ vhosts.CreateRequest, _ string, emit func(string)) (string, string, error) {
		if *locked {
			t.Fatal("nginx locked during issuance")
		}
		calls++
		emit("validating domain")
		return "", "", errors.New("ACME validation failed")
	}
	result, err := p.create(context.Background(), r, "/trusted/nginx", func(vhosts.CreateEvent) {})
	if err == nil || !result.SiteCreated || result.SSL != "failed" || *locked {
		t.Fatal(result, err)
	}
	view, err := p.store.Read("example.com.conf")
	if err != nil || strings.Contains(view.Content, "listen 443") {
		t.Fatal(view, err)
	}
	content, _ := os.ReadFile(filepath.Join(r.Root, ".user.ini"))
	if string(content) != "existing=unchanged\n" {
		t.Fatal("existing PHP settings overwritten")
	}
	p.issue = func(_ context.Context, _ vhosts.CreateRequest, _ string, emit func(string)) (string, string, error) {
		if *locked {
			t.Fatal("nginx locked during retry")
		}
		calls++
		return "/test/fullchain.pem", "/test/key.pem", nil
	}
	retry := vhosts.CreateRequest{RequestID: "request-retry", Name: "example.com.conf", Retry: true, SSL: r.SSL}
	result, err = p.create(context.Background(), retry, "/trusted/nginx", func(vhosts.CreateEvent) {})
	if err != nil || result.SSL != "enabled" || calls != 2 {
		t.Fatal(result, err, calls)
	}
	view, err = p.store.Read(result.Name)
	if err != nil || !strings.Contains(view.Content, "listen 443 ssl;") {
		t.Fatal(view, err)
	}
	// External administrator edits make further automatic retries unsafe.
	if err = os.WriteFile(filepath.Join(p.store.Directory, result.Name), []byte(view.Content+"# edited\n"), 0644); err != nil {
		t.Fatal(err)
	}
	if _, err = p.create(context.Background(), retry, "/trusted/nginx", func(vhosts.CreateEvent) {}); err == nil {
		t.Fatal("modified configuration replaced")
	}
}
func TestProvisionNginxFailureAndCollision(t *testing.T) {
	for _, reason := range []string{"check", "reload", "duplicate"} {
		t.Run(reason, func(t *testing.T) {
			p, r, _ := provisionFixture(t)
			r.SSL.Mode = "none"
			reloads := 0
			p.run = func(_ context.Context, _ string, args ...string) (string, error) {
				if args[0] == "-t" && reason == "check" {
					return "invalid", errors.New("invalid")
				}
				if args[0] == "-s" {
					reloads++
					if reason == "reload" && reloads == 1 {
						return "", errors.New("reload failed")
					}
				}
				return "", nil
			}
			if reason == "duplicate" {
				if err := os.WriteFile(filepath.Join(p.store.Directory, "other.conf"), []byte("server { server_name example.com; }"), 0644); err != nil {
					t.Fatal(err)
				}
			}
			result, err := p.create(context.Background(), r, "/trusted/nginx", func(vhosts.CreateEvent) {})
			if err == nil || result.SiteCreated {
				t.Fatal(result, err)
			}
			if _, err = os.Stat(filepath.Join(p.store.Directory, "example.com.conf")); !os.IsNotExist(err) {
				t.Fatal("failed config retained")
			}
			if _, err = os.Stat(filepath.Join(p.stateDir, "example.com.conf.json")); !os.IsNotExist(err) {
				t.Fatal("failed ownership record retained")
			}
		})
	}
}

type sqlFixture struct {
	queries []string
	fail    string
	count   int64
}
type fixtureConnector struct{ f *sqlFixture }

func (c fixtureConnector) Connect(context.Context) (driver.Conn, error) { return fixtureConn{c.f}, nil }
func (c fixtureConnector) Driver() driver.Driver                        { return fixtureDriver{c.f} }

type fixtureDriver struct{ f *sqlFixture }

func (d fixtureDriver) Open(string) (driver.Conn, error) { return fixtureConn{d.f}, nil }

type fixtureConn struct{ f *sqlFixture }

func (c fixtureConn) Prepare(string) (driver.Stmt, error) {
	return nil, errors.New("prepare not expected")
}
func (c fixtureConn) Close() error { return nil }
func (c fixtureConn) Begin() (driver.Tx, error) {
	return nil, errors.New("DDL does not use transactions")
}
func (c fixtureConn) ExecContext(_ context.Context, q string, _ []driver.NamedValue) (driver.Result, error) {
	c.f.queries = append(c.f.queries, q)
	if c.f.fail != "" && strings.HasPrefix(q, c.f.fail) {
		return nil, errors.New("simulated database failure")
	}
	return driver.RowsAffected(1), nil
}
func (c fixtureConn) QueryContext(_ context.Context, q string, _ []driver.NamedValue) (driver.Rows, error) {
	if strings.HasPrefix(q, "SHOW GLOBAL VARIABLES") {
		return &fixtureRows{done: true}, nil
	}
	return &fixtureRows{count: c.f.count}, nil
}

type fixtureRows struct {
	count int64
	done  bool
}

func (*fixtureRows) Columns() []string { return []string{"count"} }
func (*fixtureRows) Close() error      { return nil }
func (r *fixtureRows) Next(values []driver.Value) error {
	if r.done {
		return io.EOF
	}
	r.done = true
	values[0] = r.count
	return nil
}

func TestDatabaseCreationCleansOnlyNewResources(t *testing.T) {
	for _, failure := range []string{"", "CREATE USER 'site'@'127.0.0.1'", "CREATE DATABASE"} {
		t.Run(failure, func(t *testing.T) {
			f := &sqlFixture{fail: failure}
			db := sql.OpenDB(fixtureConnector{f})
			defer db.Close()
			err := createSiteDatabase(context.Background(), db, vhosts.DatabaseInput{Name: "site", Password: "complex'password\\"})
			joined := strings.Join(f.queries, "\n")
			if failure == "" {
				if err != nil || strings.Contains(joined, "DROP") {
					t.Fatal(err, joined)
				}
			} else {
				if err == nil {
					t.Fatal("failure ignored")
				}
				if failure == "CREATE DATABASE" && strings.Contains(joined, "DROP") {
					t.Fatal("removed database not created by this task")
				}
				if strings.Contains(failure, "127.0.0.1") && (!strings.Contains(joined, "DROP USER 'site'@'localhost'") || !strings.Contains(joined, "DROP DATABASE `site`")) {
					t.Fatal("partial resources not cleaned", joined)
				}
			}
			if strings.Contains(joined, "complex'password") {
				t.Fatal("password concatenated into SQL")
			}
		})
	}
	f := &sqlFixture{count: 1}
	db := sql.OpenDB(fixtureConnector{f})
	defer db.Close()
	if createSiteDatabase(context.Background(), db, vhosts.DatabaseInput{Name: "existing"}) == nil || len(f.queries) > 0 {
		t.Fatal("existing database changed")
	}
}
func TestMySQLConfigurationIsOnlyData(t *testing.T) {
	opts := mysqlClientOptions([]byte("[client]\nuser=root\npassword='a\\\\b\\'c'\nsocket=/run/mysql.sock\ninit-command=DROP DATABASE data\n[mysqld]\npassword=ignored\n"))
	if opts["password"] != "a\\b'c" || opts["socket"] != "/run/mysql.sock" || opts["init-command"] != "" {
		t.Fatal("unexpected option parsing")
	}
}

func TestSiteRootDoesNotFollowSymlinksOrChangeExistingDirectory(t *testing.T) {
	base := t.TempDir()
	target := filepath.Join(base, "existing")
	if err := os.Mkdir(target, 0700); err != nil {
		t.Fatal(err)
	}
	if err := prepareSiteRoot(target); err != nil {
		t.Fatal(err)
	}
	info, _ := os.Stat(target)
	if info.Mode().Perm() != 0700 {
		t.Fatal("existing permissions changed")
	}
	link := filepath.Join(base, "link")
	if err := os.Symlink(target, link); err != nil {
		t.Fatal(err)
	}
	if prepareSiteRoot(link) == nil || prepareSiteRoot(filepath.Join(link, "child")) == nil {
		t.Fatal("symlink path followed")
	}
	if _, err := os.Stat(filepath.Join(target, "child")); !os.IsNotExist(err) {
		t.Fatal("created directory through symlink")
	}
}

func TestDatabaseGrantEscapesUnderscores(t *testing.T) {
	f := &sqlFixture{}
	db := sql.OpenDB(fixtureConnector{f})
	defer db.Close()
	if err := createSiteDatabase(context.Background(), db, vhosts.DatabaseInput{Name: "site_db", Password: "secret-pass"}); err != nil {
		t.Fatal(err)
	}
	for _, q := range f.queries {
		if strings.HasPrefix(q, "GRANT") && !strings.Contains(q, "`site\\_db`.*") {
			t.Fatal("database grant could match other databases", q)
		}
	}
}
