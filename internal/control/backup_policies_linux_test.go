//go:build linux

package control

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/azpanel/lnmp-gopanel/internal/backups"
)

func sampleBackupPolicy() backups.SavePolicyRequest {
	return backups.SavePolicyRequest{Policy: backups.Policy{Name: "网站每日备份", Enabled: true, Schedule: "0 2 * * *", Compression: backups.Compression{Format: "zip", Level: 5}, Retention: backups.Retention{Databases: 7, Files: 7}, Databases: backups.DatabaseSelection{Enabled: true, Mode: "all", Excluded: []string{"information_schema", "performance_schema", "phpmyadmin", "sys"}}, Files: []backups.FileSource{}}, PasswordAction: "keep"}
}
func TestBackupPolicyPersistenceEncryptionAndRevision(t *testing.T) {
	p := backupTestInstaller(t)
	s := backupPolicyStore{state: p.state}
	r := sampleBackupPolicy()
	r.PasswordAction = "replace"
	r.Password = "密码-$(never-run)-secret"
	saved, err := s.save(r)
	if err != nil {
		t.Fatal(err)
	}
	if saved.ID == "" || !saved.Compression.HasPassword || saved.Revision != 1 {
		t.Fatal(saved)
	}
	raw, err := os.ReadFile(filepath.Join(s.state, "policies.json"))
	if err != nil || strings.Contains(string(raw), r.Password) {
		t.Fatal("plaintext persisted", err)
	}
	response, _ := json.Marshal(saved)
	if strings.Contains(string(response), "password_ciphertext") || strings.Contains(string(response), r.Password) {
		t.Fatal("secret leaked")
	}
	records, err := s.load()
	if err != nil {
		t.Fatal(err)
	}
	aead, err := s.aead()
	if err != nil {
		t.Fatal(err)
	}
	ciphertext, _ := base64.StdEncoding.DecodeString(records[0].Ciphertext)
	plaintext, err := aead.Open(nil, ciphertext[:aead.NonceSize()], ciphertext[aead.NonceSize():], []byte(saved.ID))
	if err != nil || string(plaintext) != r.Password {
		t.Fatal(err)
	}
	reopened := backupPolicyStore{state: s.state}
	got, err := reopened.get(saved.ID)
	if err != nil || got.Name != saved.Name {
		t.Fatal(got, err)
	}
	stale := backups.SavePolicyRequest{Policy: saved, PasswordAction: "keep"}
	update := stale
	update.Name = "编辑后的政策"
	updated, err := reopened.save(update)
	if err != nil || !updated.Compression.HasPassword {
		t.Fatal(updated, err)
	}
	if _, err = reopened.save(stale); !errors.Is(err, backups.ErrConflict) {
		t.Fatal("stale update accepted", err)
	}
	update.Policy = updated
	update.PasswordAction = "replace"
	update.Password = "new secret"
	updated, err = reopened.save(update)
	if err != nil {
		t.Fatal(err)
	}
	records, _ = reopened.load()
	aead, _ = reopened.aead()
	ciphertext, _ = base64.StdEncoding.DecodeString(records[0].Ciphertext)
	plaintext, err = aead.Open(nil, ciphertext[:aead.NonceSize()], ciphertext[aead.NonceSize():], []byte(updated.ID))
	if err != nil || string(plaintext) != "new secret" {
		t.Fatal(err)
	}
	update.Policy = updated
	update.PasswordAction = "clear"
	update.Password = ""
	updated, err = reopened.save(update)
	if err != nil || updated.Compression.HasPassword {
		t.Fatal(updated, err)
	}
	list, err := s.list()
	if err != nil || len(list) != 1 || list[0].Name != update.Name {
		t.Fatal(list, err)
	}
	if err = s.delete(backups.PolicyIdentity{ID: updated.ID, Revision: 1}); !errors.Is(err, backups.ErrConflict) {
		t.Fatal(err)
	}
	if err = s.delete(backups.PolicyIdentity{ID: updated.ID, Revision: updated.Revision}); err != nil {
		t.Fatal(err)
	}
	if _, err = s.get(updated.ID); !errors.Is(err, backups.ErrNotFound) {
		t.Fatal(err)
	}
	for _, name := range []string{"policies.json", "policies.key"} {
		info, err := os.Stat(filepath.Join(s.state, name))
		if err != nil || info.Mode().Perm() != 0600 {
			t.Fatal(name, err)
		}
	}
}
func TestBackupPolicyMissingKeyAndUnsafeState(t *testing.T) {
	p := backupTestInstaller(t)
	s := backupPolicyStore{state: p.state}
	r := sampleBackupPolicy()
	r.PasswordAction = "replace"
	r.Password = "secret"
	saved, err := s.save(r)
	if err != nil {
		t.Fatal(err)
	}
	if err = os.Remove(filepath.Join(s.state, "policies.key")); err != nil {
		t.Fatal(err)
	}
	r.Policy = saved
	if _, err = s.save(r); err == nil {
		t.Fatal("missing key silently replaced")
	}
	if _, err = os.Stat(filepath.Join(s.state, "policies.key")); !os.IsNotExist(err) {
		t.Fatal("new key invented", err)
	}
	if err = os.Chmod(filepath.Join(s.state, "policies.json"), 0644); err != nil {
		t.Fatal(err)
	}
	if _, err = s.list(); err == nil {
		t.Fatal("nonprivate policy accepted")
	}
}
func TestBackupFileSizesAndExclusions(t *testing.T) {
	dir := t.TempDir()
	files := map[string]string{"首页.html": "1234567890", "logs/app.log": strings.Repeat("l", 20), "cache/item": strings.Repeat("c", 30), "nested/node_modules/a.js": strings.Repeat("n", 40), "nested/keep space.txt": strings.Repeat("k", 50)}
	for name, data := range files {
		path := filepath.Join(dir, name)
		if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(data), 0600); err != nil {
			t.Fatal(err)
		}
	}
	outside := filepath.Join(t.TempDir(), "outside")
	if err := os.WriteFile(outside, []byte(strings.Repeat("x", 1000)), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(dir, "link")); err != nil {
		t.Fatal(err)
	}
	source := backups.FileSource{Path: dir, Excludes: []string{"cache/", "logs/*.log", "**/node_modules/**"}}
	got, err := inspectBackupFile(context.Background(), source)
	if err != nil || !got.Valid || !got.Complete || got.OriginalBytes != 150 || got.IncludedBytes != 60 {
		t.Fatal(got, err)
	}
	alias := filepath.Join(t.TempDir(), "alias")
	if err := os.Symlink(dir, alias); err != nil {
		t.Fatal(err)
	}
	aliasResult, err := inspectBackupFile(context.Background(), backups.FileSource{Path: alias, Excludes: source.Excludes})
	if err != nil || aliasResult.OriginalBytes != 150 || aliasResult.IncludedBytes != 60 {
		t.Fatal(aliasResult, err)
	}
	file, err := inspectBackupFile(context.Background(), backups.FileSource{Path: filepath.Join(dir, "首页.html")})
	if err != nil || file.Type != "file" || file.IncludedBytes != 10 {
		t.Fatal(file, err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	got, err = inspectBackupFile(ctx, source)
	if err != nil || got.Complete || got.Error == "" {
		t.Fatal("partial scan hidden", got, err)
	}
	for _, source := range []backups.FileSource{{Path: filepath.Join(dir, "missing")}, {Path: dir, Excludes: []string{"../escape"}}, {Path: dir, Excludes: []string{"/absolute"}}, {Path: dir, Excludes: []string{"["}}, {Path: "/proc"}, {Path: filepath.Join(dir, "首页.html"), Excludes: []string{"x"}}} {
		if _, err := inspectBackupFile(context.Background(), source); err == nil {
			t.Fatal("unsafe source accepted", source)
		}
	}
}

type backupSQLConnector struct{ queries *[]string }

func (c backupSQLConnector) Connect(context.Context) (driver.Conn, error) {
	return backupSQLConn{c.queries}, nil
}
func (c backupSQLConnector) Driver() driver.Driver { return backupSQLDriver{c.queries} }

type backupSQLDriver struct{ queries *[]string }

func (d backupSQLDriver) Open(string) (driver.Conn, error) { return backupSQLConn{d.queries}, nil }

type backupSQLConn struct{ queries *[]string }

func (c backupSQLConn) Prepare(string) (driver.Stmt, error) {
	return nil, errors.New("unexpected prepare")
}
func (c backupSQLConn) Close() error              { return nil }
func (c backupSQLConn) Begin() (driver.Tx, error) { return nil, errors.New("unexpected transaction") }
func (c backupSQLConn) QueryContext(_ context.Context, q string, args []driver.NamedValue) (driver.Rows, error) {
	*c.queries = append(*c.queries, q)
	if strings.Contains(q, "SCHEMATA") {
		return &backupSQLRows{cols: []string{"name", "size"}, values: [][]driver.Value{{"app", int64(2048)}, {"empty", int64(0)}}}, nil
	}
	if len(args) < 2 || args[0].Value != "库'名称" || args[1].Value != "x_%'" {
		return nil, errors.New("identifiers not bound")
	}
	if strings.Contains(q, "COUNT(*)") {
		return &backupSQLRows{cols: []string{"count"}, values: [][]driver.Value{{int64(2)}}}, nil
	}
	return &backupSQLRows{cols: []string{"name", "type", "size"}, values: [][]driver.Value{{"table", "BASE TABLE", int64(1024)}, {"view", "VIEW", nil}}}, nil
}

type backupSQLRows struct {
	cols   []string
	values [][]driver.Value
	index  int
}

func (r *backupSQLRows) Columns() []string { return r.cols }
func (r *backupSQLRows) Close() error      { return nil }
func (r *backupSQLRows) Next(dest []driver.Value) error {
	if r.index >= len(r.values) {
		return io.EOF
	}
	copy(dest, r.values[r.index])
	r.index++
	return nil
}
func TestBackupDatabaseMetadataQueries(t *testing.T) {
	queries := []string{}
	db := sql.OpenDB(backupSQLConnector{&queries})
	defer db.Close()
	value, err := queryBackupDatabase(context.Background(), db, "databases", backups.DatabaseInspectRequest{})
	if err != nil || len(value.([]backups.DatabaseItem)) != 2 {
		t.Fatal(value, err)
	}
	value, err = queryBackupDatabase(context.Background(), db, "tables", backups.DatabaseInspectRequest{Database: "库'名称", Search: "x_%'", Page: 2})
	if err != nil {
		t.Fatal(err)
	}
	list := value.(backups.TableList)
	if list.Total != 2 || list.Page != 2 || list.Items[1].Size != nil || *list.Items[0].Size != 1024 {
		t.Fatal(list)
	}
	for _, q := range queries {
		if strings.Contains(q, "库'名称") || strings.Contains(q, "x_%'") {
			t.Fatal("identifier interpolated", q)
		}
	}
}
