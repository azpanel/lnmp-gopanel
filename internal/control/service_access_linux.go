//go:build linux

package control

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"os/user"
	"strconv"
	"strings"
	"syscall"
	"time"

	"golang.org/x/sys/unix"
)

// Probe pinned inodes as the service account in a separate process. This lets
// the kernel apply ACLs and supplementary groups without changing the helper's
// credentials or following a replacement of a previously inspected pathname.
type serviceAccess struct {
	uid, gid int
	groups   []uint32
}

func (a serviceAccess) checkFD(fd int, path string, modes ...string) error {
	copyFD, err := unix.FcntlInt(uintptr(fd), unix.F_DUPFD_CLOEXEC, 0)
	if err != nil {
		return err
	}
	file := os.NewFile(uintptr(copyFD), path)
	defer file.Close()
	return a.check(file, path, modes...)
}

func serviceAccessFor(uid, gid int) (serviceAccess, error) {
	a := serviceAccess{uid: uid, gid: gid, groups: []uint32{}}
	if uid < 0 || gid < 0 {
		return a, errors.New("服务运行账号无效")
	}
	u, err := user.LookupId(strconv.Itoa(uid))
	if err != nil {
		var unknown user.UnknownUserIdError
		if errors.As(err, &unknown) {
			// Numeric service identities need not have a passwd entry.
			return a, nil
		}
		return a, fmt.Errorf("无法查询服务运行账号：%w", err)
	}
	groups, err := u.GroupIds()
	if err != nil {
		return a, fmt.Errorf("无法查询服务运行账号的附加组：%w", err)
	}
	for _, group := range groups {
		id, err := strconv.ParseUint(group, 10, 32)
		if err != nil {
			return a, errors.New("服务运行账号的附加组无效")
		}
		a.groups = append(a.groups, uint32(id))
	}
	return a, nil
}

func (a serviceAccess) check(file *os.File, path string, modes ...string) error {
	const program = "/usr/bin/test"
	if err := TrustedPath(program); err != nil {
		return errors.New("需要可信的 /usr/bin/test 检查服务访问权限")
	}
	var args []string
	var permissions []string
	for _, mode := range modes {
		if mode != "-r" && mode != "-w" && mode != "-x" {
			return errors.New("服务访问权限检查参数无效")
		}
		if len(args) > 0 {
			args = append(args, "-a")
		}
		args = append(args, mode, "/proc/self/fd/3")
		permissions = append(permissions, map[string]string{"-r": "读取", "-w": "写入", "-x": "穿越"}[mode])
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, program, args...)
	cmd.Env = []string{"PATH=/usr/bin:/bin", "LANG=C", "LC_ALL=C"}
	cmd.Dir = "/"
	cmd.ExtraFiles = []*os.File{file}
	if os.Geteuid() == 0 {
		cmd.SysProcAttr = &syscall.SysProcAttr{Credential: &syscall.Credential{
			Uid: uint32(a.uid), Gid: uint32(a.gid), Groups: a.groups,
		}}
	} else if a.uid != os.Geteuid() || a.gid != os.Getegid() {
		return errors.New("无法切换到服务运行账号检查访问权限")
	}
	if err := cmd.Run(); err != nil {
		var exit *exec.ExitError
		if errors.As(err, &exit) && exit.ExitCode() == 1 {
			return fmt.Errorf("运行账号 UID=%d GID=%d 对 %s 缺少%s权限，请检查该路径权限", a.uid, a.gid, path, strings.Join(permissions, "/"))
		}
		return fmt.Errorf("无法检查服务对 %s 的访问权限：%w", path, err)
	}
	return nil
}
