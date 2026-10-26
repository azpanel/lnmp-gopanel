//go:build linux

package control

import (
	"errors"
	"golang.org/x/sys/unix"
	"os"
	"syscall"
)

func LockNginx() (func(), error) {
	f, e := os.OpenFile("/run/lock/lnmp-gopanel-nginx.lock", os.O_CREATE|os.O_RDWR|unix.O_NOFOLLOW, 0600)
	if e != nil {
		return nil, e
	}
	info, e := f.Stat()
	if e != nil {
		f.Close()
		return nil, e
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok || stat.Uid != 0 || !info.Mode().IsRegular() || info.Mode().Perm()&0022 != 0 {
		f.Close()
		return nil, errors.New("Nginx 锁文件权限不安全")
	}
	if e := unix.Flock(int(f.Fd()), unix.LOCK_EX|unix.LOCK_NB); e != nil {
		f.Close()
		return nil, errors.New("Nginx 操作正在执行，请稍后再试")
	}
	return func() { _ = unix.Flock(int(f.Fd()), unix.LOCK_UN); _ = f.Close() }, nil
}
