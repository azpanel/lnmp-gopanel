package config

import (
	"os"
	"path/filepath"
	"testing"

	"golang.org/x/crypto/bcrypt"
	"gopkg.in/yaml.v3"
)

func TestMetricsConfigLocalConnections(t *testing.T) {
	for _, raw := range []string{"http://127.0.0.1/status", "http://[::1]:8080/fpm?json", "http://127.1.2.3:80/"} {
		if !LoopbackURL(raw) {
			t.Fatal("local URL rejected", raw)
		}
	}
	for _, raw := range []string{"https://127.0.0.1/status", "http://localhost/status", "http://example.com/status", "http://192.168.1.1/status", "http://user:pass@127.0.0.1/status", "http://127.0.0.1/status#x", "file:///etc/passwd", "http://127.0.0.1:99999/status"} {
		if LoopbackURL(raw) {
			t.Fatal("unsafe URL accepted", raw)
		}
	}
	for _, db := range []MetricsMySQLConfig{{Socket: "/run/mysql.sock", User: "monitor"}, {Address: "127.0.0.1:3306", User: "monitor"}, {Address: "[::1]:3306", User: "monitor"}, {}} {
		if err := (MetricsConfig{StateDir: "/state", MySQL: db}).Validate(); err != nil {
			t.Fatal(err)
		}
	}
	for _, db := range []MetricsMySQLConfig{{Socket: "relative", User: "m"}, {Address: "mysql.example:3306", User: "m"}, {Socket: "/run/mysql.sock"}, {User: "m"}, {Socket: "/sock", Address: "127.0.0.1:3306", User: "m"}} {
		if (MetricsConfig{StateDir: "/state", MySQL: db}).Validate() == nil {
			t.Fatal("invalid database config accepted")
		}
	}
}
func TestMetricsStateDefaultsAndLegacyCompatibility(t *testing.T) {
	hash, _ := bcrypt.GenerateFromPassword([]byte("test-password"), bcrypt.MinCost)
	path := filepath.Join(t.TempDir(), "config.yaml")
	c := Config{Username: "admin", PasswordHash: string(hash), TLS: TLSConfig{CertFile: "/cert", KeyFile: "/key"}}
	data, _ := yaml.Marshal(c)
	if err := os.WriteFile(path, data, 0600); err != nil {
		t.Fatal(err)
	}
	loaded, err := Load(path)
	if err != nil || loaded.Metrics.StateDir != filepath.Join(filepath.Dir(path), "metrics-state") {
		t.Fatal("legacy config not supported", err)
	}
}
