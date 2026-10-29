package metricssetup

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"errors"
	"net"
	"time"

	"github.com/azpanel/lnmp-gopanel/internal/config"
	"github.com/go-sql-driver/mysql"
)

type accountAdmin interface {
	Exists(context.Context, string, string) (bool, error)
	Create(context.Context, string, string, string) error
	Drop(context.Context, string, string) error
	Close() error
}
type databaseAdmin struct{ db *sql.DB }

func connectAdmin(ctx context.Context, c config.MetricsMySQLConfig) (accountAdmin, error) {
	options := mysql.NewConfig()
	options.User, options.Passwd = c.User, c.Password
	options.Net, options.Addr = "unix", c.Socket
	if c.Address != "" {
		options.Net, options.Addr = "tcp", c.Address
	}
	options.Timeout, options.ReadTimeout, options.WriteTimeout = 2*time.Second, 3*time.Second, 3*time.Second
	options.InterpolateParams = true
	connector, err := mysql.NewConnector(options)
	if err != nil {
		return nil, errors.New("管理员连接配置无效")
	}
	db := sql.OpenDB(connector)
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)
	if err = db.PingContext(ctx); err != nil {
		db.Close()
		return nil, errors.New("无法认证本机数据库管理员，请检查账号、密码和连接地址")
	}
	return &databaseAdmin{db: db}, nil
}
func (a *databaseAdmin) Exists(ctx context.Context, user, host string) (bool, error) {
	var count int
	err := a.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM mysql.user WHERE User = ? AND Host = ?", user, host).Scan(&count)
	if err != nil {
		return false, errors.New("无法检查监控账号，请确认管理员权限")
	}
	return count > 0, nil
}
func (a *databaseAdmin) Create(ctx context.Context, user, host, password string) error {
	if !accountName.MatchString(user) || !validAccountHost(host) {
		return errors.New("监控账号名称或来源无效")
	}
	// CREATE USER grants only implicit USAGE. No database, PROCESS, SUPER or
	// REPLICATION privileges are needed for the collector's two SHOW counters.
	if _, err := a.db.ExecContext(ctx, "CREATE USER ?@? IDENTIFIED BY ?", user, host, password); err != nil {
		return errors.New("监控账号创建失败，可能同名账号已存在或管理员权限不足")
	}
	return nil
}
func (a *databaseAdmin) Drop(ctx context.Context, user, host string) error {
	if !accountName.MatchString(user) || !validAccountHost(host) {
		return errors.New("监控账号名称或来源无效")
	}
	if _, err := a.db.ExecContext(ctx, "DROP USER ?@?", user, host); err != nil {
		return errors.New("新建监控账号清理失败")
	}
	return nil
}
func (a *databaseAdmin) Close() error { return a.db.Close() }
func validAccountHost(host string) bool {
	return host == "localhost" || host == "127.0.0.1" || host == "::1"
}
func accountHost(c config.MetricsMySQLConfig) string {
	if c.Socket != "" {
		return "localhost"
	}
	host, _, _ := net.SplitHostPort(c.Address)
	return host
}
func randomPassword() (string, error) {
	var data [24]byte
	if _, err := rand.Read(data[:]); err != nil {
		return "", err
	}
	return hex.EncodeToString(data[:]), nil
}
