//go:build linux

package control

import (
	"bytes"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"os/user"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/azpanel/lnmp-gopanel/internal/vhosts"
	"golang.org/x/sys/unix"
)

type createdSiteDirectory struct {
	parent, file *os.File
	name, path   string
	identity     unix.Stat_t
}

type linuxSiteRootPreparation struct {
	files      []*os.File
	dirs       []createdSiteDirectory
	ini        *os.File
	iniParent  *os.File
	iniContent []byte
}

func prepareSiteRoot(path string) (siteRootPreparation, error) {
	if !vhosts.ValidSiteRoot(path) {
		return nil, errors.New("站点目录必须是 /home/wwwroot/ 下的规范绝对路径")
	}
	if err := TrustedPath(vhosts.SiteRootDirectory); err != nil {
		return nil, errors.New("站点根目录不存在或权限不安全")
	}
	account, err := user.Lookup("www")
	if err != nil {
		return nil, errors.New("未找到 LNMP www 用户")
	}
	uid, err := strconv.Atoi(account.Uid)
	if err != nil {
		return nil, err
	}
	gid, err := strconv.Atoi(account.Gid)
	if err != nil {
		return nil, err
	}
	return prepareSiteRootAt(vhosts.SiteRootDirectory, strings.TrimPrefix(path, vhosts.SiteRootDirectory+"/"), uid, gid)
}

// Open every base component without following symlinks, including wwwroot itself.
func openSiteRootBase(path string) (*os.File, error) {
	fd, err := unix.Open("/", unix.O_RDONLY|unix.O_DIRECTORY|unix.O_CLOEXEC, 0)
	if err != nil {
		return nil, err
	}
	for _, part := range strings.Split(strings.TrimPrefix(path, "/"), "/") {
		next, e := unix.Openat(fd, part, unix.O_RDONLY|unix.O_DIRECTORY|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0)
		_ = unix.Close(fd)
		if e != nil {
			return nil, errors.New("站点路径包含符号链接、非目录组件或不可访问目录")
		}
		fd = next
	}
	return os.NewFile(uintptr(fd), path), nil
}

func safeSiteRootParent(file *os.File) bool {
	var stat unix.Stat_t
	return unix.Fstat(int(file.Fd()), &stat) == nil && stat.Mode&unix.S_IFMT == unix.S_IFDIR && stat.Uid == uint32(os.Geteuid()) && stat.Mode&0022 == 0
}

// The physical base is injectable for tests; production always uses the fixed
// wwwroot base. Existing directories are opened but never chowned or chmodded.
func prepareSiteRootAt(base, relative string, uid, gid int) (prepared *linuxSiteRootPreparation, err error) {
	if !vhosts.SafeAbsolutePath(base) || relative == "." || filepath.IsAbs(relative) || filepath.Clean(relative) != relative || relative == ".." || strings.HasPrefix(relative, "../") {
		return nil, errors.New("站点子目录无效")
	}
	parent, err := openSiteRootBase(base)
	if err != nil {
		return nil, err
	}
	p := &linuxSiteRootPreparation{files: []*os.File{parent}}
	defer func() {
		if err != nil {
			err = errors.Join(err, p.Rollback())
			p.Close()
		}
	}()
	if !safeSiteRootParent(parent) {
		return nil, errors.New("站点根目录权限不安全")
	}
	parts := strings.Split(relative, "/")
	fresh := false
	for i, part := range parts {
		fd, e := unix.Openat(int(parent.Fd()), part, unix.O_RDONLY|unix.O_DIRECTORY|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0)
		var next *os.File
		if errors.Is(e, unix.ENOENT) {
			next, e = p.createDirectory(parent, part, filepath.Join(base, filepath.Join(parts[:i+1]...)))
			fresh = i == len(parts)-1
		} else if e == nil {
			next = os.NewFile(uintptr(fd), part)
			p.files = append(p.files, next)
		}
		if e != nil {
			return nil, fmt.Errorf("站点子目录无法创建或包含符号链接：%w", e)
		}
		parent = next
	}
	if fresh {
		fd, e := unix.Openat(int(parent.Fd()), ".user.ini", unix.O_CREAT|unix.O_EXCL|unix.O_RDWR|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0644)
		if e != nil {
			return nil, fmt.Errorf("无法创建 PHP 隔离配置：%w", e)
		}
		p.ini = os.NewFile(uintptr(fd), ".user.ini")
		p.iniParent = parent
		content := []byte("open_basedir=\"" + strings.ReplaceAll(filepath.Join(base, relative), `"`, `\"`) + ":/tmp/:/proc/\"\n")
		n, e := p.ini.Write(content)
		p.iniContent = content[:n]
		if e != nil {
			return nil, e
		}
		if e = p.ini.Chown(uid, gid); e != nil {
			return nil, e
		}
		if e = p.ini.Chmod(0644); e != nil {
			return nil, e
		}
	}
	for _, dir := range p.dirs {
		if e := dir.file.Chmod(0755); e != nil {
			return nil, e
		}
	}
	if fresh {
		if e := parent.Chown(uid, gid); e != nil {
			return nil, e
		}
	}
	return p, nil
}

// Stage in the trusted base and retain the new inode before publishing it.
// mkdir/open by name in a www-writable parent could otherwise open a substituted
// existing directory and accidentally give it www ownership.
func (p *linuxSiteRootPreparation) createDirectory(parent *os.File, name, path string) (*os.File, error) {
	var random [16]byte
	if _, err := rand.Read(random[:]); err != nil {
		return nil, err
	}
	stage := ".gopanel-root-" + hex.EncodeToString(random[:])
	base := p.files[0]
	stagePath := filepath.Join(base.Name(), stage)
	// A trusted immediate parent also supports roots below a separate mount.
	if safeSiteRootParent(parent) {
		base = parent
		stagePath = filepath.Join(filepath.Dir(path), stage)
	}
	if err := unix.Mkdirat(int(base.Fd()), stage, 0700); err != nil {
		return nil, err
	}
	fd, err := unix.Openat(int(base.Fd()), stage, unix.O_RDONLY|unix.O_DIRECTORY|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0)
	if err != nil {
		return nil, errors.Join(err, unix.Unlinkat(int(base.Fd()), stage, unix.AT_REMOVEDIR))
	}
	file := os.NewFile(uintptr(fd), path)
	p.files = append(p.files, file)
	dir := createdSiteDirectory{parent: base, file: file, name: stage, path: stagePath}
	if err = unix.Fstat(fd, &dir.identity); err != nil {
		return nil, errors.Join(err, unix.Unlinkat(int(base.Fd()), stage, unix.AT_REMOVEDIR))
	}
	p.dirs = append(p.dirs, dir)
	if err = unix.Renameat2(int(base.Fd()), stage, int(parent.Fd()), name, unix.RENAME_NOREPLACE); err != nil {
		return nil, err
	}
	p.dirs[len(p.dirs)-1].parent = parent
	p.dirs[len(p.dirs)-1].name = name
	p.dirs[len(p.dirs)-1].path = path
	return file, nil
}

func sameSiteRootEntry(parent *os.File, name string, expected unix.Stat_t) error {
	if !safeSiteRootParent(parent) {
		return errors.New("父目录可被普通用户修改，保留资源")
	}
	var current unix.Stat_t
	if err := unix.Fstatat(int(parent.Fd()), name, &current, unix.AT_SYMLINK_NOFOLLOW); err != nil {
		return err
	}
	if current.Dev != expected.Dev || current.Ino != expected.Ino || current.Mode&unix.S_IFMT != expected.Mode&unix.S_IFMT {
		return errors.New("路径已被替换，保留当前对象")
	}
	return nil
}

func (p *linuxSiteRootPreparation) removeINI() error {
	if p.ini == nil {
		return nil
	}
	var stat unix.Stat_t
	if err := unix.Fstat(int(p.ini.Fd()), &stat); err != nil {
		return err
	}
	if stat.Nlink == 0 {
		return nil
	}
	if err := sameSiteRootEntry(p.iniParent, ".user.ini", stat); err != nil {
		return err
	}
	if stat.Nlink != 1 || stat.Size != int64(len(p.iniContent)) {
		return errors.New(".user.ini 已被修改或建立硬链接，保留文件")
	}
	data := make([]byte, len(p.iniContent)+1)
	n, err := p.ini.ReadAt(data, 0)
	if err != nil && !errors.Is(err, io.EOF) {
		return err
	}
	if !bytes.Equal(data[:n], p.iniContent) {
		return errors.New(".user.ini 内容已被修改，保留文件")
	}
	return unix.Unlinkat(int(p.iniParent.Fd()), ".user.ini", 0)
}

func (p *linuxSiteRootPreparation) Rollback() error {
	var failures []error
	// Revoke www's directory write access first, using pinned descriptors. This
	// also prevents replacement between the identity checks and unlinkat below.
	for _, dir := range p.dirs {
		if err := dir.file.Chown(int(dir.identity.Uid), int(dir.identity.Gid)); err != nil {
			failures = append(failures, fmt.Errorf("%s 收回所有权失败：%w", dir.path, err))
		}
		if err := dir.file.Chmod(0700); err != nil {
			failures = append(failures, fmt.Errorf("%s 收回写权限失败：%w", dir.path, err))
		}
	}
	if err := p.removeINI(); err != nil {
		failures = append(failures, fmt.Errorf("%s/.user.ini 清理失败：%w", p.iniParent.Name(), err))
	}
	for i := len(p.dirs) - 1; i >= 0; i-- {
		dir := p.dirs[i]
		var stat unix.Stat_t
		if err := unix.Fstat(int(dir.file.Fd()), &stat); err == nil && stat.Nlink == 0 {
			continue
		}
		err := sameSiteRootEntry(dir.parent, dir.name, dir.identity)
		if err == nil {
			err = unix.Unlinkat(int(dir.parent.Fd()), dir.name, unix.AT_REMOVEDIR)
		}
		if err != nil {
			failures = append(failures, fmt.Errorf("%s 清理失败，目录已保留：%w", dir.path, err))
		}
	}
	return errors.Join(failures...)
}

func (p *linuxSiteRootPreparation) Close() {
	if p.ini != nil {
		_ = p.ini.Close()
	}
	for _, file := range p.files {
		_ = file.Close()
	}
}
