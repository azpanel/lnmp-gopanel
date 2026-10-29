//go:build linux

package logs

import (
	"errors"
	"os"
	"path/filepath"
	"syscall"

	"golang.org/x/sys/unix"
)

func loginPrivate(info os.FileInfo, directory bool) bool {
	stat, ok := info.Sys().(*syscall.Stat_t)
	mode := os.FileMode(0600)
	if directory {
		mode = 0700
	}
	return ok && int(stat.Uid) == os.Geteuid() && info.Mode().Perm() == mode &&
		info.IsDir() == directory && (directory || info.Mode().IsRegular() && stat.Nlink == 1)
}

func (l *LoginLog) open(write bool) (*os.File, error) {
	dir := filepath.Dir(l.path)
	if write {
		if err := os.MkdirAll(dir, 0700); err != nil {
			return nil, err
		}
	}
	parent, err := openPath(dir, true)
	if err != nil {
		return nil, err
	}
	defer parent.Close()
	info, err := parent.Stat()
	if err != nil || !loginPrivate(info, true) {
		return nil, errors.New("登录日志目录必须属于面板账号且权限为 0700")
	}
	flags := unix.O_RDONLY
	if write {
		flags = unix.O_WRONLY | unix.O_APPEND | unix.O_CREAT
	}
	fd, err := unix.Openat(int(parent.Fd()), "login.log", flags|unix.O_CLOEXEC|unix.O_NOFOLLOW|unix.O_NONBLOCK, 0600)
	if err != nil {
		return nil, err
	}
	f := os.NewFile(uintptr(fd), l.path)
	info, err = f.Stat()
	if err != nil || !loginPrivate(info, false) {
		f.Close()
		return nil, errors.New("登录日志必须是面板账号所有的 0600 普通文件且不能是链接")
	}
	return f, nil
}
