//go:build linux

package control

import (
	"bytes"
	"context"
	"database/sql"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/azpanel/lnmp-gopanel/internal/backups"
	"github.com/go-sql-driver/mysql"
)

// This fixture starts its own daemon with --no-defaults, an isolated data
// directory/socket, and networking disabled. It never contacts the host database.
func isolatedBackupDatabase(t *testing.T) (*sql.DB, *mysql.Config, string, string) {
	t.Helper()
	base := os.Getenv("GOPANEL_TEST_MARIADB_BASE")
	if base == "" {
		t.Skip("set GOPANEL_TEST_MARIADB_BASE to an extracted MariaDB installation for dump/restore integration")
	}
	p := backupTestInstaller(t)
	data := filepath.Join(p.state, "db")
	socket := filepath.Join(p.state, "db.sock")
	logfile := filepath.Join(p.state, "db.log")
	server := filepath.Join(base, "sbin/mariadbd")
	client := filepath.Join(base, "bin/mariadb")
	dump := filepath.Join(base, "bin/mariadb-dump")
	installer := filepath.Join(base, "bin/mariadb-install-db")
	for _, program := range []string{server, client, dump, installer} {
		if err := TrustedPath(program); err != nil {
			t.Fatal("untrusted fixture executable", program, err)
		}
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	init := exec.CommandContext(ctx, installer, "--no-defaults", "--basedir="+base, "--datadir="+data, "--auth-root-authentication-method=normal", "--skip-test-db", "--user=root")
	if output, err := init.CombinedOutput(); err != nil {
		t.Fatalf("isolated database initialization: %v\n%s", err, output)
	}
	cmd := exec.Command(server, "--no-defaults", "--user=root", "--basedir="+base, "--datadir="+data, "--socket="+socket, "--skip-networking", "--pid-file="+filepath.Join(p.state, "db.pid"), "--log-error="+logfile, "--innodb-buffer-pool-size=32M", "--innodb-log-file-size=8M", "--performance-schema=OFF", "--skip-log-bin", "--tmpdir="+p.state)
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = cmd.Process.Signal(os.Interrupt)
		done := make(chan error, 1)
		go func() { done <- cmd.Wait() }()
		select {
		case <-done:
		case <-time.After(10 * time.Second):
			_ = cmd.Process.Kill()
			<-done
		}
	})
	config := mysql.NewConfig()
	config.User = "root"
	config.Net = "unix"
	config.Addr = socket
	config.Timeout = time.Second
	config.MultiStatements = true
	config.InterpolateParams = true
	connector, err := mysql.NewConnector(config)
	if err != nil {
		t.Fatal(err)
	}
	db := sql.OpenDB(connector)
	deadline := time.Now().Add(20 * time.Second)
	for db.Ping() != nil {
		if time.Now().After(deadline) {
			log, _ := os.ReadFile(logfile)
			t.Fatalf("isolated database did not start: %s", log)
		}
		time.Sleep(50 * time.Millisecond)
	}
	// Exercise MySQL option-file quoting, rather than password flags or env vars.
	password := "fixture-'quoted\"-\\-#-line\nnext"
	if _, err = db.Exec("SET PASSWORD FOR 'root'@'localhost' = PASSWORD(?)", password); err != nil {
		t.Fatal(err)
	}
	db.Close()
	config.Passwd = password
	connector, err = mysql.NewConnector(config)
	if err != nil {
		t.Fatal(err)
	}
	db = sql.OpenDB(connector)
	if err = db.Ping(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	return db, config, dump, client
}

func TestBackupDatabaseDumpRestoreSelectionsAndCredentials(t *testing.T) {
	db, config, dump, client := isolatedBackupDatabase(t)
	ctx := context.Background()
	root := filepath.Dir(config.Addr)
	dbName := "备份库_名"
	emptyName := "empty_backup_test"
	reset := func() {
		t.Helper()
		for _, name := range []string{dbName, emptyName} {
			if _, err := db.Exec("DROP DATABASE IF EXISTS " + backupSQLIdentifier(name)); err != nil {
				t.Fatal(err)
			}
		}
		if _, err := db.Exec("CREATE DATABASE " + backupSQLIdentifier(dbName) + " CHARACTER SET utf8mb4 COLLATE utf8mb4_bin; CREATE DATABASE " + backupSQLIdentifier(emptyName)); err != nil {
			t.Fatal(err)
		}
		prefix := backupSQLIdentifier(dbName) + "."
		for _, query := range []string{"CREATE TABLE " + prefix + "orders (id INT PRIMARY KEY, value VARBINARY(64)) ENGINE=InnoDB", "INSERT INTO " + prefix + "orders VALUES (1, X'00FF4142')", "CREATE TABLE " + prefix + "audit_log (id INT)", "CREATE VIEW " + prefix + "v_orders AS SELECT id,value FROM " + prefix + "orders", "CREATE TRIGGER " + prefix + "tr_order AFTER INSERT ON " + prefix + "orders FOR EACH ROW INSERT INTO " + prefix + "audit_log VALUES (NEW.id)", "CREATE PROCEDURE " + prefix + "routine_test() SELECT 42", "CREATE EVENT " + prefix + "event_test ON SCHEDULE EVERY 1 DAY DO SELECT 1"} {
			if _, err := db.Exec(query); err != nil {
				t.Fatal(query, err)
			}
		}
	}
	defaults := filepath.Join(root, "restore.cnf")
	if err := os.WriteFile(defaults, []byte("[client]\nuser=root\nprotocol=socket\nsocket="+mysqlBackupOption(config.Addr)+"\npassword="+mysqlBackupOption(config.Passwd)+"\n"), 0600); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name       string
		selection  backups.DatabaseSelection
		expected   []string
		wantTables []string
	}{
		{"all", backups.DatabaseSelection{Enabled: true, Mode: "all", Excluded: []string{"mysql", "sys", "information_schema", "performance_schema"}}, []string{dbName, emptyName}, []string{"audit_log", "orders", "v_orders"}},
		{"selected-include", backups.DatabaseSelection{Enabled: true, Mode: "selected", Selected: []string{dbName}, Rules: []backups.TableRule{{Database: dbName, Mode: "include", Tables: []string{"orders", "v_orders"}}}}, []string{dbName}, []string{"orders", "v_orders"}},
		{"selected-exclude", backups.DatabaseSelection{Enabled: true, Mode: "selected", Selected: []string{dbName}, Rules: []backups.TableRule{{Database: dbName, Mode: "exclude", Tables: []string{"audit_log"}}}}, []string{dbName}, []string{"orders", "v_orders"}},
		{"exclude-every-table", backups.DatabaseSelection{Enabled: true, Mode: "selected", Selected: []string{dbName}, Rules: []backups.TableRule{{Database: dbName, Mode: "exclude", Tables: []string{"audit_log", "orders", "v_orders"}}}}, []string{dbName}, []string{}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			reset()
			stage := filepath.Join(root, tc.name)
			if err := os.Mkdir(stage, 0700); err != nil {
				t.Fatal(err)
			}
			manifest := backupManifest{}
			if err := dumpBackupDatabases(ctx, db, config, dump, tc.selection, stage, &manifest); err != nil {
				probe := exec.CommandContext(ctx, dump, "--defaults-file="+defaults, "--single-transaction", "--skip-lock-tables", "--quick", "--hex-blob", "--no-tablespaces", "--routines", "--events", "--triggers", "--", dbName)
				var diagnostic bytes.Buffer
				probe.Stderr = &diagnostic
				probeErr := probe.Run()
				t.Logf("isolated dump diagnostic: %v %s", probeErr, strings.ReplaceAll(diagnostic.String(), config.Passwd, "[redacted]"))
				t.Fatal(err)
			}
			if len(manifest.Sources) != len(tc.expected) {
				t.Fatal(manifest)
			}
			for _, name := range []string{dbName, emptyName} {
				if _, err := db.Exec("DROP DATABASE " + backupSQLIdentifier(name)); err != nil {
					t.Fatal(err)
				}
			}
			for _, source := range manifest.Sources {
				data, err := os.ReadFile(filepath.Join(stage, source.ArchivePath))
				if err != nil {
					t.Fatal(err)
				}
				if bytes.Contains(data, []byte(config.Passwd)) {
					t.Fatal("password in dump")
				}
				cmd := exec.CommandContext(ctx, client, "--defaults-file="+defaults, "--binary-mode=1")
				cmd.Stdin = bytes.NewReader(data)
				if output, err := cmd.CombinedOutput(); err != nil {
					t.Fatalf("restore failed: %v\n%s", err, output)
				}
			}
			rows, err := db.Query("SELECT TABLE_NAME FROM information_schema.TABLES WHERE TABLE_SCHEMA=? ORDER BY TABLE_NAME", dbName)
			if err != nil {
				t.Fatal(err)
			}
			actual := []string{}
			for rows.Next() {
				var table string
				if err = rows.Scan(&table); err != nil {
					t.Fatal(err)
				}
				actual = append(actual, table)
			}
			rows.Close()
			if strings.Join(actual, ",") != strings.Join(tc.wantTables, ",") {
				t.Fatal("restored selection", actual, tc.wantTables)
			}
			if len(tc.wantTables) > 0 {
				var hex string
				if err = db.QueryRow("SELECT HEX(value) FROM " + backupSQLIdentifier(dbName) + ".orders WHERE id=1").Scan(&hex); err != nil || hex != "00FF4142" {
					t.Fatal("binary data not restored", hex, err)
				}
			}
			var collation string
			if err = db.QueryRow("SELECT DEFAULT_COLLATION_NAME FROM information_schema.SCHEMATA WHERE SCHEMA_NAME=?", dbName).Scan(&collation); err != nil || collation != "utf8mb4_bin" {
				t.Fatal(collation, err)
			}
			if _, err = os.Stat(filepath.Join(root, "mysql-client.cnf")); !os.IsNotExist(err) {
				t.Fatal("temporary credentials retained", err)
			}
		})
	}
	reset()
	for _, selection := range []backups.DatabaseSelection{{Enabled: true, Mode: "selected", Selected: []string{"missing_database"}}, {Enabled: true, Mode: "selected", Selected: []string{dbName}, Rules: []backups.TableRule{{Database: dbName, Mode: "include", Tables: []string{"missing_table"}}}}} {
		stage := t.TempDir()
		if err := dumpBackupDatabases(ctx, db, config, dump, selection, stage, &backupManifest{}); err == nil {
			t.Fatal("missing selection accepted")
		}
	}
	wrong := config.Clone()
	wrong.Passwd = "wrong-password"
	if err := dumpBackupDatabases(ctx, db, wrong, dump, backups.DatabaseSelection{Enabled: true, Mode: "selected", Selected: []string{dbName}}, t.TempDir(), &backupManifest{}); err == nil || strings.Contains(err.Error(), wrong.Passwd) {
		t.Fatal("authentication error missing or secret exposed", err)
	}
}
