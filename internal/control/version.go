package control

import (
	"context"
	"errors"
	"fmt"
	"os"
	"regexp"
	"time"
)

// Only fixed, known daemon executables may be used for version probes.
func serviceBinaries(name string) []string {
	switch name {
	case "nginx":
		return []string{"/usr/local/nginx/sbin/nginx", "/usr/sbin/nginx"}
	case "php-fpm":
		return []string{"/usr/local/php/sbin/php-fpm", "/usr/sbin/php-fpm", "/usr/sbin/php-fpm8.5", "/usr/sbin/php-fpm8.4", "/usr/sbin/php-fpm8.3", "/usr/sbin/php-fpm8.2"}
	case "mysql":
		return []string{"/usr/local/mysql/bin/mysqld", "/usr/sbin/mysqld"}
	case "mariadb":
		return []string{"/usr/local/mariadb/bin/mariadbd", "/usr/local/mariadb/bin/mysqld", "/usr/sbin/mariadbd", "/usr/sbin/mysqld"}
	case "redis":
		return []string{"/usr/local/redis/bin/redis-server", "/usr/bin/redis-server", "/usr/local/bin/redis-server"}
	}
	return nil
}

var versionPatterns = map[string]*regexp.Regexp{
	"nginx":   regexp.MustCompile(`nginx/([0-9]+\.[0-9]+\.[0-9]+)`),
	"php-fpm": regexp.MustCompile(`PHP\s+([0-9]+\.[0-9]+\.[0-9]+)`),
	"mysql":   regexp.MustCompile(`(?i)Ver\s+([0-9]+\.[0-9]+\.[0-9]+)`),
	"mariadb": regexp.MustCompile(`(?i)Ver\s+([0-9]+\.[0-9]+\.[0-9]+)`),
	"redis":   regexp.MustCompile(`Redis server v=([0-9]+\.[0-9]+\.[0-9]+)`),
}

func parseInstalledVersion(name, output string) (string, error) {
	pattern := versionPatterns[name]
	if pattern != nil {
		if match := pattern.FindStringSubmatch(output); len(match) == 2 {
			return match[1], nil
		}
	}
	return "", fmt.Errorf("无法识别服务版本号")
}

func InstalledVersion(ctx context.Context, name string, run CommandRunner) (string, error) {
	program, err := versionProgram(serviceBinaries(name), os.Stat)
	if err != nil {
		return "", err
	}
	// Production probes run as the unprivileged panel user, including LNMP
	// binaries owned by their service account. Never elevate version queries.
	if os.Geteuid() == 0 {
		if err := TrustedPath(program); err != nil {
			return "", fmt.Errorf("服务可执行文件权限不安全")
		}
	}
	flag := "--version"
	if name == "nginx" || name == "php-fpm" {
		flag = "-v"
	}
	output, err := run(ctx, program, flag)
	if err != nil {
		if errors.Is(err, os.ErrPermission) {
			return "", fmt.Errorf("服务程序或安装目录权限不足，面板账号无法访问")
		}
		return "", fmt.Errorf("服务版本读取失败")
	}
	return parseInstalledVersion(name, output)
}

func versionProgram(paths []string, stat func(string) (os.FileInfo, error)) (string, error) {
	denied := false
	for _, path := range paths {
		info, err := stat(path)
		if errors.Is(err, os.ErrPermission) {
			denied = true
		}
		if err == nil && info.Mode().IsRegular() && info.Mode().Perm()&0111 != 0 {
			return path, nil
		}
	}
	if denied {
		return "", fmt.Errorf("服务程序或安装目录权限不足，面板账号无法访问")
	}
	return "", fmt.Errorf("未找到服务可执行文件")
}

func (h Helper) Version(ctx context.Context, name string) (string, error) {
	return InstalledVersion(ctx, name, RunCommand)
}

func (m *Manager) refreshVersion(ctx context.Context, name string) {
	reader, ok := m.executor.(interface {
		Version(context.Context, string) (string, error)
	})
	if !ok {
		return
	}
	m.mu.RLock()
	view := m.views[name]
	m.mu.RUnlock()
	retry := time.Hour
	if view.VersionError != "" {
		retry = time.Minute
	}
	if time.Since(view.VersionCheckedAt) < retry {
		return
	}
	ctx, cancel := context.WithTimeout(ctx, 8*time.Second)
	defer cancel()
	version, err := reader.Version(ctx, name)
	m.mu.Lock()
	defer m.mu.Unlock()
	view = m.views[name]
	view.VersionCheckedAt = time.Now()
	view.VersionError = ""
	if err != nil {
		view.VersionError = err.Error()
	} else {
		view.Version = version
	}
	m.views[name] = view
}
