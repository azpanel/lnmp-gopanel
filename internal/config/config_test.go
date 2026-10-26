package config

import (
	"golang.org/x/crypto/bcrypt"
	"gopkg.in/yaml.v3"
	"os"
	"path/filepath"
	"testing"
)

func TestServiceWhitelist(t *testing.T) {
	for _, s := range []Service{{Name: "nginx", Manager: "init", Program: "/etc/init.d/nginx"}, {Name: "php-fpm", Manager: "systemd", Program: "/usr/bin/systemctl", Target: "php-fpm.service"}, {Name: "mariadb", Manager: "lnmp", Program: "/bin/lnmp", Target: "mariadb"}, {Name: "redis", Manager: "init", Program: "/etc/init.d/redis"}, {Name: "redis", Manager: "systemd", Program: "/usr/bin/systemctl", Target: "redis-server.service"}} {
		if err := s.Validate(); err != nil {
			t.Fatal(err)
		}
	}
	for _, s := range []Service{{Name: "nginx;id", Manager: "init", Program: "/etc/init.d/nginx"}, {Name: "nginx", Manager: "init", Program: "/bin/sh"}, {Name: "nginx", Manager: "systemd", Program: "/usr/bin/systemctl", Target: "ssh.service"}, {Name: "nginx", Manager: "lnmp", Program: "/bin/lnmp", Target: "mysql"}, {Name: "mysql", Manager: "init", Program: "/etc/init.d/mysql", CheckProgram: "/usr/sbin/nginx"}, {Name: "nginx", Manager: "systemd", Program: "systemctl", Target: "nginx.service"}} {
		if err := s.Validate(); err == nil {
			t.Fatalf("unsafe service accepted: %+v", s)
		}
	}
}

func TestRedisRejectsUnrelatedUnitsAndLNMPWrapper(t *testing.T) {
	for _, s := range []Service{
		{Name: "redis", Manager: "systemd", Program: "/usr/bin/systemctl", Target: "sshd.service"},
		{Name: "redis", Manager: "init", Program: "/etc/init.d/mysql"},
		{Name: "redis", Manager: "lnmp", Program: "/usr/bin/lnmp", Target: "redis"},
	} {
		if s.Validate() == nil {
			t.Fatalf("unsafe Redis config accepted: %+v", s)
		}
	}
}

func TestHTTPSOriginAndMalformedOrigins(t *testing.T) {
	hash, err := bcrypt.GenerateFromPassword([]byte("test-password"), bcrypt.MinCost)
	if err != nil {
		t.Fatal(err)
	}
	file := filepath.Join(t.TempDir(), "config.yaml")
	for _, origin := range []string{"https://panel.example.com/", "https://panel.example.com/path", "https://user:pass@panel.example.com", "https://", "javascript:alert(1)", "https://panel.example.com?x=1"} {
		c := Config{Username: "admin", PasswordHash: string(hash), PublicOrigin: origin}
		data, _ := yaml.Marshal(c)
		if err := os.WriteFile(file, data, 0600); err != nil {
			t.Fatal(err)
		}
		result, err := Load(file)
		if origin == "https://panel.example.com/" {
			if err != nil || !result.SecureCookie || result.PublicOrigin != "https://panel.example.com" {
				t.Fatalf("HTTPS cookie defaults: %+v %v", result, err)
			}
		} else if err == nil {
			t.Fatalf("invalid origin accepted: %s", origin)
		}
	}
}

func TestScreenshotConfigDefaultsAndAbsolutePaths(t *testing.T) {
	hash, err := bcrypt.GenerateFromPassword([]byte("test-password"), bcrypt.MinCost)
	if err != nil {
		t.Fatal(err)
	}
	file := filepath.Join(t.TempDir(), "config.yaml")
	for _, settings := range []ScreenshotConfig{{}, {Browser: "chromium"}, {StateDir: "state"}, {Browser: "/usr/bin/chromium", StateDir: "/var/lib/lnmp-gopanel"}} {
		c := Config{Username: "admin", PasswordHash: string(hash), Screenshots: settings}
		data, _ := yaml.Marshal(c)
		if err := os.WriteFile(file, data, 0600); err != nil {
			t.Fatal(err)
		}
		loaded, err := Load(file)
		if settings.Browser == "chromium" || settings.StateDir == "state" {
			if err == nil {
				t.Fatal("relative screenshot path accepted")
			}
			continue
		}
		if err != nil {
			t.Fatal(err)
		}
		if settings.StateDir == "" && loaded.Screenshots.StateDir != filepath.Join(filepath.Dir(file), "screenshots-state") {
			t.Fatal("local config should use local state")
		}
	}
}
