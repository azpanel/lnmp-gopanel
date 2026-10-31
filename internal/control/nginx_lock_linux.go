//go:build linux

package control

import (
	"errors"
	"fmt"
	"golang.org/x/sys/unix"
	"os"
	"syscall"
)

func LockNginx() (func(), error) {
	return LockService("nginx")
}

// Database aliases share one lock because LNMP may manage MariaDB as mysql.
func LockService(name string) (func(), error) {
	switch name {
	case "mysql", "mariadb":
		name = "database"
	case "nginx", "php-fpm":
	default:
		return nil, ErrUnsupported
	}
	f, e := os.OpenFile("/run/lock/lnmp-gopanel-"+name+".lock", os.O_CREATE|os.O_RDWR|unix.O_NOFOLLOW, 0600)
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
		return nil, errors.New("服务锁文件权限不安全")
	}
	if e := unix.Flock(int(f.Fd()), unix.LOCK_EX|unix.LOCK_NB); e != nil {
		f.Close()
		return nil, fmt.Errorf("%w：%s", ErrBusy, name)
	}
	return func() { _ = unix.Flock(int(f.Fd()), unix.LOCK_UN); _ = f.Close() }, nil
}
