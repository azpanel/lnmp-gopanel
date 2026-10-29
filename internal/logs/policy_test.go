package logs

import (
	"errors"
	"strings"
	"testing"
)

func TestCustomPathPolicy(t *testing.T) {
	for _, path := range []string{
		Directory + "/a.log", MySQL, PHP, "/var/log/syslog", "/srv/app/logs/output",
		"/opt/app/logs/server error.log", Directory + "/nested/a.log", Directory + "/a.log.1",
		"/etc-logs/application.log", "/var/lib/mysql-logs/server.log",
	} {
		if err := ValidatePath(path); err != nil {
			t.Errorf("valid path %q: %v", path, err)
		}
	}
	for _, path := range []string{
		"", "relative.log", "/opt/../etc/shadow", "/opt//error.log", "/opt/error.log/", "/opt/error\x00.log",
		"/opt/error\n.log", "/opt/error\\file.log", "/" + strings.Repeat("a", 2048),
		"/etc/shadow", "/etc/lnmp-gopanel/config.yaml", "/root/secret.log", "/proc/self/environ",
		"/sys/kernel/security/keys", "/dev/sda", "/run/secrets/token", "/var/backups/passwords.log",
		"/var/lib/mysql/table.ibd", "/var/lib/lnmp-gopanel/state", "/home/mysql/data.log",
		"/usr/local/nginx/conf/ssl/site/key", "/usr/local/php/etc/php.ini",
		"/home/user/.ssh/id_rsa", "/srv/app/.aws/credentials", "/srv/app/.git/config",
		"/srv/app/.env", "/srv/app/.env.production", "/srv/app/credentials.json",
		"/srv/app/secret.KEY", "/srv/app/cert.pem", "/srv/app/wp-config.php", "/srv/app/backup.sql",
		"/srv/app/screenshots.key", "/srv/app/custom-logs.json", "/srv/app/config.yaml",
		"/home/user/.bash_history", "/srv/app/.git-credentials", "/srv/app/credentials.json.bak",
		"/srv/app/key.pem.old~", "/data/mysql/error.log", "/srv/app/redis.conf",
	} {
		if err := ValidatePath(path); !errors.Is(err, ErrInvalid) {
			t.Errorf("sensitive or malformed path %q accepted: %v", path, err)
		}
	}
}
