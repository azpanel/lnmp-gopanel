//go:build linux

package control

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"unicode"

	"github.com/azpanel/lnmp-gopanel/internal/backups"
	"golang.org/x/sys/unix"
)

const backupAccountLimit = 1000
const backupConfigLimit = 1 << 20

var errBackupAccountConfig = errors.New("账户配置无法读取，请检查文件格式和权限")

// Configs are read by the root helper, never by the web process. Errors are
// deliberately static: parser diagnostics can contain credentials from a line.
func inspectBackupAccounts(name, home string) backups.AccountSnapshot {
	result := backups.AccountSnapshot{Status: "missing", Items: []backups.Account{}}
	var err error
	switch name {
	case "gdrive":
		err = inspectGdriveAccounts(home, &result)
	case "rclone":
		err = inspectRcloneAccounts(filepath.Join(home, ".config/rclone/rclone.conf"), &result)
	}
	if err != nil {
		result.Status, result.Error = "failed", errBackupAccountConfig.Error()
		result.Items = []backups.Account{}
	} else if len(result.Items) > 0 {
		result.Status = "ready"
	}
	return result
}

func backupAccountText(value string, max int) bool {
	return value != "" && len(value) <= max && strings.IndexFunc(value, unicode.IsControl) == -1
}

func backupConfigInfo(path string) (os.FileInfo, error) {
	info, err := os.Lstat(path)
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() || info.Size() > backupConfigLimit || TrustedPath(path) != nil {
		return nil, errBackupAccountConfig
	}
	return info, nil
}

func readBackupAccountConfig(path string) ([]byte, error) {
	if _, err := backupConfigInfo(path); err != nil {
		return nil, err
	}
	fd, err := unix.Open(path, unix.O_RDONLY|unix.O_CLOEXEC|unix.O_NOFOLLOW|unix.O_NONBLOCK, 0)
	if err != nil {
		return nil, err
	}
	f := os.NewFile(uintptr(fd), path)
	defer f.Close()
	info, err := f.Stat()
	if err != nil || !info.Mode().IsRegular() {
		return nil, errBackupAccountConfig
	}
	data, err := io.ReadAll(io.LimitReader(f, backupConfigLimit+1))
	if err != nil || len(data) > backupConfigLimit {
		return nil, errBackupAccountConfig
	}
	return data, nil
}

func inspectGdriveAccounts(home string, result *backups.AccountSnapshot) error {
	base := filepath.Join(home, ".config/gdrive3")
	info, err := os.Lstat(base)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil || !info.IsDir() || TrustedPath(base) != nil {
		return errBackupAccountConfig
	}
	dir, err := os.Open(base)
	if err != nil {
		return err
	}
	defer dir.Close()
	entries, err := dir.ReadDir(backupAccountLimit + 2) // accounts plus account.json
	if err != nil && err != io.EOF || len(entries) > backupAccountLimit+1 {
		return errBackupAccountConfig
	}
	var current struct {
		Current string `json:"current"`
	}
	data, err := readBackupAccountConfig(filepath.Join(base, "account.json"))
	if err != nil && !os.IsNotExist(err) {
		return err
	}
	if err == nil && json.Unmarshal(data, &current) != nil {
		return errBackupAccountConfig
	}
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		name := entry.Name()
		if !backupAccountText(name, 256) {
			return errBackupAccountConfig
		}
		info, err := backupConfigInfo(filepath.Join(base, name, "tokens.json"))
		if os.IsNotExist(err) {
			continue
		}
		if err != nil {
			return err
		}
		if info.Size() > 0 {
			result.Items = append(result.Items, backups.Account{Name: name, Type: "drive", Current: name == current.Current})
		}
	}
	sort.Slice(result.Items, func(i, j int) bool { return result.Items[i].Name < result.Items[j].Name })
	return nil
}

func inspectRcloneAccounts(path string, result *backups.AccountSnapshot) error {
	data, err := readBackupAccountConfig(path)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	// Only section names and the exact "type" key are retained. In particular,
	// do not marshal arbitrary INI fields or invoke config dump/show commands.
	scanner := bufio.NewScanner(bytes.NewReader(data))
	scanner.Buffer(make([]byte, 4096), backupConfigLimit)
	seen := map[string]bool{}
	var account *backups.Account
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") || strings.HasPrefix(line, ";") {
			continue
		}
		if line == "RCLONE_ENCRYPT_V0:" && account == nil {
			result.Status = "encrypted"
			return nil
		}
		if strings.HasPrefix(line, "[") {
			if !strings.HasSuffix(line, "]") {
				return errBackupAccountConfig
			}
			name := strings.TrimSpace(line[1 : len(line)-1])
			if !backupAccountText(name, 256) || seen[name] || len(result.Items) >= backupAccountLimit {
				return errBackupAccountConfig
			}
			seen[name] = true
			result.Items = append(result.Items, backups.Account{Name: name})
			account = &result.Items[len(result.Items)-1]
			continue
		}
		key, value, ok := strings.Cut(line, "=")
		if !ok || account == nil {
			return errBackupAccountConfig
		}
		if strings.TrimSpace(key) == "type" {
			value = strings.TrimSpace(value)
			if !backupAccountText(value, 64) || account.Type != "" {
				return errBackupAccountConfig
			}
			account.Type = value
		}
	}
	if scanner.Err() != nil {
		return errBackupAccountConfig
	}
	for _, item := range result.Items {
		if item.Type == "" {
			return errBackupAccountConfig
		}
	}
	sort.Slice(result.Items, func(i, j int) bool { return result.Items[i].Name < result.Items[j].Name })
	return nil
}
