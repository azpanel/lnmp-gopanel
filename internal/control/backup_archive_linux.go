//go:build linux

package control

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"syscall"
	"time"
	"unicode/utf8"

	"github.com/azpanel/lnmp-gopanel/internal/backups"
	"github.com/go-sql-driver/mysql"
	"golang.org/x/sys/unix"
)

type backupManifest struct {
	Version   int                    `json:"version"`
	Metadata  string                 `json:"metadata,omitempty"`
	PolicyID  string                 `json:"policy_id"`
	RunID     string                 `json:"run_id"`
	Revision  uint64                 `json:"revision"`
	Category  string                 `json:"category"`
	CreatedAt time.Time              `json:"created_at"`
	Sources   []backupManifestSource `json:"sources"`
}
type backupManifestSource struct {
	Original    string   `json:"original"`
	ArchivePath string   `json:"archive_path"`
	Tables      []string `json:"tables,omitempty"`
}

// Metadata is authoritative for ownership and special permission bits: ZIP/7z
// extraction alone cannot reliably restore them. Never chown staged copies.
type backupFileMetadata struct {
	ArchivePath string    `json:"archive_path"`
	Type        string    `json:"type"`
	UID         uint32    `json:"uid"`
	GID         uint32    `json:"gid"`
	Mode        uint32    `json:"mode"`
	ModifiedAt  time.Time `json:"modified_at"`
}

func writeBackupFileMetadata(encoder *json.Encoder, name string, info os.FileInfo) error {
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok || !utf8.ValidString(name) || (!info.IsDir() && !info.Mode().IsRegular()) {
		return errors.New("无法完整记录文件元数据")
	}
	kind := "file"
	if info.IsDir() {
		kind = "directory"
	}
	return encoder.Encode(backupFileMetadata{ArchivePath: filepath.ToSlash(name), Type: kind, UID: stat.Uid, GID: stat.Gid, Mode: stat.Mode & 07777, ModifiedAt: info.ModTime().UTC()})
}

func sameBackupFileMetadata(before, after os.FileInfo) bool {
	a, okA := before.Sys().(*syscall.Stat_t)
	b, okB := after.Sys().(*syscall.Stat_t)
	return okA && okB && os.SameFile(before, after) && a.Uid == b.Uid && a.Gid == b.Gid && a.Mode == b.Mode && a.Ctim == b.Ctim && before.Size() == after.Size() && before.ModTime().Equal(after.ModTime())
}

// Check the inode observed before opening as well as its type. A source replaced
// during traversal is an error, even when the replacement is a special file.
func openBackupEntry(base *os.File, relative string, expected os.FileInfo) (*os.File, os.FileInfo, error) {
	file, err := openBackupRelative(base, relative)
	if err != nil {
		return nil, nil, errors.New("文件路径在备份期间变化或不可读取")
	}
	current, err := file.Stat()
	if err != nil || !os.SameFile(expected, current) || expected.Mode().Type() != current.Mode().Type() {
		file.Close()
		return nil, nil, errors.New("文件路径在备份期间变化或不可读取")
	}
	return file, current, nil
}

type backupContextReader struct {
	ctx context.Context
	r   io.Reader
}

func (r backupContextReader) Read(p []byte) (int, error) {
	if err := r.ctx.Err(); err != nil {
		return 0, err
	}
	return r.r.Read(p)
}

func (s backupPolicyStore) decryptBackupPassword(p storedBackupPolicy) ([]byte, error) {
	if p.Ciphertext == "" {
		return nil, nil
	}
	aead, err := s.policyCipher(false)
	if err != nil {
		return nil, err
	}
	data, err := base64.StdEncoding.DecodeString(p.Ciphertext)
	if err != nil || len(data) < aead.NonceSize()+aead.Overhead() {
		return nil, errors.New("压缩密码无法解密，请恢复政策密钥")
	}
	password, err := aead.Open(nil, data[:aead.NonceSize()], data[aead.NonceSize():], []byte(p.ID))
	if err != nil {
		return nil, errors.New("压缩密码无法解密，请恢复政策密钥")
	}
	if strings.ContainsAny(string(password), "\x00\r\n") {
		clear(password)
		return nil, errors.New("压缩密码含不支持的控制字符，请更换密码")
	}
	return password, nil
}

func (e backupExecutor) executeRun(ctx context.Context, j *storedBackupRun) error {
	p := j.Snapshot
	if p == nil || p.ID != j.PolicyID {
		return errors.New("备份政策快照无效")
	}
	if err := backups.ValidatePolicy(backups.SavePolicyRequest{Policy: p.Policy, PasswordAction: "keep"}); err != nil {
		return err
	}
	if err := backups.ValidateDestinations(p.Destinations, true); err != nil {
		return err
	}
	checks, err := inspectBackupPaths(backups.PathsRequest{Files: p.Files, Destinations: p.Destinations})
	if err != nil {
		return err
	}
	filePathError := ""
	for _, issue := range checks.Errors {
		if issue.Field == "destinations" {
			return errors.New(issue.Message)
		}
		if filePathError == "" {
			filePathError = issue.Message
		}
	}
	tool := e.executable("7zip")
	if tool == "" {
		return errors.New("缺少可信的 7-Zip，请由管理员安装 7zz 或 7z")
	}
	password, err := e.store.decryptBackupPassword(*p)
	if err != nil {
		return err
	}
	defer clear(password)
	if err = backups.ValidateCompressionPassword(p.Compression.Format, string(password)); err != nil {
		return err
	}
	if err = privateBackupDirectory(filepath.Join(e.store.state, "work")); err != nil {
		return err
	}
	work := e.workPath(j.ID)
	if err = os.Mkdir(work, 0700); err != nil {
		return errors.New("备份暂存目录已存在或不可创建")
	}
	successes, failures := 0, 0
	for _, category := range []string{"databases", "files"} {
		if category == "databases" && !p.Databases.Enabled || category == "files" && len(p.Files) == 0 {
			continue
		}
		j.Phase = category
		result := backups.CategoryResult{Category: category, Status: "running", Deliveries: []backups.Delivery{}}
		j.Categories = append(j.Categories, result)
		n := len(j.Categories) - 1
		if err = e.update(j); err != nil {
			return err
		}
		stage := filepath.Join(work, category)
		if err = os.Mkdir(stage, 0700); err != nil {
			return err
		}
		manifest := backupManifest{Version: 1, PolicyID: p.ID, RunID: j.ID, Revision: p.Revision, Category: category, CreatedAt: j.CreatedAt, Sources: []backupManifestSource{}}
		if category == "databases" {
			err = e.stageBackupDatabases(ctx, p.Databases, stage, &manifest)
		} else {
			if filePathError != "" {
				err = errors.New(filePathError)
			} else {
				err = e.stageBackupFiles(ctx, p.Files, stage, &manifest)
			}
		}
		if err == nil {
			err = writeBackupJSON(filepath.Join(stage, "manifest.json"), manifest)
		}
		name := backupArchiveName(*j, category)
		archive := filepath.Join(work, name)
		if err == nil {
			err = compressBackupArchive(ctx, tool, stage, archive, p.Compression, password)
		}
		if err == nil {
			result.Bytes, result.SHA256, err = backupFileDigest(ctx, archive)
		}
		if err != nil {
			result.Status = "failed"
			result.Error = backupDiagnostic(err.Error())
			failures++
		} else {
			result.Name = name
			result.Status = "succeeded"
			j.Phase = "delivery"
			j.Categories[n] = result
			if err = e.update(j); err != nil {
				return err
			}
			for _, destination := range p.Destinations {
				delivery := backups.Delivery{Destination: destination, Status: "failed"}
				object, deliveryErr := e.deliver(ctx, j.ID, destination, archive, name)
				if deliveryErr == nil {
					delivery.Status = "succeeded"
					delivery.Object = object
					successes++
					receipt := backupArtifact{PolicyID: p.ID, RunID: j.ID, Category: category, Destination: destination, Object: object, Name: name, Bytes: result.Bytes, SHA256: result.SHA256, CreatedAt: e.now().UTC()}
					if receiptErr := e.recordArtifact(receipt); receiptErr != nil {
						delivery.CleanupError = "备份已交付，但无法保存产物索引，未清理旧备份"
						failures++
					} else {
						keep := p.Retention.Files
						if category == "databases" {
							keep = p.Retention.Databases
						}
						if cleanupErr := e.retainArtifacts(ctx, j.ID, receipt, keep); cleanupErr != nil {
							delivery.CleanupError = backupDiagnostic(cleanupErr.Error())
							failures++
						}
					}
				} else {
					delivery.Error = backupDiagnostic(deliveryErr.Error())
					failures++
				}
				result.Deliveries = append(result.Deliveries, delivery)
				j.Categories[n] = result
				if err = e.update(j); err != nil {
					return err
				}
			}
			ok := 0
			for _, d := range result.Deliveries {
				if d.Status == "succeeded" {
					ok++
				}
			}
			if ok == 0 {
				result.Status = "failed"
			} else if ok < len(result.Deliveries) {
				result.Status = "partial"
			}
		}
		j.Categories[n] = result
		if err = e.update(j); err != nil {
			return err
		}
		_ = os.RemoveAll(stage)
		_ = os.Remove(archive)
	}
	if failures == 0 && successes > 0 {
		j.Status = "succeeded"
	} else if successes > 0 {
		j.Status = "partial"
	} else {
		j.Status = "failed"
	}
	return nil
}

func backupArchiveName(j storedBackupRun, category string) string {
	return fmt.Sprintf("%s-%s-%s-%s.%s", j.PolicyID, category, j.CreatedAt.UTC().Format("20060102T150405Z"), j.ID, j.Snapshot.Compression.Format)
}

// Open each relative component without following links. The pinned directory
// descriptor prevents path replacements from escaping the selected source.
func openBackupRelative(base *os.File, name string) (*os.File, error) {
	parts := strings.Split(filepath.ToSlash(name), "/")
	fd, err := unix.Dup(int(base.Fd()))
	if err != nil {
		return nil, err
	}
	for n, part := range parts {
		if part == "" || part == ".." {
			unix.Close(fd)
			return nil, errors.New("备份来源路径无效")
		}
		flags := unix.O_RDONLY | unix.O_CLOEXEC | unix.O_NOFOLLOW | unix.O_NONBLOCK
		if n < len(parts)-1 {
			flags |= unix.O_DIRECTORY
		}
		next, openErr := unix.Openat(fd, part, flags, 0)
		unix.Close(fd)
		if openErr != nil {
			return nil, openErr
		}
		fd = next
	}
	return os.NewFile(uintptr(fd), name), nil
}
func copyBackupFile(ctx context.Context, source *os.File, destination string, record func(os.FileInfo) error) error {
	before, err := source.Stat()
	if err != nil || !before.Mode().IsRegular() {
		return errors.New("备份来源已变化或不是普通文件")
	}
	f, err := os.OpenFile(destination, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		return err
	}
	n, copyErr := io.Copy(f, backupContextReader{ctx: ctx, r: io.LimitReader(source, before.Size()+1)})
	if copyErr == nil {
		copyErr = f.Chmod(before.Mode().Perm())
	}
	closeErr := f.Close()
	if copyErr == nil {
		copyErr = closeErr
	}
	after, statErr := source.Stat()
	if copyErr != nil || statErr != nil || n != before.Size() || !sameBackupFileMetadata(before, after) {
		return errors.New("文件读取失败或在备份期间发生变化，请重试")
	}
	if err := os.Chtimes(destination, before.ModTime(), before.ModTime()); err != nil {
		return err
	}
	if record != nil {
		return record(before)
	}
	return nil
}
func (e backupExecutor) stageBackupFiles(ctx context.Context, sources []backups.FileSource, stage string, manifest *backupManifest) error {
	workRoot, err := filepath.EvalSymlinks(filepath.Join(e.store.state, "work"))
	if err != nil {
		return err
	}
	metadata, err := os.OpenFile(filepath.Join(stage, "metadata.jsonl"), os.O_WRONLY|os.O_CREATE|os.O_EXCL|unix.O_NOFOLLOW, 0600)
	if err != nil {
		return err
	}
	defer metadata.Close()
	encoder := json.NewEncoder(metadata)
	record := func(name string, info os.FileInfo) error { return writeBackupFileMetadata(encoder, name, info) }
	for n, source := range sources {
		if _, err = backupSourceInfo(source); err != nil {
			return err
		}
		resolved, err := filepath.EvalSymlinks(source.Path)
		if err != nil {
			return errors.New("文件来源无法解析")
		}
		base, err := os.OpenFile(resolved, os.O_RDONLY|unix.O_NOFOLLOW|unix.O_NONBLOCK, 0)
		if err != nil {
			return errors.New("文件来源不可读取")
		}
		info, err := base.Stat()
		if err != nil {
			base.Close()
			return err
		}
		prefix := fmt.Sprintf("source-%03d", n+1)
		target := filepath.Join(stage, prefix)
		if err = os.Mkdir(target, 0700); err != nil {
			base.Close()
			return err
		}
		manifest.Sources = append(manifest.Sources, backupManifestSource{Original: source.Path, ArchivePath: prefix})
		if info.Mode().IsRegular() {
			err = copyBackupFile(ctx, base, filepath.Join(target, filepath.Base(source.Path)), func(info os.FileInfo) error {
				return record(filepath.Join(prefix, filepath.Base(source.Path)), info)
			})
			base.Close()
			if err != nil {
				return err
			}
			continue
		}
		if !info.IsDir() {
			base.Close()
			return errors.New("文件来源已变化")
		}
		root, err := os.OpenRoot("/proc/self/fd/" + fmt.Sprint(base.Fd()))
		if err != nil {
			base.Close()
			return err
		}
		type directory struct {
			path     string
			mode     os.FileMode
			modified time.Time
		}
		dirs := []directory{}
		err = fs.WalkDir(root.FS(), ".", func(relative string, entry fs.DirEntry, walkErr error) error {
			if ctx.Err() != nil {
				return ctx.Err()
			}
			if walkErr != nil {
				return errors.New("部分文件不可访问，未发布文件备份")
			}
			absolute := filepath.Join(resolved, relative)
			if virtualBackupPath(absolute) || backupPathContains(workRoot, absolute) || relative != "." && excludedBackupPath(relative, source.Excludes) {
				if entry.IsDir() {
					return fs.SkipDir
				}
				return nil
			}
			if entry.Type()&os.ModeSymlink != 0 {
				return nil
			}
			if relative == "." {
				dirs = append(dirs, directory{target, info.Mode().Perm(), info.ModTime()})
				return record(prefix, info)
			}
			expected, err := entry.Info()
			if err != nil {
				return errors.New("文件路径在备份期间变化或不可读取")
			}
			if entry.Type() != expected.Mode().Type() {
				return errors.New("文件类型在备份期间变化")
			}
			// Root-backed DirEntry.Info uses a no-follow stat and resolves unknown
			// directory entry types before we open sockets or devices.
			if !expected.IsDir() && !expected.Mode().IsRegular() {
				return nil
			}
			file, current, err := openBackupEntry(base, relative, expected)
			if err != nil {
				return err
			}
			defer file.Close()
			destination := filepath.Join(target, relative)
			if current.IsDir() {
				if err = os.Mkdir(destination, 0700); err != nil {
					return err
				}
				dirs = append(dirs, directory{destination, current.Mode().Perm(), current.ModTime()})
				return record(filepath.Join(prefix, relative), current)
			}
			return copyBackupFile(ctx, file, destination, func(info os.FileInfo) error {
				return record(filepath.Join(prefix, relative), info)
			})
		})
		root.Close()
		base.Close()
		if err != nil {
			return err
		}
		for n := len(dirs) - 1; n >= 0; n-- {
			d := dirs[n]
			if err = os.Chmod(d.path, d.mode); err != nil {
				return err
			}
			if err = os.Chtimes(d.path, d.modified, d.modified); err != nil {
				return err
			}
		}
	}
	if err := metadata.Sync(); err != nil {
		return err
	}
	if err := metadata.Close(); err != nil {
		return err
	}
	manifest.Version = 2
	manifest.Metadata = "metadata.jsonl"
	return nil
}

func mysqlBackupOption(value string) string {
	return "\"" + strings.NewReplacer("\\", "\\\\", "\"", "\\\"", "\n", "\\n", "\r", "\\r", "\t", "\\t").Replace(value) + "\""
}
func backupSQLIdentifier(value string) string {
	return "`" + strings.ReplaceAll(value, "`", "``") + "`"
}
func (e backupExecutor) stageBackupDatabases(ctx context.Context, selection backups.DatabaseSelection, stage string, manifest *backupManifest) error {
	tool := e.executable("dump")
	if tool == "" {
		return errors.New("缺少可信的 mysqldump/mariadb-dump，请由管理员安装")
	}
	db, err := openBackupDatabase(ctx, "")
	if err != nil {
		return errors.New("自动备份无法认证本机数据库，请配置 root 客户端凭据或 Socket 认证")
	}
	defer db.Close()
	return dumpBackupDatabases(ctx, db, localDatabaseConfig(""), tool, selection, stage, manifest)
}

func dumpBackupDatabases(ctx context.Context, db *sql.DB, config *mysql.Config, tool string, selection backups.DatabaseSelection, stage string, manifest *backupManifest) error {
	rows, err := db.QueryContext(ctx, "SELECT SCHEMA_NAME FROM information_schema.SCHEMATA ORDER BY SCHEMA_NAME")
	if err != nil {
		return errors.New("数据库清单读取失败")
	}
	available := []string{}
	for rows.Next() {
		var name string
		if err = rows.Scan(&name); err != nil {
			break
		}
		available = append(available, name)
	}
	if err == nil {
		err = rows.Err()
	}
	rows.Close()
	if err != nil {
		return errors.New("数据库清单读取失败")
	}
	names := []string{}
	if selection.Mode == "selected" {
		names = append(names, selection.Selected...)
		for _, name := range names {
			if !slices.Contains(available, name) {
				return errors.New("所选数据库已不存在，请更新政策")
			}
		}
	} else {
		for _, name := range available {
			if !slices.Contains(selection.Excluded, name) {
				names = append(names, name)
			}
		}
	}
	if len(names) == 0 {
		return errors.New("没有符合政策的数据库，未生成空数据库备份")
	}
	if strings.ContainsRune(config.Passwd, 0) {
		return errors.New("数据库客户端凭据格式不受支持")
	}
	// Never let the external client load arbitrary [mysqldump] options or plugins.
	defaults := filepath.Join(filepath.Dir(stage), "mysql-client.cnf")
	contents := "[client]\nuser=root\nprotocol=socket\nsocket=" + mysqlBackupOption(config.Addr) + "\npassword=" + mysqlBackupOption(config.Passwd) + "\n"
	if err = os.WriteFile(defaults, []byte(contents), 0600); err != nil {
		return err
	}
	defer os.Remove(defaults)
	version, _ := RunCommand(ctx, tool, "--version")
	for n, name := range names {
		if err = backups.ValidateNames([]string{name}); err != nil {
			return err
		}
		var charset, collation string
		if err = db.QueryRowContext(ctx, "SELECT DEFAULT_CHARACTER_SET_NAME, DEFAULT_COLLATION_NAME FROM information_schema.SCHEMATA WHERE SCHEMA_NAME=?", name).Scan(&charset, &collation); err != nil {
			return errors.New("数据库字符集读取失败")
		}
		allTables := []string{}
		rows, err = db.QueryContext(ctx, "SELECT TABLE_NAME FROM information_schema.TABLES WHERE TABLE_SCHEMA=? ORDER BY TABLE_NAME", name)
		if err != nil {
			return errors.New("表清单读取失败")
		}
		for rows.Next() {
			var table string
			if err = rows.Scan(&table); err != nil {
				break
			}
			allTables = append(allTables, table)
		}
		if err == nil {
			err = rows.Err()
		}
		rows.Close()
		if err != nil {
			return errors.New("表清单读取失败")
		}
		tables := append([]string{}, allTables...)
		fullDatabase := true
		for _, rule := range selection.Rules {
			if rule.Database != name || rule.Mode == "all" {
				continue
			}
			fullDatabase = false
			if rule.Mode == "include" {
				for _, table := range rule.Tables {
					if !slices.Contains(allTables, table) {
						return errors.New("所选数据表已不存在，请更新政策")
					}
				}
				tables = append([]string{}, rule.Tables...)
			} else {
				tables = slices.DeleteFunc(tables, func(table string) bool { return slices.Contains(rule.Tables, table) })
			}
			break
		}
		filename := fmt.Sprintf("database-%03d.sql", n+1)
		out, err := os.OpenFile(filepath.Join(stage, filename), os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
		if err != nil {
			return err
		}
		_, err = fmt.Fprintf(out, "CREATE DATABASE IF NOT EXISTS %s CHARACTER SET %s COLLATE %s;\nUSE %s;\n", backupSQLIdentifier(name), backupSQLIdentifier(charset), backupSQLIdentifier(collation), backupSQLIdentifier(name))
		if err != nil {
			out.Close()
			return err
		}
		args := []string{"--defaults-file=" + defaults, "--single-transaction", "--skip-lock-tables", "--quick", "--hex-blob", "--no-tablespaces", "--triggers", "--default-character-set=utf8mb4"}
		if fullDatabase {
			args = append(args, "--routines", "--events")
		}
		if !strings.Contains(strings.ToLower(version), "mariadb") {
			args = append(args, "--set-gtid-purged=OFF")
		}
		if len(tables) == 0 {
			args = append(args, "--no-data", "--no-create-info", "--skip-triggers")
		}
		args = append(args, "--", name)
		args = append(args, tables...)
		cmd := exec.CommandContext(ctx, tool, args...)
		configureProcess(cmd)
		cmd.Env = backupCommandEnvironment()
		cmd.Stdout = out
		cmd.Stderr = &limitedBuffer{}
		runErr := cmd.Run()
		syncErr := out.Sync()
		closeErr := out.Close()
		if runErr != nil || syncErr != nil || closeErr != nil {
			return errors.New("数据库导出失败，请检查客户端兼容性、权限及可用磁盘空间")
		}
		manifest.Sources = append(manifest.Sources, backupManifestSource{Original: name, ArchivePath: filename, Tables: tables})
	}
	return nil
}
func backupCommandEnvironment() []string {
	return []string{"PATH=/usr/sbin:/usr/bin:/sbin:/bin:/usr/local/bin", "LANG=C.UTF-8", "LC_ALL=C.UTF-8"}
}
func compressBackupArchive(ctx context.Context, tool, stage, archive string, c backups.Compression, password []byte) error {
	if err := backups.ValidateCompressionPassword(c.Format, string(password)); err != nil {
		return err
	}
	args := []string{"a", "-t" + c.Format, fmt.Sprintf("-mx=%d", c.Level), "-y", "-bd", "-bb0"}
	if len(password) > 0 {
		args = append(args, "-p")
		if c.Format == "zip" {
			args = append(args, "-mem=AES256")
		} else {
			args = append(args, "-mhe=on")
		}
	}
	args = append(args, "--", archive, ".")
	if err := runBackupArchiveCommand(ctx, tool, stage, args, password); err != nil {
		return errors.New("压缩失败，请检查 7-Zip 兼容性及可用磁盘空间")
	}
	check := []string{"t", "-bd", "-bb0"}
	// On read, a bare -p defines an empty password and suppresses the prompt.
	// Omit it so 7-Zip requests the real password through standard input.
	check = append(check, "--", archive)
	if err := runBackupArchiveCommand(ctx, tool, stage, check, password); err != nil {
		return errors.New("压缩包完整性校验失败，未发布备份")
	}
	return nil
}
func runBackupArchiveCommand(ctx context.Context, tool, dir string, args []string, password []byte) error {
	cmd := exec.CommandContext(ctx, tool, args...)
	configureProcess(cmd)
	cmd.Dir = dir
	cmd.Env = backupCommandEnvironment()
	cmd.Stdout = &limitedBuffer{}
	cmd.Stderr = &limitedBuffer{}
	if len(password) > 0 {
		cmd.Stdin = strings.NewReader(string(password) + "\n" + string(password) + "\n")
	}
	return cmd.Run()
}
func backupFileDigest(ctx context.Context, path string) (int64, string, error) {
	f, err := os.OpenFile(path, os.O_RDONLY|unix.O_NOFOLLOW|unix.O_NONBLOCK, 0)
	if err != nil {
		return 0, "", err
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil || !info.Mode().IsRegular() {
		return 0, "", errors.New("备份产物不是普通文件")
	}
	h := sha256.New()
	n, err := io.Copy(h, backupContextReader{ctx: ctx, r: f})
	return n, hex.EncodeToString(h.Sum(nil)), err
}
