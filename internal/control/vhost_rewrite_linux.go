//go:build linux

package control

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"syscall"

	"github.com/azpanel/lnmp-gopanel/internal/vhosts"
	"golang.org/x/sys/unix"
)

// LNMP rewrite templates can retain group/world write bits from the installer.
// Secure root-owned templates before using them, without relaxing TrustedPath
// for other configs, executables, or certificate material.
func trustedNginxConfigPath(path string) error {
	return trustedRewritePathAt(path, filepath.Join(vhosts.Prefix, "conf/rewrite"))
}

func trustedRewritePathAt(path, directory string) error {
	if filepath.Clean(path) != path || filepath.Dir(path) != directory || !strings.HasSuffix(filepath.Base(path), ".conf") {
		return TrustedPath(path)
	}
	if e := TrustedPath(directory); e != nil {
		return e
	}
	// Do not follow symlinks or block on special files. Inspect and chmod the
	// same inode, and avoid changing permissions of hard-linked files elsewhere.
	fd, e := unix.Open(path, unix.O_RDONLY|unix.O_CLOEXEC|unix.O_NOFOLLOW|unix.O_NONBLOCK, 0)
	if e != nil {
		return e
	}
	f := os.NewFile(uintptr(fd), path)
	defer f.Close()
	info, e := f.Stat()
	if e != nil {
		return e
	}
	st, ok := info.Sys().(*syscall.Stat_t)
	if !ok || !info.Mode().IsRegular() || st.Uid != 0 || st.Nlink != 1 {
		return fmt.Errorf("伪静态模板必须是 root 所有的普通文件且不能有硬链接：%s", path)
	}
	if info.Mode().Perm()&0022 != 0 {
		if e = f.Chmod(info.Mode().Perm() &^ 0022); e != nil {
			return e
		}
	}
	return TrustedPath(path)
}
