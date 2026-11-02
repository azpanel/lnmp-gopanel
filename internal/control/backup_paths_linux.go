//go:build linux

package control

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/azpanel/lnmp-gopanel/internal/backups"
)

func backupPathContains(parent, child string) bool {
	rel, err := filepath.Rel(parent, child)
	return err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}

func backupPathLabel(path string) string {
	runes := []rune(path)
	if len(runes) > 128 {
		return string(runes[:125]) + "…"
	}
	return path
}

// Resolve the longest existing prefix without creating any target directories.
// Lstat distinguishes a missing component from a dangling symbolic link.
func resolveBackupDestination(path string) (string, error) {
	var suffix []string
	for prefix := path; ; prefix = filepath.Dir(prefix) {
		info, err := os.Lstat(prefix)
		if err == nil {
			resolved, err := filepath.EvalSymlinks(prefix)
			if err != nil {
				return "", err
			}
			if !info.IsDir() {
				info, err = os.Stat(prefix)
			}
			if err != nil || !info.IsDir() {
				return "", fmt.Errorf("存储位置的已有路径必须是目录")
			}
			for i := len(suffix) - 1; i >= 0; i-- {
				resolved = filepath.Join(resolved, suffix[i])
			}
			if virtualBackupPath(resolved) {
				return "", fmt.Errorf("不支持虚拟文件系统存储位置")
			}
			return resolved, nil
		}
		if !os.IsNotExist(err) || prefix == "/" {
			return "", fmt.Errorf("存储位置无法解析")
		}
		suffix = append(suffix, filepath.Base(prefix))
	}
}

func inspectBackupPaths(r backups.PathsRequest) (backups.PathsResult, error) {
	result := backups.PathsResult{Errors: []backups.PathDiagnostic{}, Warnings: []backups.PathDiagnostic{}}
	if len(r.Files) > 100 || len(r.Destinations) > 100 {
		return result, backups.ErrInvalid
	}
	add := func(field string, index int, message string, control ...string) {
		for strings.HasPrefix(message, backups.ErrInvalid.Error()+"：") {
			message = strings.TrimPrefix(message, backups.ErrInvalid.Error()+"：")
		}
		item := backups.PathDiagnostic{Field: field, Index: index, Message: message}
		if len(control) > 0 {
			item.Control = control[0]
		}
		result.Errors = append(result.Errors, item)
	}
	type sourcePath struct {
		path      string
		directory bool
		index     int
	}
	sources := []sourcePath{}
	seenSources := map[string]bool{}
	for i, source := range r.Files {
		info, err := backupSourceInfo(source)
		if err != nil {
			add("files", i, err.Error())
			continue
		}
		resolved, err := filepath.EvalSymlinks(source.Path)
		if err != nil {
			add("files", i, "来源路径无法解析")
			continue
		}
		if seenSources[resolved] {
			add("files", i, "文件来源指向同一实际路径")
			continue
		}
		seenSources[resolved] = true
		for _, old := range sources {
			if (old.directory && backupPathContains(old.path, resolved)) || (info.IsDir() && backupPathContains(resolved, old.path)) {
				result.Warnings = append(result.Warnings, backups.PathDiagnostic{Field: "files", Index: i, Message: fmt.Sprintf("来源 %s 与 %s 存在父子范围重叠，可能重复备份", backupPathLabel(r.Files[old.index].Path), backupPathLabel(source.Path))})
				// One representative overlapping range per source bounds diagnostics.
				break
			}
		}
		sources = append(sources, sourcePath{resolved, info.IsDir(), i})
	}
	seen := map[string]int{}
	for i, d := range r.Destinations {
		if err := backups.ValidateDestination(d); err != nil {
			control := "path"
			if d.Type == "gdrive" {
				control = "folder_id"
				if d.Account == "" {
					control = "account"
				}
			}
			if d.Type == "rclone" && d.Remote == "" {
				control = "remote"
			}
			add("destinations", i, err.Error(), control)
			continue
		}
		key := backups.DestinationKey(d)
		if d.Type == "local" {
			resolved, err := resolveBackupDestination(d.Path)
			if err != nil {
				add("destinations", i, err.Error(), "path")
				continue
			}
			key = "local\x00" + resolved
			for _, source := range sources {
				if source.directory && backupPathContains(source.path, resolved) {
					add("destinations", i, fmt.Sprintf("本地存储位置 %s 不能位于备份来源 %s 内部或与其相同", backupPathLabel(d.Path), backupPathLabel(r.Files[source.index].Path)))
					break
				}
			}
		}
		if previous, ok := seen[key]; ok {
			add("destinations", i, fmt.Sprintf("与存储位置 %d 重复（指向同一目标）", previous+1))
		}
		seen[key] = i
	}
	return result, nil
}

func checkBackupAccounts(destinations []backups.Destination, home string) error {
	loaded := map[string]backups.AccountSnapshot{}
	for _, d := range destinations {
		if d.Type == "local" {
			continue
		}
		snapshot, ok := loaded[d.Type]
		if !ok {
			snapshot = inspectBackupAccounts(d.Type, home)
			loaded[d.Type] = snapshot
		}
		name := d.Account
		if d.Type == "rclone" {
			name = d.Remote
		}
		found := false
		for _, account := range snapshot.Items {
			if account.Name == name {
				found = true
				break
			}
		}
		if snapshot.Status != "ready" || !found {
			return fmt.Errorf("%w：存储位置的 %s 账户无法从本机配置识别，请重新选择账户或停用政策", backups.ErrInvalid, d.Type)
		}
	}
	return nil
}
