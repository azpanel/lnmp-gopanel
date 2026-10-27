//go:build linux

package control

import (
	"errors"
	"os"
	"os/user"
	"strconv"
	"strings"

	"github.com/azpanel/lnmp-gopanel/internal/vhosts"
	"golang.org/x/sys/unix"
)

func prepareSiteRoot(path string) error {
	if !vhosts.SafeAbsolutePath(path) {
		return errors.New("站点绝对路径无效")
	}
	fd, err := unix.Open("/", unix.O_RDONLY|unix.O_DIRECTORY|unix.O_CLOEXEC, 0)
	if err != nil {
		return err
	}
	defer func() { _ = unix.Close(fd) }()
	parts := strings.Split(strings.TrimPrefix(path, "/"), "/")
	fresh := false
	for i, part := range parts {
		flags := unix.O_RDONLY | unix.O_DIRECTORY | unix.O_NOFOLLOW | unix.O_CLOEXEC
		next, e := unix.Openat(fd, part, flags, 0)
		if errors.Is(e, unix.ENOENT) {
			e = unix.Mkdirat(fd, part, 0755)
			if e == nil && i == len(parts)-1 {
				fresh = true
			}
			if e != nil && !errors.Is(e, unix.EEXIST) {
				return errors.New("无法创建站点目录")
			}
			next, e = unix.Openat(fd, part, flags, 0)
		}
		if e != nil {
			return errors.New("站点路径包含符号链接、非目录组件或不可访问目录")
		}
		_ = unix.Close(fd)
		fd = next
	}
	// Existing directories never receive ownership, permission or PHP changes.
	if !fresh {
		return nil
	}
	account, err := user.Lookup("www")
	if err != nil {
		return errors.New("未找到 LNMP www 用户")
	}
	uid, err := strconv.Atoi(account.Uid)
	if err != nil {
		return err
	}
	gid, err := strconv.Atoi(account.Gid)
	if err != nil {
		return err
	}
	ini, err := unix.Openat(fd, ".user.ini", unix.O_CREAT|unix.O_EXCL|unix.O_WRONLY|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0644)
	if err != nil {
		return errors.New("无法创建 PHP 隔离配置")
	}
	f := os.NewFile(uintptr(ini), ".user.ini")
	_, err = f.WriteString("open_basedir=\"" + strings.ReplaceAll(path, `"`, `\"`) + ":/tmp/:/proc/\"\n")
	if err == nil {
		err = f.Chown(uid, gid)
	}
	if err == nil {
		err = f.Chmod(0644)
	}
	closeErr := f.Close()
	if err != nil {
		return err
	}
	if closeErr != nil {
		return closeErr
	}
	if err = unix.Fchown(fd, uid, gid); err != nil {
		return err
	}
	return unix.Fchmod(fd, 0755)
}
