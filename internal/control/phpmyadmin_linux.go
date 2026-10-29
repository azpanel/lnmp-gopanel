//go:build linux

package control

import (
	"errors"
	"fmt"
	"io"
	"net"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/azpanel/lnmp-gopanel/internal/components"
	"golang.org/x/sys/unix"
)

const phpMyAdminSecurity = "/usr/local/etc/lnmp/security.conf"
const phpMyAdminDefaultRoot = "/home/wwwroot/default"

type phpMyAdminLocation struct{ Mode, Directory, Root, IPv4 string }

// Read printf %q's plain/backslash-escaped words as data, never as shell code.
func securityLiteral(raw string) (string, error) {
	if raw == "''" {
		return "", nil
	}
	var out strings.Builder
	escaped := false
	for _, ch := range raw {
		if ch < 32 || ch == 127 {
			return "", errors.New("保护配置包含控制字符")
		}
		if escaped {
			out.WriteRune(ch)
			escaped = false
			continue
		}
		if ch == '\\' {
			escaped = true
			continue
		}
		if strings.ContainsRune(" \t'\"$`;&|<>(){}[]*?!#~", ch) {
			return "", errors.New("保护配置不是受支持的字面量")
		}
		out.WriteRune(ch)
	}
	if escaped || raw == "" {
		return "", errors.New("保护配置字面量无效")
	}
	return out.String(), nil
}

func parsePHPMyAdminLocation(data []byte) (phpMyAdminLocation, error) {
	var location phpMyAdminLocation
	fields := map[string]*string{
		"Installed_PhpMyAdmin_Mode": &location.Mode,
		"Installed_PhpMyAdmin_Dir":  &location.Directory,
		"Installed_Website_Dir":     &location.Root,
		"Installed_Server_IP":       &location.IPv4,
	}
	seen := map[string]bool{}
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		key, raw, ok := strings.Cut(line, "=")
		target := fields[key]
		if !ok || target == nil || seen[key] {
			return location, errors.New("phpMyAdmin 保护配置字段无效或重复")
		}
		value, err := securityLiteral(raw)
		if err != nil {
			return location, err
		}
		*target, seen[key] = value, true
	}
	validDir := (location.Mode == "basic" && location.Directory == "phpmyadmin") || (location.Mode == "random" && regexp.MustCompile(`^[a-f0-9]{32}$`).MatchString(location.Directory))
	ip := net.ParseIP(location.IPv4)
	if len(seen) != 4 || !validDir || !filepath.IsAbs(location.Root) || filepath.Clean(location.Root) != location.Root || location.Root == "/" || ip == nil || ip.To4() == nil || ip.String() != location.IPv4 || ip.IsUnspecified() || ip.Equal(net.IPv4bcast) {
		return location, errors.New("phpMyAdmin 保护配置中的路径或 IPv4 无效")
	}
	return location, nil
}

func readPHPMyAdminData(root, name string) ([]byte, error) {
	// Root confines symlink resolution to this tree. O_NONBLOCK prevents a replaced
	// version file/FIFO from blocking the privileged inventory worker.
	directory, err := os.OpenRoot(root)
	if err != nil {
		return nil, err
	}
	defer directory.Close()
	f, err := directory.OpenFile(name, os.O_RDONLY|unix.O_NOFOLLOW|unix.O_NONBLOCK, 0)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() || info.Size() > 65536 {
		return nil, errors.New("phpMyAdmin 元数据文件类型或大小无效")
	}
	data, err := io.ReadAll(io.LimitReader(f, 65537))
	if err != nil {
		return nil, err
	}
	if len(data) > 65536 {
		return nil, errors.New("phpMyAdmin 元数据文件过大")
	}
	return data, nil
}

func loadPHPMyAdminLocation(path string, trust func(string) error) (phpMyAdminLocation, error) {
	if err := trust(path); err != nil {
		return phpMyAdminLocation{}, fmt.Errorf("phpMyAdmin 保护配置不可读取或权限不安全：%w", err)
	}
	data, err := readPHPMyAdminData(filepath.Dir(path), filepath.Base(path))
	if err != nil {
		return phpMyAdminLocation{}, err
	}
	return parsePHPMyAdminLocation(data)
}

func (l phpMyAdminLocation) accessURL() string {
	return (&url.URL{Scheme: "https", Host: l.IPv4, Path: "/" + l.Directory + "/"}).String()
}

func probePHPMyAdminAt(security, defaultRoot string, trust func(string) error) components.Item {
	now := time.Now()
	item := components.Item{Name: "phpmyadmin", Status: "missing", CheckedAt: &now}
	root, dir := defaultRoot, "phpmyadmin"
	var location *phpMyAdminLocation
	if _, err := os.Lstat(security); err == nil {
		value, err := loadPHPMyAdminLocation(security, trust)
		if err != nil {
			item.Status, item.Error = "failed", err.Error()
			return item
		}
		root, dir, location = value.Root, value.Directory, &value
	} else if !os.IsNotExist(err) {
		item.Status, item.Error = "failed", "无法读取 phpMyAdmin 保护配置"
		return item
	}
	item.Path = filepath.Join(root, dir)
	info, err := os.Lstat(item.Path)
	if os.IsNotExist(err) {
		if location != nil {
			item.Status, item.Error = "failed", "已记录的 phpMyAdmin 目录不存在"
		}
		return item
	}
	if err != nil || !info.IsDir() {
		item.Status, item.Error = "failed", "phpMyAdmin 安装目录无效或不可读取"
		return item
	}
	item.Status = "failed"
	// Reading literals permits www-owned application files without executing PHP as root.
	for _, filename := range []string{"libraries/classes/Version.php", "libraries/Config.php", "libraries/Config.class.php"} {
		data, err := readPHPMyAdminData(item.Path, filename)
		if os.IsNotExist(err) {
			continue
		}
		if err != nil {
			item.Error = "phpMyAdmin 版本文件不可读取或不安全"
			return item
		}
		patterns := []string{`(?m)^\s*(?:public\s+)?const VERSION\s*=\s*['"]([0-9]+\.[0-9]+\.[0-9]+(?:[a-zA-Z0-9.+-]*))['"]`, `(?m)^\s*(?:public\s+)?\$version\s*=\s*['"]([0-9]+\.[0-9]+\.[0-9]+(?:[a-zA-Z0-9.+-]*))['"]`}
		for _, pattern := range patterns {
			if match := regexp.MustCompile(pattern).FindSubmatch(data); len(match) == 2 {
				item.Version = string(match[1])
				break
			}
		}
		if item.Version != "" {
			break
		}
	}
	if item.Version == "" {
		item.Error = "无法识别 phpMyAdmin 版本号"
		return item
	}
	item.Status = "ready"
	if location != nil {
		item.AccessURL = location.accessURL()
	}
	return item
}

func probePHPMyAdmin() components.Item {
	return probePHPMyAdminAt(phpMyAdminSecurity, phpMyAdminDefaultRoot, TrustedPath)
}
