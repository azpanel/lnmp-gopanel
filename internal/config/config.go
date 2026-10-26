package config

import (
	"errors"
	"fmt"
	"net"
	"net/url"
	"os"
	"path/filepath"
	"strings"

	"golang.org/x/crypto/bcrypt"
	"gopkg.in/yaml.v3"
)

type Config struct {
	Listen       string           `yaml:"listen"`
	Username     string           `yaml:"username"`
	PasswordHash string           `yaml:"password_hash"`
	SecureCookie bool             `yaml:"secure_cookie"`
	PublicOrigin string           `yaml:"public_origin"`
	TLS          TLSConfig        `yaml:"tls"`
	Helper       string           `yaml:"helper"`
	Screenshots  ScreenshotConfig `yaml:"screenshots"`
}
type TLSConfig struct {
	CertFile string `yaml:"cert_file"`
	KeyFile  string `yaml:"key_file"`
}
type ScreenshotConfig struct {
	Browser  string `yaml:"browser"`
	StateDir string `yaml:"state_dir"`
}

type Service struct {
	Name         string `yaml:"name" json:"name"`
	Manager      string `yaml:"manager" json:"-"`
	Program      string `yaml:"program" json:"-"`
	Target       string `yaml:"target" json:"-"`
	CheckProgram string `yaml:"check_program,omitempty" json:"-"`
}

type Services struct {
	Services []Service `yaml:"services"`
}

func Read(path string, out any) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()
	d := yaml.NewDecoder(f)
	d.KnownFields(true)
	return d.Decode(out)
}

func Load(path string) (Config, error) {
	var c Config
	if err := Read(path, &c); err != nil {
		return c, err
	}
	if c.Username == "" || len(c.Username) > 128 {
		return c, errors.New("username is required (max 128 characters)")
	}
	if _, err := bcrypt.Cost([]byte(c.PasswordHash)); err != nil {
		return c, errors.New("invalid bcrypt password_hash")
	}
	if c.Listen == "" {
		c.Listen = "127.0.0.1:8090"
	}
	if _, _, err := net.SplitHostPort(c.Listen); err != nil {
		return c, fmt.Errorf("listen: %w", err)
	}
	if !filepath.IsAbs(c.TLS.CertFile) || !filepath.IsAbs(c.TLS.KeyFile) {
		return c, errors.New("HTTPS requires absolute tls.cert_file and tls.key_file paths")
	}
	c.SecureCookie = true
	if c.Helper == "" {
		c.Helper = "/usr/local/libexec/lnmp-gopanel-control"
	}
	if !filepath.IsAbs(c.Helper) {
		return c, errors.New("helper must be an absolute path")
	}
	if c.Screenshots.Browser != "" && !filepath.IsAbs(c.Screenshots.Browser) {
		return c, errors.New("screenshots.browser must be an absolute path")
	}
	if c.Screenshots.StateDir == "" {
		absolute, err := filepath.Abs(path)
		if err != nil {
			return c, err
		}
		if filepath.Dir(absolute) == "/etc/lnmp-gopanel" {
			c.Screenshots.StateDir = "/var/lib/lnmp-gopanel"
		} else {
			c.Screenshots.StateDir = filepath.Join(filepath.Dir(absolute), "screenshots-state")
		}
	}
	if !filepath.IsAbs(c.Screenshots.StateDir) {
		return c, errors.New("screenshots.state_dir must be an absolute path")
	}
	if c.PublicOrigin != "" {
		parsed, err := url.Parse(c.PublicOrigin)
		if err != nil || parsed.Scheme != "https" || parsed.Host == "" || parsed.User != nil || (parsed.Path != "" && parsed.Path != "/") || parsed.RawQuery != "" || parsed.Fragment != "" {
			return c, errors.New("public_origin must be an https origin without credentials, path, query or fragment")
		}
		c.PublicOrigin = strings.TrimSuffix(c.PublicOrigin, "/")
	}
	return c, nil
}

func ValidName(s string) bool {
	return s == "nginx" || s == "php-fpm" || s == "mysql" || s == "mariadb" || s == "redis"
}
func ValidAction(s string) bool {
	return s == "status" || s == "start" || s == "stop" || s == "restart" || s == "reload"
}

func (s Service) Validate() error {
	if !ValidName(s.Name) {
		return errors.New("unsupported service")
	}
	if !filepath.IsAbs(s.Program) {
		return errors.New("program must be absolute")
	}
	switch s.Manager {
	case "systemd":
		if s.Program != "/usr/bin/systemctl" && s.Program != "/bin/systemctl" {
			return errors.New("invalid systemctl path")
		}
		allowed := map[string][]string{"nginx": {"nginx.service"}, "php-fpm": {"php-fpm.service", "php.service"}, "mysql": {"mysql.service", "mysqld.service"}, "mariadb": {"mariadb.service"}, "redis": {"redis.service", "redis-server.service"}}
		ok := false
		for _, target := range allowed[s.Name] {
			if s.Target == target {
				ok = true
			}
		}
		if !ok {
			return errors.New("invalid unit")
		}
	case "init":
		allowed := map[string][]string{"nginx": {"/etc/init.d/nginx"}, "php-fpm": {"/etc/init.d/php-fpm"}, "mysql": {"/etc/init.d/mysql", "/etc/init.d/mysqld"}, "mariadb": {"/etc/init.d/mariadb", "/etc/init.d/mysql"}, "redis": {"/etc/init.d/redis", "/etc/init.d/redis-server"}}
		ok := false
		for _, p := range allowed[s.Name] {
			if s.Program == p {
				ok = true
			}
		}
		if !ok || s.Target != "" {
			return errors.New("invalid init script")
		}
	case "lnmp":
		if s.Name == "redis" {
			return errors.New("redis requires its own systemd unit or init script")
		}
		if s.Program != "/usr/bin/lnmp" && s.Program != "/bin/lnmp" && s.Program != "/usr/local/bin/lnmp" {
			return errors.New("invalid lnmp path")
		}
		if s.Target != s.Name {
			return errors.New("invalid lnmp target")
		}
	default:
		return errors.New("unsupported manager")
	}
	if s.CheckProgram != "" && (s.Name != "nginx" || (s.CheckProgram != "/usr/local/nginx/sbin/nginx" && s.CheckProgram != "/usr/sbin/nginx")) {
		return errors.New("invalid nginx executable")
	}
	return nil
}

func LoadServices(path string) ([]Service, error) {
	var c Services
	if err := Read(path, &c); err != nil {
		return nil, err
	}
	seen := map[string]bool{}
	for _, s := range c.Services {
		if err := s.Validate(); err != nil {
			return nil, fmt.Errorf("%s: %w", s.Name, err)
		}
		if seen[s.Name] {
			return nil, errors.New("duplicate service")
		}
		seen[s.Name] = true
	}
	return c.Services, nil
}
