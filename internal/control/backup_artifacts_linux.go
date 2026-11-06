//go:build linux

package control

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/azpanel/lnmp-gopanel/internal/backups"
)

const backupArtifactLimit = 1000
const backupArtifactScanLimit = 100000
const backupArtifactOutputLimit = 8 << 20

// gdrive passes every table through TabWriter, which expands tab separators
// into padding. A printable separator survives that formatting unchanged.
const backupGdriveFieldSeparator = "|"

var backupArchivePattern = regexp.MustCompile(`^([a-f0-9]{32})-(databases|files)-([0-9]{8}T[0-9]{6}Z)-([a-f0-9]{32})\.(zip|7z)$`)
var backupSizeDisplay = regexp.MustCompile(`^[0-9]+(?:\.[0-9]+)?\s*(?:B|[KMGTPE]i?B)$`)
var errArtifactOutput = errors.New("备份文件列表格式无效，请检查存储组件版本")

func parseBackupArtifactName(policyID, name string) (backups.ArtifactFile, bool) {
	match := backupArchivePattern.FindStringSubmatch(name)
	if match == nil || match[1] != policyID {
		return backups.ArtifactFile{}, false
	}
	date, err := time.Parse("20060102T150405Z", match[3])
	if err != nil {
		return backups.ArtifactFile{}, false
	}
	return backups.ArtifactFile{Name: name, Object: name, Category: match[2], BackupAt: date}, true
}

func (e backupExecutor) listArtifacts(ctx context.Context, q backups.ArtifactQuery) (backups.ArtifactList, error) {
	result := backups.ArtifactList{Items: []backups.ArtifactFile{}}
	if err := backups.ValidateArtifactQuery(q); err != nil {
		return result, err
	}
	p, err := e.store.get(q.ID)
	if err != nil {
		return result, err
	}
	if p.Revision != q.Revision {
		return result, backups.ErrConflict
	}
	if *q.DestinationIndex >= len(p.Destinations) {
		return result, backups.ErrInvalid
	}
	destination := p.Destinations[*q.DestinationIndex]
	if err := backups.ValidateDestination(destination); err != nil {
		return result, err
	}
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	switch destination.Type {
	case "local":
		err = listLocalBackupArtifacts(ctx, q.ID, destination.Path, &result)
	case "rclone":
		err = e.listRcloneArtifacts(ctx, q.ID, destination, &result)
	case "gdrive":
		err = e.listGdriveArtifacts(ctx, q.ID, destination, &result)
	}
	if ctx.Err() != nil {
		return result, errors.New("读取备份文件超时或已取消，请稍后重试")
	}
	if err != nil {
		return result, err
	}
	// Do not present an old destination as the current configuration after a
	// slow remote request. No policy lock is held across filesystem/network I/O.
	current, err := e.store.get(q.ID)
	if err != nil {
		return result, err
	}
	if current.Revision != q.Revision {
		return result, backups.ErrConflict
	}
	sort.Slice(result.Items, func(i, j int) bool {
		a, b := result.Items[i], result.Items[j]
		if !a.BackupAt.Equal(b.BackupAt) {
			return a.BackupAt.After(b.BackupAt)
		}
		if a.Name != b.Name {
			return a.Name < b.Name
		}
		return a.Object < b.Object
	})
	result.CheckedAt = e.now().UTC()
	if result.Truncated {
		result.Warning = "列表达到查询上限，仅展示部分匹配的备份文件。"
	}
	return result, nil
}
func addBackupArtifact(result *backups.ArtifactList, item backups.ArtifactFile) bool {
	if len(result.Items) >= backupArtifactLimit {
		result.Truncated = true
		return false
	}
	result.Items = append(result.Items, item)
	return true
}
func listLocalBackupArtifacts(ctx context.Context, id, location string, result *backups.ArtifactList) error {
	resolved, err := resolveBackupDestination(location)
	if err != nil {
		return errors.New("本地存储位置无法解析")
	}
	root, err := os.OpenRoot(resolved)
	if os.IsNotExist(err) {
		result.Warning = "存储目录尚不存在。"
		return nil
	}
	if err != nil {
		return errors.New("本地备份目录不可读取，请检查目录权限")
	}
	defer root.Close()
	directory, err := root.Open(".")
	if err != nil {
		return errors.New("本地备份目录不可读取")
	}
	defer directory.Close()
	seen := 0
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		entries, readErr := directory.ReadDir(128)
		if readErr != nil && !errors.Is(readErr, io.EOF) {
			return errors.New("本地备份目录读取失败")
		}
		for _, entry := range entries {
			seen++
			if seen > backupArtifactScanLimit {
				result.Truncated = true
				return nil
			}
			item, ok := parseBackupArtifactName(id, entry.Name())
			if !ok {
				continue
			}
			// Lstat through the opened root never follows a candidate file symlink.
			info, err := root.Lstat(entry.Name())
			if os.IsNotExist(err) {
				continue
			}
			if err != nil {
				return errors.New("部分备份文件信息无法读取，请刷新重试")
			}
			if !info.Mode().IsRegular() {
				continue
			}
			size := info.Size()
			item.Bytes = &size
			if !addBackupArtifact(result, item) {
				return nil
			}
		}
		if errors.Is(readErr, io.EOF) {
			return nil
		}
	}
}

// Drain only bounded data, then cancel/reap the child on truncation or malformed
// output. Raw stdout/stderr never escape into a public error or a run log.
func runBackupArtifactCommand(ctx context.Context, tool string, env, args []string, parse func(io.Reader) (bool, error)) (bool, error) {
	childCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	cmd := exec.CommandContext(childCtx, tool, args...)
	configureProcess(cmd)
	cmd.Env = env
	cmd.Stderr = &limitedBuffer{}
	out, err := cmd.StdoutPipe()
	if err != nil {
		return false, errArtifactOutput
	}
	if err = cmd.Start(); err != nil {
		out.Close()
		return false, errors.New("备份存储组件无法启动")
	}
	limited := &io.LimitedReader{R: out, N: backupArtifactOutputLimit}
	truncated, parseErr := parse(limited)
	if limited.N == 0 {
		truncated = true
		parseErr = nil
	}
	if truncated || parseErr != nil {
		cancel()
	}
	out.Close()
	waitErr := cmd.Wait()
	if ctx.Err() != nil {
		return false, ctx.Err()
	}
	// Exit errors (including permission failures with empty stdout) are not an
	// empty directory. Deliberately terminated children may return partial data.
	if waitErr != nil && !truncated && parseErr == nil {
		return false, errors.New("备份存储查询失败，请检查账户授权、目录权限及网络")
	}
	if parseErr != nil {
		return false, errArtifactOutput
	}
	return truncated, nil
}

func parseRcloneArtifacts(reader io.Reader, id string, result *backups.ArtifactList) (bool, error) {
	decoder := json.NewDecoder(reader)
	token, err := decoder.Token()
	if err != nil || token != json.Delim('[') {
		return false, errArtifactOutput
	}
	seen := 0
	for decoder.More() {
		var file struct {
			Name   string
			Path   string
			Size   *int64
			IsDir  bool
			IsLink bool
		}
		if err := decoder.Decode(&file); err != nil {
			return false, err
		}
		seen++
		if seen > backupArtifactScanLimit {
			return true, nil
		}
		item, ok := parseBackupArtifactName(id, file.Name)
		if !ok || file.IsDir || file.IsLink || file.Path != file.Name {
			continue
		}
		if file.Size != nil && *file.Size >= 0 {
			item.Bytes = file.Size
		}
		if !addBackupArtifact(result, item) {
			return true, nil
		}
	}
	if _, err := decoder.Token(); err != nil {
		return false, err
	}
	var extra any
	if err := decoder.Decode(&extra); err != io.EOF {
		return false, errArtifactOutput
	}
	return false, nil
}
func (e backupExecutor) listRcloneArtifacts(ctx context.Context, id string, d backups.Destination, result *backups.ArtifactList) error {
	tool := e.executable("rclone")
	if tool == "" {
		return errors.New("缺少可信的 rclone，请先安装组件")
	}
	if err := checkBackupAccounts([]backups.Destination{d}, e.accountHome()); err != nil {
		return err
	}
	config := filepath.Join(e.accountHome(), ".config/rclone/rclone.conf")
	args := []string{"lsjson", "--config", config, "--retries", "1", "--low-level-retries", "2", "--files-only", "--max-depth", "1", "--no-mimetype", "--no-modtime", "--include", id + "-*", "--", d.Remote + ":" + d.Path}
	truncated, err := runBackupArtifactCommand(ctx, tool, backupCommandEnvironment(), args, func(r io.Reader) (bool, error) { return parseRcloneArtifacts(r, id, result) })
	result.Truncated = result.Truncated || truncated
	return err
}

func parseGdriveArtifacts(reader io.Reader, id string, result *backups.ArtifactList) (bool, error) {
	scanner := bufio.NewScanner(reader)
	scanner.Buffer(make([]byte, 4096), 64*1024)
	seen := 0
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		fields := strings.Split(line, backupGdriveFieldSeparator)
		if len(fields) != 5 {
			return false, errArtifactOutput
		}
		for i := range fields {
			fields[i] = strings.TrimSpace(fields[i])
		}
		seen++
		if seen > backupArtifactLimit {
			return true, nil
		}
		item, ok := parseBackupArtifactName(id, fields[1])
		if !ok || fields[2] != "regular" || !backupDriveID.MatchString(fields[0]) {
			continue
		}
		item.Object = fields[0]
		if len(fields[3]) <= 32 && backupSizeDisplay.MatchString(fields[3]) {
			item.SizeDisplay = fields[3]
		}
		if !addBackupArtifact(result, item) {
			return true, nil
		}
	}
	if errors.Is(scanner.Err(), bufio.ErrTooLong) {
		return true, nil
	}
	if scanner.Err() != nil {
		return false, errArtifactOutput
	}
	return false, nil
}
func (e backupExecutor) listGdriveArtifacts(ctx context.Context, id string, d backups.Destination, result *backups.ArtifactList) error {
	tool := e.executable("gdrive")
	if tool == "" {
		return errors.New("缺少可信的 gdrive，请先安装组件")
	}
	if err := checkBackupAccounts([]backups.Destination{d}, e.accountHome()); err != nil {
		return err
	}
	lockCtx, done := context.WithTimeout(ctx, 2*time.Second)
	unlock, err := e.lockGdriveAccount(lockCtx, d.Account)
	done()
	if err != nil {
		return errors.New("gdrive 账户正忙或账户锁不可用，请稍后刷新")
	}
	defer unlock()
	parent := filepath.Join(e.store.state, "queries")
	if err := privateBackupDirectory(parent); err != nil {
		return err
	}
	work, err := os.MkdirTemp(parent, "artifacts-")
	if err != nil {
		return errors.New("无法准备备份查询")
	}
	defer os.RemoveAll(work)
	env, cleanup, err := e.gdriveEnvironmentAt(work, d.Account)
	if err != nil {
		return err
	}
	defer cleanup()
	// Verify the parent first: a query for a missing/inaccessible Drive folder
	// can otherwise succeed with zero rows, hiding an authorization problem.
	parentTruncated, err := runBackupArtifactCommand(ctx, tool, env, []string{"files", "info", d.FolderID}, func(reader io.Reader) (bool, error) {
		scanner := bufio.NewScanner(reader)
		folder := false
		for scanner.Scan() {
			if strings.TrimSpace(scanner.Text()) == "Mime: application/vnd.google-apps.folder" {
				folder = true
			}
		}
		if scanner.Err() != nil || !folder {
			return false, errors.New("invalid folder")
		}
		return false, nil
	})
	if err != nil || parentTruncated {
		return errors.New("gdrive 文件夹不可读取，请检查文件夹 ID、账户授权及网络")
	}
	query := "'" + d.FolderID + "' in parents and trashed = false and name contains '" + id + "-' and mimeType != 'application/vnd.google-apps.folder' and mimeType != 'application/vnd.google-apps.shortcut'"
	args := []string{"files", "list", "--query", query, "--order-by", "createdTime desc,name", "--max", "1001", "--skip-header", "--full-name", "--field-separator", backupGdriveFieldSeparator}
	truncated, err := runBackupArtifactCommand(ctx, tool, env, args, func(reader io.Reader) (bool, error) { return parseGdriveArtifacts(reader, id, result) })
	result.Truncated = result.Truncated || truncated
	return err
}
