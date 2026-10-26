package control

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/azpanel/lnmp-gopanel/internal/config"
)

func executable(p string) bool {
	s, e := os.Stat(p)
	return e == nil && s.Mode().IsRegular() && s.Mode().Perm()&0111 != 0
}
func firstExecutable(paths ...string) string {
	for _, p := range paths {
		if executable(p) {
			return p
		}
	}
	return ""
}
func Detect(ctx context.Context) []config.Service {
	out := []config.Service{}
	seen := map[string]bool{}
	systemctl := firstExecutable("/usr/bin/systemctl", "/bin/systemctl")
	_, systemd := os.Stat("/run/systemd/system")
	candidates := []struct {
		name                     string
		units, scripts, binaries []string
	}{
		{"nginx", []string{"nginx.service"}, []string{"nginx"}, []string{"/usr/local/nginx/sbin/nginx"}},
		{"php-fpm", []string{"php-fpm.service", "php.service"}, []string{"php-fpm"}, []string{"/usr/local/php/sbin/php-fpm"}},
		// Prefer MariaDB's canonical unit before following common mysql aliases.
		{"mariadb", []string{"mariadb.service"}, []string{"mariadb"}, []string{"/usr/local/mariadb/bin/mariadbd", "/usr/local/mariadb/bin/mysqld"}},
		{"mysql", []string{"mysql.service", "mysqld.service"}, []string{"mysql", "mysqld"}, []string{"/usr/local/mysql/bin/mysqld"}},
		{"redis", []string{"redis.service", "redis-server.service"}, []string{"redis", "redis-server"}, nil},
	}
	for _, c := range candidates {
		s := config.Service{Name: c.name}
		identity := ""
		if c.name == "nginx" {
			s.CheckProgram = firstExecutable("/usr/local/nginx/sbin/nginx", "/usr/sbin/nginx")
		}
		if systemd == nil && systemctl != "" {
			for _, unit := range c.units {
				timeout, cancel := context.WithTimeout(ctx, 2*time.Second)
				v, e := RunCommand(timeout, systemctl, "show", "--property=LoadState,Id,FragmentPath,ActiveState", unit)
				cancel()
				if e != nil {
					continue
				}
				properties := map[string]string{}
				for _, line := range strings.Split(v, "\n") {
					k, val, ok := strings.Cut(line, "=")
					if ok {
						properties[k] = val
					}
				}
				initAvailable := false
				for _, script := range c.scripts {
					if executable(filepath.Join("/etc/init.d", script)) {
						initAvailable = true
						break
					}
				}
				processState := "unknown"
				if nativeUnit(properties) && initAvailable && (properties["ActiveState"] == "failed" || properties["ActiveState"] == "inactive") {
					if status, handled := NativeStatus(ctx, config.Service{Name: c.name, Manager: "init"}); handled {
						processState = status.State
					}
				}
				if useNativeUnit(properties, initAvailable, processState) {
					s.Manager = "systemd"
					s.Program = systemctl
					s.Target = unit
					identity = "systemd:" + properties["Id"]
					if properties["Id"] == "" {
						identity = "systemd:" + unit
					}
					break
				}
			}
		}
		if s.Manager == "" {
			for _, script := range c.scripts {
				p := filepath.Join("/etc/init.d", script)
				if executable(p) {
					s.Manager = "init"
					s.Program = p
					resolved, _ := filepath.EvalSymlinks(p)
					identity = "init:" + resolved
					break
				}
			}
		}
		if s.Manager == "" && firstExecutable(c.binaries...) != "" {
			if p := firstExecutable("/usr/bin/lnmp", "/bin/lnmp", "/usr/local/bin/lnmp"); p != "" {
				s.Manager = "lnmp"
				s.Program = p
				s.Target = c.name
				identity = "lnmp:" + c.name
			}
		}
		// LNMP MariaDB installations may expose only /etc/init.d/mysql.
		if s.Name == "mysql" && s.Manager == "init" && firstExecutable("/usr/local/mariadb/bin/mariadbd", "/usr/local/mariadb/bin/mysqld") != "" && firstExecutable("/usr/local/mysql/bin/mysqld") == "" {
			s.Name = "mariadb"
		}
		if s.Manager != "" && !seen[identity] && !seen["name:"+s.Name] {
			seen[identity] = true
			seen["name:"+s.Name] = true
			out = append(out, s)
		}
	}
	return out
}

// systemd generates wrappers for SysV scripts. Those are not evidence that
// systemd owns a daemon that LNMP started directly through /etc/init.d.
func nativeUnit(properties map[string]string) bool {
	fragment := properties["FragmentPath"]
	return properties["LoadState"] == "loaded" && fragment != "" && !strings.HasPrefix(fragment, "/run/systemd/generator")
}

// A failed/inactive native unit does not own a daemon already running outside
// that unit. Preserve LNMP's working init path rather than report it stopped or
// start a duplicate instance through systemd.
func useNativeUnit(properties map[string]string, initAvailable bool, processState string) bool {
	if !nativeUnit(properties) {
		return false
	}
	state := properties["ActiveState"]
	return !(initAvailable && processState == "running" && (state == "failed" || state == "inactive"))
}
