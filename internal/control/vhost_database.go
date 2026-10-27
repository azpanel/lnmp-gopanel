package control

import (
	"bufio"
	"context"
	"database/sql"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/azpanel/lnmp-gopanel/internal/vhosts"
	"github.com/go-sql-driver/mysql"
)

func databaseInstalled() bool {
	for _, p := range []string{"/usr/local/mysql", "/usr/local/mariadb", "/run/mysqld/mysqld.sock", "/tmp/mysql.sock"} {
		if _, e := os.Stat(p); e == nil {
			return true
		}
	}
	return false
}

// Read only connection data. Never source root's shell or run an LNMP-owned
// database client as root (LNMP installations may own those binaries as mysql).
func mysqlClientOptions(data []byte) map[string]string {
	out := map[string]string{}
	section := ""
	scanner := bufio.NewScanner(strings.NewReader(string(data)))
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") || strings.HasPrefix(line, ";") {
			continue
		}
		if strings.HasPrefix(line, "[") {
			section = strings.Trim(line, "[] ")
			continue
		}
		if section != "client" {
			continue
		}
		key, value, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		key = strings.TrimSpace(key)
		value = strings.TrimSpace(value)
		if !strings.Contains("|user|password|socket|host|port|", "|"+key+"|") {
			continue
		}
		if len(value) >= 2 && ((value[0] == '\'' && value[len(value)-1] == '\'') || (value[0] == '"' && value[len(value)-1] == '"')) {
			value = value[1 : len(value)-1]
		}
		var decoded strings.Builder
		for i := 0; i < len(value); i++ {
			if value[i] == '\\' && i+1 < len(value) {
				i++
				switch value[i] {
				case 'n':
					decoded.WriteByte('\n')
				case 'r':
					decoded.WriteByte('\r')
				case 't':
					decoded.WriteByte('\t')
				case 'b':
					decoded.WriteByte('\b')
				case 's':
					decoded.WriteByte(' ')
				default:
					decoded.WriteByte(value[i])
				}
			} else {
				decoded.WriteByte(value[i])
			}
		}
		out[key] = decoded.String()
	}
	return out
}

func localDatabaseConfig(password string) *mysql.Config {
	c := mysql.NewConfig()
	c.User = "root"
	c.Net = "unix"
	c.Addr = "/tmp/mysql.sock"
	c.Timeout = 2 * time.Second
	c.ReadTimeout = 5 * time.Second
	c.WriteTimeout = 5 * time.Second
	c.InterpolateParams = true
	for _, path := range []string{"/run/mysqld/mysqld.sock", "/var/run/mysqld/mysqld.sock", "/tmp/mysql.sock", "/usr/local/mysql/mysql.sock", "/usr/local/mariadb/mysql.sock"} {
		if _, err := os.Stat(path); err == nil {
			c.Addr = path
			break
		}
	}
	for _, path := range []string{"/etc/my.cnf", "/etc/mysql/my.cnf", "/root/.my.cnf"} {
		data, err := readTrustedFile(path, 64*1024)
		if err != nil {
			continue
		}
		opts := mysqlClientOptions(data)
		if opts["socket"] != "" && filepath.IsAbs(opts["socket"]) {
			c.Net = "unix"
			c.Addr = opts["socket"]
		}
		if path == "/root/.my.cnf" {
			if opts["user"] != "" {
				c.User = opts["user"]
			}
			c.Passwd = opts["password"]
		}
	}
	if password != "" {
		c.User = "root"
		c.Passwd = password
	}
	return c
}
func openSiteDatabase(ctx context.Context, password string) (*sql.DB, error) {
	connector, err := mysql.NewConnector(localDatabaseConfig(password))
	if err != nil {
		return nil, errors.New("数据库连接配置无效")
	}
	db := sql.OpenDB(connector)
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)
	if err = db.PingContext(ctx); err != nil {
		db.Close()
		return nil, errors.New("无法认证本机数据库，请填写数据库管理员密码并重试")
	}
	return db, nil
}
func databasePreflight(ctx context.Context, db *sql.DB, name string) error {
	if !vhosts.DatabaseName.MatchString(name) {
		return errors.New("数据库名称无效")
	}
	var n int
	if err := db.QueryRowContext(ctx, "SELECT COUNT(*) FROM information_schema.SCHEMATA WHERE SCHEMA_NAME = ?", name).Scan(&n); err != nil {
		return errors.New("无法检查数据库名称，请确认管理员权限")
	}
	if n != 0 {
		return errors.New("同名数据库已存在，不会覆盖")
	}
	if err := db.QueryRowContext(ctx, "SELECT COUNT(*) FROM mysql.user WHERE User = ? AND Host IN ('localhost','127.0.0.1')", name).Scan(&n); err != nil {
		return errors.New("无法检查数据库账号，请确认管理员权限")
	}
	if n != 0 {
		return errors.New("同名本机数据库账号已存在，不会覆盖")
	}
	return nil
}
func createSiteDatabase(ctx context.Context, db *sql.DB, r vhosts.DatabaseInput) error {
	if err := databasePreflight(ctx, db, r.Name); err != nil {
		return err
	}
	quoted := "`" + r.Name + "`"
	// Database-level GRANT treats underscores as wildcards, even in backticks.
	// MySQL partial_revokes changes that behavior; MariaDB lacks this variable.
	grantName := r.Name
	var variable, setting string
	modeErr := db.QueryRowContext(ctx, "SHOW GLOBAL VARIABLES LIKE 'partial_revokes'").Scan(&variable, &setting)
	if modeErr != nil && !errors.Is(modeErr, sql.ErrNoRows) {
		return errors.New("无法读取数据库授权模式，未创建资源")
	}
	if !strings.EqualFold(setting, "ON") {
		grantName = strings.ReplaceAll(grantName, "_", `\_`)
	}
	grantTarget := "`" + grantName + "`"
	createdDB := false
	hosts := []string{}
	cleanup := func() error {
		restore, done := context.WithTimeout(context.Background(), 10*time.Second)
		defer done()
		var failures []string
		for _, host := range hosts {
			if _, e := db.ExecContext(restore, "DROP USER '"+r.Name+"'@'"+host+"'"); e != nil {
				failures = append(failures, "账号")
			}
		}
		if createdDB {
			if _, e := db.ExecContext(restore, "DROP DATABASE "+quoted); e != nil {
				failures = append(failures, "数据库")
			}
		}
		if len(failures) > 0 {
			return errors.New("数据库创建失败，部分新增资源清理失败，请手动检查")
		}
		return errors.New("数据库创建失败，已清理本次新增资源")
	}
	// User and host are fixed validated ASCII identifiers; passwords are bound
	// through the driver's SQL escaping, never concatenated or logged.
	if _, err := db.ExecContext(ctx, "CREATE DATABASE "+quoted+" CHARACTER SET utf8mb4"); err != nil {
		return errors.New("数据库创建失败，同名资源可能已被其他操作创建")
	}
	createdDB = true
	for _, host := range []string{"localhost", "127.0.0.1"} {
		account := "'" + r.Name + "'@'" + host + "'"
		if _, err := db.ExecContext(ctx, "CREATE USER "+account+" IDENTIFIED BY ?", r.Password); err != nil {
			return cleanup()
		}
		hosts = append(hosts, host)
		if _, err := db.ExecContext(ctx, "GRANT ALL PRIVILEGES ON "+grantTarget+".* TO "+account); err != nil {
			return cleanup()
		}
	}
	return nil
}
