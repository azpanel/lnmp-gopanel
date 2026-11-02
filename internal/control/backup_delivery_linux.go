//go:build linux

package control

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/azpanel/lnmp-gopanel/internal/backups"
	"golang.org/x/sys/unix"
)

type backupArtifact struct {
	PolicyID    string              `json:"policy_id"`
	RunID       string              `json:"run_id"`
	Category    string              `json:"category"`
	Destination backups.Destination `json:"destination"`
	Object      string              `json:"object"`
	Name        string              `json:"name"`
	Bytes       int64               `json:"bytes"`
	SHA256      string              `json:"sha256"`
	CreatedAt   time.Time           `json:"created_at"`
}

func (e backupExecutor) receiptDirectory(a backupArtifact) string {
	h := sha256.Sum256([]byte(backups.DestinationKey(a.Destination)))
	return filepath.Join(e.store.state, "artifacts", a.PolicyID, hex.EncodeToString(h[:]), a.Category)
}
func (e backupExecutor) recordArtifact(a backupArtifact) error {
	dir := e.receiptDirectory(a)
	if err := privateBackupDirectory(dir); err != nil {
		return err
	}
	return writeBackupJSON(filepath.Join(dir, a.RunID+".json"), a)
}
func (e backupExecutor) retainArtifacts(ctx context.Context, runID string, current backupArtifact, keep int) error {
	dir := e.receiptDirectory(current)
	entries, err := os.ReadDir(dir)
	if err != nil {
		return errors.New("无法读取产物索引，未清理旧备份")
	}
	items := []backupArtifact{}
	for _, entry := range entries {
		if !strings.HasSuffix(entry.Name(), ".json") || !backups.ValidPolicyID(strings.TrimSuffix(entry.Name(), ".json")) {
			return errors.New("产物索引异常，未清理旧备份")
		}
		var a backupArtifact
		if err = readBackupJSON(filepath.Join(dir, entry.Name()), 64*1024, &a); err != nil {
			return errors.New("产物索引损坏或权限不安全，未清理旧备份")
		}
		if a.PolicyID != current.PolicyID || a.Category != current.Category || a.RunID+".json" != entry.Name() || backups.DestinationKey(a.Destination) != backups.DestinationKey(current.Destination) || !safeArtifactName(a) {
			return errors.New("产物索引不匹配，未清理旧备份")
		}
		items = append(items, a)
	}
	sort.Slice(items, func(i, j int) bool {
		if items[i].CreatedAt.Equal(items[j].CreatedAt) {
			return items[i].RunID > items[j].RunID
		}
		return items[i].CreatedAt.After(items[j].CreatedAt)
	})
	for n := keep; n < len(items); n++ {
		a := items[n]
		if a.RunID == current.RunID {
			continue
		}
		if err = e.removeArtifact(ctx, runID, a); err != nil {
			return err
		}
		if err = os.Remove(filepath.Join(dir, a.RunID+".json")); err != nil {
			return errors.New("旧备份已清理，但产物索引更新失败")
		}
	}
	return nil
}
func safeArtifactName(a backupArtifact) bool {
	return backups.ValidPolicyID(a.PolicyID) && backups.ValidPolicyID(a.RunID) && (a.Category == "files" || a.Category == "databases") && filepath.Base(a.Name) == a.Name && strings.HasPrefix(a.Name, a.PolicyID+"-"+a.Category+"-") && (strings.HasSuffix(a.Name, "-"+a.RunID+".zip") || strings.HasSuffix(a.Name, "-"+a.RunID+".7z")) && len(a.SHA256) == 64
}
func (e backupExecutor) accountHome() string {
	if e.store.accountHome != "" {
		return e.store.accountHome
	}
	return "/root"
}

var backupDriveID = regexp.MustCompile(`^[a-zA-Z0-9_-]{1,256}$`)

// gdrive 3 selects accounts via account.json, not a command flag. Give each
// invocation its own config home; only its selected, trusted credential directory
// is shared so refreshed tokens are persisted without changing root's selection.
func (e backupExecutor) gdriveEnvironment(runID, account string) ([]string, func(), error) {
	if account == "." || account == ".." || filepath.Base(account) != account {
		return nil, nil, backups.ErrInvalid
	}
	if err := checkBackupAccounts([]backups.Destination{{Type: "gdrive", Account: account, FolderID: "root"}}, e.accountHome()); err != nil {
		return nil, nil, err
	}
	accountDir := filepath.Join(e.accountHome(), ".config/gdrive3", account)
	for _, filename := range []string{"secret.json", "tokens.json"} {
		if _, err := backupConfigInfo(filepath.Join(accountDir, filename)); err != nil {
			return nil, nil, errors.New("gdrive 账户凭据缺失或权限不安全")
		}
	}
	if err := privateBackupDirectory(e.workPath(runID)); err != nil {
		return nil, nil, err
	}
	home, err := os.MkdirTemp(e.workPath(runID), "gdrive-")
	if err != nil {
		return nil, nil, err
	}
	cleanup := func() { _ = os.RemoveAll(home) }
	config := filepath.Join(home, ".config/gdrive3")
	if err = os.MkdirAll(config, 0700); err == nil {
		err = writeBackupJSON(filepath.Join(config, "account.json"), struct {
			Current string `json:"current"`
		}{account})
	}
	if err == nil {
		err = os.Symlink(accountDir, filepath.Join(config, account))
	}
	if err != nil {
		cleanup()
		return nil, nil, errors.New("无法准备 gdrive 账户")
	}
	// HOME is scoped to this child process, never assigned in the agent shell or
	// inherited by the panel, other jobs, or administrator commands.
	env := append(backupCommandEnvironment(), "HOME="+home)
	return env, cleanup, nil
}
func (e backupExecutor) gdrive(ctx context.Context, runID, account string, args ...string) (string, error) {
	tool := e.executable("gdrive")
	if tool == "" {
		return "", errors.New("缺少可信的 gdrive，请先安装组件")
	}
	unlock, err := e.lockGdriveAccount(ctx, account)
	if err != nil {
		return "", err
	}
	defer unlock()
	env, cleanup, err := e.gdriveEnvironment(runID, account)
	if err != nil {
		return "", err
	}
	defer cleanup()
	cmd := exec.CommandContext(ctx, tool, args...)
	configureProcess(cmd)
	cmd.Env = env
	out := &limitedBuffer{}
	cmd.Stdout = out
	cmd.Stderr = &limitedBuffer{}
	if err = cmd.Run(); err != nil {
		return "", errors.New("gdrive 操作失败，请检查账户授权、文件夹权限及网络")
	}
	return strings.TrimSpace(string(out.data)), nil
}

func (e backupExecutor) lockGdriveAccount(ctx context.Context, account string) (func(), error) {
	dir := filepath.Join(e.store.state, "account-locks")
	if err := privateBackupDirectory(dir); err != nil {
		return nil, err
	}
	digest := sha256.Sum256([]byte(account))
	filename := filepath.Join(dir, hex.EncodeToString(digest[:])+".lock")
	f, err := os.OpenFile(filename, os.O_CREATE|os.O_RDWR|unix.O_NOFOLLOW, 0600)
	if err != nil {
		return nil, err
	}
	if TrustedPath(filename) != nil {
		f.Close()
		return nil, errors.New("gdrive 账户锁权限不安全")
	}
	for {
		err = unix.Flock(int(f.Fd()), unix.LOCK_EX|unix.LOCK_NB)
		if err == nil {
			break
		}
		if err != unix.EWOULDBLOCK && err != unix.EAGAIN {
			f.Close()
			return nil, err
		}
		select {
		case <-ctx.Done():
			f.Close()
			return nil, ctx.Err()
		case <-time.After(50 * time.Millisecond):
		}
	}
	return func() { _ = unix.Flock(int(f.Fd()), unix.LOCK_UN); _ = f.Close() }, nil
}
func (e backupExecutor) rclone(ctx context.Context, args ...string) error {
	tool := e.executable("rclone")
	if tool == "" {
		return errors.New("缺少可信的 rclone，请先安装组件")
	}
	config := filepath.Join(e.accountHome(), ".config/rclone/rclone.conf")
	if _, err := backupConfigInfo(config); err != nil {
		return errors.New("rclone 配置缺失或权限不安全")
	}
	// Explicit config and a clean environment prevent inherited RCLONE_* options
	// from redirecting credentials, commands, logs, or the selected destination.
	command := append([]string{args[0], "--config", config, "--retries", "1", "--low-level-retries", "2"}, args[1:]...)
	cmd := exec.CommandContext(ctx, tool, command...)
	configureProcess(cmd)
	cmd.Env = backupCommandEnvironment()
	cmd.Stdout = &limitedBuffer{}
	cmd.Stderr = &limitedBuffer{}
	if err := cmd.Run(); err != nil {
		return errors.New("rclone 操作失败，请检查远程配置、写入权限及网络")
	}
	return nil
}
func (e backupExecutor) deliver(ctx context.Context, runID string, d backups.Destination, archive, name string) (string, error) {
	switch d.Type {
	case "local":
		resolved, err := resolveBackupDestination(d.Path)
		if err != nil {
			return "", err
		}
		if err = os.MkdirAll(resolved, 0700); err != nil {
			return "", errors.New("无法创建本地备份目录")
		}
		root, err := os.OpenRoot(resolved)
		if err != nil {
			return "", errors.New("无法打开本地备份目录")
		}
		defer root.Close()
		temp := "." + name + ".partial"
		out, err := root.OpenFile(temp, os.O_WRONLY|os.O_CREATE|os.O_EXCL|unix.O_NOFOLLOW, 0600)
		if err != nil {
			return "", errors.New("无法写入本地备份临时文件")
		}
		defer root.Remove(temp)
		in, err := os.Open(archive)
		if err != nil {
			out.Close()
			return "", err
		}
		_, err = io.Copy(out, backupContextReader{ctx: ctx, r: in})
		in.Close()
		if err == nil {
			err = out.Sync()
		}
		closeErr := out.Close()
		if err == nil {
			err = closeErr
		}
		if err != nil {
			return "", errors.New("本地备份写入失败，请检查可用空间及权限")
		}
		// Link publishes without replacing an existing file; unlink the staging
		// name only after the complete archive is durably written.
		if err = root.Link(temp, name); err != nil {
			return "", errors.New("本地备份发布失败，同名文件可能已存在")
		}
		if err = root.Remove(temp); err != nil {
			return "", errors.New("本地备份发布后临时文件清理失败")
		}
		dir, err := root.Open(".")
		if err != nil {
			return "", errors.New("本地备份目录不可同步")
		}
		err = dir.Sync()
		dir.Close()
		if err != nil {
			return "", errors.New("本地备份目录同步失败")
		}
		return filepath.Join(resolved, name), nil
	case "gdrive":
		id, err := e.gdrive(ctx, runID, d.Account, "files", "upload", "--parent", d.FolderID, "--print-only-id", archive)
		if err == nil && !backupDriveID.MatchString(id) {
			err = errors.New("gdrive 未返回有效文件 ID，未清理旧备份")
		}
		return id, err
	case "rclone":
		if err := checkBackupAccounts([]backups.Destination{d}, e.accountHome()); err != nil {
			return "", err
		}
		object := d.Remote + ":" + path.Join(d.Path, name)
		return object, e.rclone(ctx, "copyto", "--", archive, object)
	}
	return "", backups.ErrInvalid
}
func (e backupExecutor) removeArtifact(ctx context.Context, runID string, a backupArtifact) error {
	if !safeArtifactName(a) {
		return errors.New("产物名称异常，未清理旧备份")
	}
	switch a.Destination.Type {
	case "local":
		// The original resolved location is authoritative. A changed destination
		// symlink must not redirect removal into an unrelated directory.
		if filepath.Base(a.Object) != a.Name || !filepath.IsAbs(a.Object) {
			return errors.New("本地产物索引无效")
		}
		root, err := os.OpenRoot(filepath.Dir(a.Object))
		if os.IsNotExist(err) {
			return nil
		}
		if err != nil {
			return errors.New("旧备份目录不可读取")
		}
		defer root.Close()
		f, err := root.OpenFile(a.Name, os.O_RDONLY|unix.O_NOFOLLOW|unix.O_NONBLOCK, 0)
		if os.IsNotExist(err) {
			return nil
		}
		if err != nil {
			return errors.New("旧备份路径已变化，未删除")
		}
		defer f.Close()
		info, err := f.Stat()
		if err != nil || !info.Mode().IsRegular() || info.Size() != a.Bytes {
			return errors.New("旧备份已被修改，未删除")
		}
		h := sha256.New()
		if _, err = io.Copy(h, backupContextReader{ctx: ctx, r: f}); err != nil || hex.EncodeToString(h.Sum(nil)) != a.SHA256 {
			return errors.New("旧备份校验不匹配，未删除")
		}
		return removeVerifiedLocalBackup(root, a.Name, info)
	case "gdrive":
		if !backupDriveID.MatchString(a.Object) {
			return errors.New("gdrive 产物 ID 无效")
		}
		_, err := e.gdrive(ctx, runID, a.Destination.Account, "files", "delete", a.Object)
		return err
	case "rclone":
		if a.Object != a.Destination.Remote+":"+path.Join(a.Destination.Path, a.Name) {
			return errors.New("rclone 产物索引无效")
		}
		return e.rclone(ctx, "deletefile", "--", a.Object)
	}
	return backups.ErrInvalid
}

// Unlink a verified inode, never whichever file happens to occupy its pathname
// after hashing. A private directory on the same filesystem lets us atomically
// capture a raced replacement, verify its identity, and restore it without
// overwriting a newer administrator file.
func removeVerifiedLocalBackup(root *os.Root, name string, expected os.FileInfo) error {
	base, err := root.Open(".")
	if err != nil {
		return errors.New("旧备份目录不可读取")
	}
	defer base.Close()
	id, err := newBackupRunID()
	if err != nil {
		return err
	}
	temporary := ".gopanel-prune-" + id
	if err = unix.Mkdirat(int(base.Fd()), temporary, 0700); err != nil {
		return errors.New("无法创建旧备份清理暂存目录")
	}
	defer root.Remove(temporary)
	fd, err := unix.Openat(int(base.Fd()), temporary, unix.O_RDONLY|unix.O_DIRECTORY|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0)
	if err != nil {
		return errors.New("清理暂存目录已变化，未删除旧备份")
	}
	private := os.NewFile(uintptr(fd), temporary)
	defer private.Close()
	var stat unix.Stat_t
	if err = unix.Fstat(fd, &stat); err != nil || stat.Uid != uint32(os.Geteuid()) || stat.Mode&0777 != 0700 {
		return errors.New("清理暂存目录权限不安全")
	}
	if err = unix.Renameat(int(base.Fd()), name, fd, "archive"); err == unix.ENOENT {
		return nil
	} else if err != nil {
		return errors.New("旧备份暂存失败，未删除")
	}
	restore := func() error {
		if err := unix.Renameat2(fd, "archive", int(base.Fd()), name, unix.RENAME_NOREPLACE); err != nil {
			return errors.New("旧备份路径被并发修改，未删除；文件保存在存储目录的 " + temporary + "/archive")
		}
		return errors.New("旧备份被并发修改，已恢复，未删除")
	}
	fileFD, err := unix.Openat(fd, "archive", unix.O_RDONLY|unix.O_NOFOLLOW|unix.O_NONBLOCK|unix.O_CLOEXEC, 0)
	if err != nil {
		return restore()
	}
	file := os.NewFile(uintptr(fileFD), "archive")
	actual, statErr := file.Stat()
	file.Close()
	if statErr != nil || !os.SameFile(expected, actual) || !actual.Mode().IsRegular() || actual.Size() != expected.Size() || !actual.ModTime().Equal(expected.ModTime()) {
		return restore()
	}
	if err = unix.Unlinkat(fd, "archive", 0); err != nil {
		return restore()
	}
	return nil
}
