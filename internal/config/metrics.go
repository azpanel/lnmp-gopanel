package config

import (
	"errors"
	"net"
	"net/url"
	"path/filepath"
)

// MetricsConfig contains server-only, read-only monitoring connections.
type MetricsConfig struct {
	StateDir        string             `yaml:"state_dir"`
	NginxStatusURL  string             `yaml:"nginx_status_url"`
	PHPFPMStatusURL string             `yaml:"php_fpm_status_url"`
	MySQL           MetricsMySQLConfig `yaml:"mysql"`
}
type MetricsMySQLConfig struct {
	Socket   string `yaml:"socket"`
	Address  string `yaml:"address"`
	User     string `yaml:"user"`
	Password string `yaml:"password"`
}

// Only literal loopback IPs are accepted, so DNS cannot redirect monitoring.
func LoopbackURL(raw string) bool {
	u, err := url.Parse(raw)
	if err != nil || u.Scheme != "http" || u.User != nil || u.Fragment != "" {
		return false
	}
	ip := net.ParseIP(u.Hostname())
	if ip == nil || !ip.IsLoopback() {
		return false
	}
	if u.Port() != "" {
		_, err = net.LookupPort("tcp", u.Port())
		return err == nil
	}
	return true
}
func (m MetricsConfig) Validate() error {
	if !filepath.IsAbs(m.StateDir) || filepath.Clean(m.StateDir) != m.StateDir {
		return errors.New("metrics.state_dir must be a clean absolute path")
	}
	for _, raw := range []string{m.NginxStatusURL, m.PHPFPMStatusURL} {
		if raw != "" && !LoopbackURL(raw) {
			return errors.New("metrics status URLs must use http and a literal loopback IP")
		}
	}
	db := m.MySQL
	if db.Socket != "" && db.Address != "" {
		return errors.New("metrics.mysql: use either socket or address")
	}
	if db.Socket != "" && (!filepath.IsAbs(db.Socket) || filepath.Clean(db.Socket) != db.Socket) {
		return errors.New("metrics.mysql.socket must be a clean absolute path")
	}
	if db.Address != "" {
		host, port, err := net.SplitHostPort(db.Address)
		ip := net.ParseIP(host)
		if err != nil || ip == nil || !ip.IsLoopback() {
			return errors.New("metrics.mysql.address must use a literal loopback IP and port")
		}
		if _, err = net.LookupPort("tcp", port); err != nil {
			return errors.New("metrics.mysql.address has an invalid port")
		}
	}
	if (db.Socket != "" || db.Address != "") && db.User == "" {
		return errors.New("metrics.mysql.user is required")
	}
	if db.User != "" && db.Socket == "" && db.Address == "" {
		return errors.New("metrics.mysql requires socket or address")
	}
	return nil
}
