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

func TestLogExtensionPolicy(t *testing.T) {
	for _, path := range []string{"/srv/app.log", "/srv/app.txt", "/srv/app.LOG", "/srv/app.TxT", MySQL, PHP} {
		if err := ValidateLogPath(path); err != nil {
			t.Errorf("valid log path %q: %v", path, err)
		}
	}
	for _, path := range []string{
		"/var/log/syslog", "/srv/database.php", "/srv/app.log.1", "/srv/app.txt.bak",
		"/srv/app.log~", "/srv/app.log/secret", "/srv/app.log.php", "/srv/app.txt ",
		"/etc/secret.txt", "/root/private.log", "/srv/.env.txt", "/srv/.ssh/debug.log",
	} {
		for _, mode := range []string{"stat", "tail"} {
			if err := Validate(Request{Path: path, Mode: mode}); !errors.Is(err, ErrInvalid) {
				t.Errorf("invalid log path %q (%s) accepted: %v", path, mode, err)
			}
		}
	}
}
