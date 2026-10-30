package logs

import (
	"fmt"
	"path/filepath"
	"strings"
	"unicode"
	"unicode/utf8"
)

// Keep the policy in the logs package so the unprivileged API and the root
// helper apply the same restrictions, including when called without the UI.
var sensitiveDirectories = []string{
	"/etc", "/root", "/proc", "/sys", "/dev", "/run", "/boot",
	"/var/backups", "/var/lib/lnmp-gopanel", "/var/lib/mysql", "/var/lib/mariadb",
	"/var/lib/postgresql", "/var/lib/redis", "/home/mysql",
	"/usr/local/nginx/conf", "/usr/local/php/etc", "/usr/local/mysql/etc",
	"/usr/local/mysql/var", "/usr/local/mysql/data", "/usr/local/mariadb/data",
	"/usr/local/mariadb/var", "/usr/local/pgsql/data", "/var/lib/mongodb",
	"/data/mysql", "/data/mariadb", "/data/postgresql", "/data/redis", "/data/mongodb",
	"/usr/local/redis/etc", "/usr/local/redis/var",
}

func ValidatePath(path string) error {
	if len(path) == 0 || len(path) > 2048 || !utf8.ValidString(path) || !filepath.IsAbs(path) ||
		filepath.Clean(path) != path || strings.Contains(path, "\\") || strings.ContainsFunc(path, unicode.IsControl) {
		return fmt.Errorf("%w：请输入规范的绝对文件路径（最多 2048 字节）", ErrInvalid)
	}
	for _, dir := range sensitiveDirectories {
		if path == dir || strings.HasPrefix(path, dir+"/") {
			return fmt.Errorf("%w：禁止读取敏感目录中的文件", ErrInvalid)
		}
	}
	for _, part := range strings.Split(strings.ToLower(path), "/") {
		switch part {
		case ".ssh", ".aws", ".azure", ".kube", ".docker", ".gnupg", ".pki", ".config", ".git", ".svn", ".hg":
			return fmt.Errorf("%w：禁止读取凭据或版本库目录", ErrInvalid)
		}
		if strings.HasPrefix(part, ".env") {
			return fmt.Errorf("%w：禁止读取环境凭据文件", ErrInvalid)
		}
	}
	name := strings.ToLower(filepath.Base(path))
	// Backups of a credential remain credentials.
	for {
		previous := name
		name = strings.TrimSuffix(name, "~")
		for _, suffix := range []string{".bak", ".old", ".orig", ".save", ".backup"} {
			name = strings.TrimSuffix(name, suffix)
		}
		if name == previous {
			break
		}
	}
	switch name {
	case "shadow", "shadow-", "gshadow", "gshadow-", "passwd", "passwd-", "sudoers",
		"id_rsa", "id_dsa", "id_ecdsa", "id_ed25519", "authorized_keys", "known_hosts",
		"credentials", "credentials.json", ".netrc", ".npmrc", ".pypirc", ".my.cnf",
		".git-credentials", ".gitconfig", ".pgpass", ".htpasswd", ".htdigest", ".dockercfg", ".s3cfg",
		".bash_history", ".zsh_history", ".python_history", ".mysql_history", ".rediscli_history", ".psql_history",
		"redis.conf", "my.cnf", "php.ini", "kubeconfig",
		"wp-config.php", "config.php", "settings.php", "config.yaml", "config.yml", "config.json",
		"application.yml", "application.yaml", "auth.json", "services.yaml",
		"custom-logs.json", "screenshot-settings.json", "screenshots.key", "dump.rdb", "appendonly.aof":
		return fmt.Errorf("%w：禁止读取敏感配置或凭据文件", ErrInvalid)
	}
	for _, suffix := range []string{".key", ".pem", ".crt", ".cer", ".p12", ".pfx", ".jks", ".keystore", ".kdbx", ".ibd", ".frm", ".myd", ".myi", ".sqlite", ".sqlite3", ".db", ".sql", ".rdb", ".aof"} {
		if strings.HasSuffix(name, suffix) {
			return fmt.Errorf("%w：禁止读取证书、密钥或数据库文件", ErrInvalid)
		}
	}
	return nil
}

func ValidPath(path string) bool { return ValidatePath(path) == nil }

// ValidateLogPath applies the extension restriction at every read boundary.
// ValidatePath remains available for loading and removing legacy catalog entries.
func ValidateLogPath(path string) error {
	if err := ValidatePath(path); err != nil {
		return err
	}
	switch strings.ToLower(filepath.Ext(path)) {
	case ".log", ".txt":
		return nil
	default:
		return fmt.Errorf("%w：自定义日志仅支持 .log 或 .txt 文件", ErrInvalid)
	}
}

// BuiltinPath describes auto-discovery only; other permitted paths must first
// be explicitly added to the server's custom catalog.
func BuiltinPath(path string) bool {
	if !ValidPath(path) {
		return false
	}
	if path == MySQL || path == PHP {
		return true
	}
	return (filepath.Dir(path) == Directory || filepath.Dir(path) == ManagedDirectory) && strings.HasSuffix(filepath.Base(path), ".log")
}
