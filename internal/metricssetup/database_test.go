package metricssetup

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"io"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/azpanel/lnmp-gopanel/internal/config"
)

type accountStatement struct {
	query string
	args  []driver.NamedValue
}
type accountFixture struct {
	statements []accountStatement
	fail       bool
}
type accountConnector struct{ fixture *accountFixture }

func (c accountConnector) Connect(context.Context) (driver.Conn, error) {
	return &accountConnection{fixture: c.fixture}, nil
}
func (accountConnector) Driver() driver.Driver { return accountDriver{} }

type accountDriver struct{}

func (accountDriver) Open(string) (driver.Conn, error) { return nil, errors.New("unexpected Open") }

type accountConnection struct{ fixture *accountFixture }

func (*accountConnection) Prepare(string) (driver.Stmt, error) {
	return nil, errors.New("unexpected prepare")
}
func (*accountConnection) Close() error { return nil }
func (*accountConnection) Begin() (driver.Tx, error) {
	return nil, errors.New("unexpected transaction")
}
func (c *accountConnection) ExecContext(_ context.Context, query string, args []driver.NamedValue) (driver.Result, error) {
	c.fixture.statements = append(c.fixture.statements, accountStatement{query, args})
	if c.fixture.fail {
		return nil, errors.New("private driver error")
	}
	return driver.RowsAffected(1), nil
}
func (c *accountConnection) QueryContext(_ context.Context, query string, args []driver.NamedValue) (driver.Rows, error) {
	c.fixture.statements = append(c.fixture.statements, accountStatement{query, args})
	if c.fixture.fail {
		return nil, errors.New("private driver error")
	}
	return &accountRows{}, nil
}

type accountRows struct{ done bool }

func (*accountRows) Columns() []string { return []string{"count"} }
func (*accountRows) Close() error      { return nil }
func (r *accountRows) Next(values []driver.Value) error {
	if r.done {
		return io.EOF
	}
	r.done = true
	values[0] = int64(0)
	return nil
}
func TestNewAccountsOnlyReceiveImplicitUsageAndBoundCredentials(t *testing.T) {
	fixture := &accountFixture{}
	a := &databaseAdmin{db: sql.OpenDB(accountConnector{fixture})}
	defer a.Close()
	ctx := context.Background()
	password := "quote'\\#秘密"
	exists, err := a.Exists(ctx, "panel_monitor", "localhost")
	if err != nil || exists {
		t.Fatal(exists, err)
	}
	if err = a.Create(ctx, "panel_monitor", "localhost", password); err != nil {
		t.Fatal(err)
	}
	if err = a.Drop(ctx, "panel_monitor", "localhost"); err != nil {
		t.Fatal(err)
	}
	if len(fixture.statements) != 3 {
		t.Fatal("unexpected SQL")
	}
	creation := fixture.statements[1]
	if creation.query != "CREATE USER ?@? IDENTIFIED BY ?" || len(creation.args) != 3 || creation.args[2].Value != password {
		t.Fatal("account credentials not bound")
	}
	for _, statement := range fixture.statements {
		if strings.Contains(statement.query, password) || strings.Contains(statement.query, "GRANT") || strings.Contains(statement.query, "FLUSH") {
			t.Fatal("password concatenated or unnecessary privileges granted")
		}
	}
	before := len(fixture.statements)
	for _, host := range []string{"%", "remote.example", "127.0.0.1';DROP USER root;--"} {
		if a.Create(ctx, "panel_monitor", host, password) == nil || a.Drop(ctx, "panel_monitor", host) == nil {
			t.Fatal("unsafe account host accepted")
		}
	}
	if a.Create(ctx, "evil'", "localhost", password) == nil || len(fixture.statements) != before {
		t.Fatal("invalid account reached SQL")
	}
	fixture.fail = true
	if err = a.Create(ctx, "panel_monitor", "localhost", password); err == nil || strings.Contains(err.Error(), "private driver error") {
		t.Fatal("raw error disclosed", err)
	}
}
func TestDatabaseErrorsAndGeneratedPasswordsAreSafe(t *testing.T) {
	first, err := randomPassword()
	if err != nil {
		t.Fatal(err)
	}
	second, err := randomPassword()
	if err != nil || len(first) != 48 || first == second {
		t.Fatal("weak generated credentials")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	_, err = connectAdmin(ctx, config.MetricsMySQLConfig{Socket: filepath.Join(t.TempDir(), "missing.sock"), User: "private-user", Password: "private-password"})
	if err == nil || strings.Contains(err.Error(), "private-user") || strings.Contains(err.Error(), "private-password") {
		t.Fatal("connection error disclosed credentials", err)
	}
}
