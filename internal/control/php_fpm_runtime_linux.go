//go:build linux

package control

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/azpanel/lnmp-gopanel/internal/config"
	"golang.org/x/sys/unix"
)

const phpFPMPIDFile = "/usr/local/php/var/run/php-fpm.pid"
const phpFPMLaunchMarker = "lnmp-gopanel-php-fpm-standard-v1"
const phpFPMLaunchFDTarget = "/memfd:" + phpFPMLaunchMarker + " (deleted)"

// FPM overwrites the original argument and environment strings in /proc.
// An empty, sealed descriptor survives FPM's graceful re-exec.
func LaunchStandardPHPFPM(mode string) error {
	var option string
	switch mode {
	case "foreground":
		option = "--nodaemonize"
	case "daemon":
		option = "--daemonize"
	default:
		return ErrUnsupported
	}
	var env []string
	for _, value := range os.Environ() {
		if strings.HasPrefix(value, "PHPRC=") || strings.HasPrefix(value, "PHP_INI_SCAN_DIR=") {
			return errors.New("PHP-FPM 设置了自定义 INI 环境，无法使用标准启动入口")
		}
		if !strings.HasPrefix(value, "FPM_SOCKETS") {
			env = append(env, value)
		}
	}
	for _, path := range []string{loggingPHPBinary, phpINIConfig, loggingPHPConfig, filepath.Dir(phpFPMPIDFile)} {
		if err := TrustedPath(path); err != nil {
			return fmt.Errorf("无法验证标准 PHP-FPM 路径 %s：%w", path, err)
		}
	}
	fd, err := unix.MemfdCreate(phpFPMLaunchMarker, unix.MFD_ALLOW_SEALING)
	if err != nil {
		return fmt.Errorf("无法创建 PHP-FPM 启动凭据：%w", err)
	}
	defer unix.Close(fd)
	if _, err = unix.FcntlInt(uintptr(fd), unix.F_ADD_SEALS, unix.F_SEAL_WRITE|unix.F_SEAL_SHRINK|unix.F_SEAL_GROW|unix.F_SEAL_SEAL); err != nil {
		return fmt.Errorf("无法锁定 PHP-FPM 启动凭据：%w", err)
	}
	return syscall.Exec(loggingPHPBinary, []string{
		loggingPHPBinary, option, "-c", phpINIConfig,
		"--fpm-config", loggingPHPConfig, "--pid", phpFPMPIDFile,
	}, env)
}

func attestPHPConfigRuntime(ctx context.Context) (int, error) {
	return attestPHPConfigRuntimeAt(ctx, "/proc", loggingPHPBinary)
}

func attestPHPConfigRuntimeAt(ctx context.Context, proc, program string) (int, error) {
	binary, err := filepath.EvalSymlinks(program)
	if err != nil {
		return 0, errors.New("无法识别标准 PHP-FPM 程序")
	}
	entries, err := os.ReadDir(proc)
	if err != nil {
		return 0, errors.New("无法检查 PHP-FPM 主进程")
	}
	master := 0
	for _, entry := range entries {
		if err := ctx.Err(); err != nil {
			return 0, err
		}
		pid, err := strconv.Atoi(entry.Name())
		if err != nil || pid <= 0 {
			continue
		}
		base := filepath.Join(proc, entry.Name())
		exe, err := os.Readlink(filepath.Join(base, "exe"))
		if err != nil || exe != binary {
			continue
		}
		cmd, err := os.ReadFile(filepath.Join(base, "cmdline"))
		if err != nil {
			continue
		}
		title := strings.TrimSpace(strings.ReplaceAll(string(cmd), "\x00", " "))
		if !strings.HasPrefix(title, "php-fpm: master process ") {
			continue
		}
		if master != 0 {
			return 0, errors.New("未找到唯一的标准 PHP-FPM 主进程，无法确认配置来源")
		}
		if title != "php-fpm: master process ("+loggingPHPConfig+")" {
			return 0, errors.New("PHP-FPM 使用自定义配置，无法确认全局配置来源")
		}
		info, err := os.Stat(base)
		if err != nil {
			return 0, errors.New("无法检查 PHP-FPM 主进程所有者")
		}
		stat, ok := info.Sys().(*syscall.Stat_t)
		if !ok || stat.Uid != 0 {
			return 0, errors.New("PHP-FPM 主进程不是 root，无法确认配置来源")
		}
		fdDir := filepath.Join(base, "fd")
		fds, err := os.ReadDir(fdDir)
		if err != nil {
			return 0, errors.New("无法检查 PHP-FPM 启动凭据")
		}
		marked := false
		for _, fd := range fds {
			if target, err := os.Readlink(filepath.Join(fdDir, fd.Name())); err == nil && target == phpFPMLaunchFDTarget {
				marked = true
				break
			}
		}
		if !marked {
			return 0, errors.New("无法确认 PHP-FPM 启动参数。请按 README 配置标准启动入口并重启服务，或通过 SSH 管理配置")
		}
		master = pid
	}
	if master == 0 {
		return 0, errors.New("未找到唯一的标准 PHP-FPM 主进程，无法确认配置来源")
	}
	return master, nil
}

type phpFPMPID struct {
	file *os.File
	info os.FileInfo
	pid  int
}

func (p phpConfigController) openPIDFile() (phpFPMPID, error) {
	path := p.path(phpFPMPIDFile)
	if err := p.trust(filepath.Dir(path)); err != nil {
		return phpFPMPID{}, errors.New("PHP-FPM PID 文件目录权限不安全")
	}
	file, err := os.OpenFile(path, os.O_RDONLY|syscall.O_NOFOLLOW|syscall.O_NONBLOCK, 0)
	if err != nil {
		return phpFPMPID{}, errors.New("无法打开标准 PHP-FPM PID 文件")
	}
	info, err := file.Stat()
	if err == nil {
		stat, ok := info.Sys().(*syscall.Stat_t)
		if ok && stat.Uid == 0 && info.Mode().IsRegular() && info.Mode().Perm()&0022 == 0 {
			data, readErr := io.ReadAll(io.LimitReader(file, 33))
			pid, parseErr := strconv.Atoi(strings.TrimSpace(string(data)))
			if readErr == nil && len(data) <= 32 && parseErr == nil && pid > 0 {
				return phpFPMPID{file, info, pid}, nil
			}
		}
	}
	file.Close()
	return phpFPMPID{}, errors.New("标准 PHP-FPM PID 文件不安全或无效")
}

func (p phpConfigController) masterPID(ctx context.Context) (phpFPMPID, error) {
	master, err := p.attest(ctx)
	if err != nil {
		return phpFPMPID{}, err
	}
	pidFile, err := p.openPIDFile()
	if err != nil {
		return phpFPMPID{}, err
	}
	if master != pidFile.pid {
		pidFile.file.Close()
		return phpFPMPID{}, errors.New("标准 PHP-FPM PID 文件与主进程不匹配")
	}
	return pidFile, nil
}

func (p phpConfigController) reload(ctx context.Context, s config.Service) (bool, error) {
	before, err := p.masterPID(ctx)
	if err != nil {
		return false, err
	}
	defer before.file.Close()
	if _, err := p.execute(ctx, s, "reload"); err != nil {
		return true, fmt.Errorf("PHP-FPM 重载失败：%w", err)
	}
	task, done := context.WithTimeout(ctx, 8*time.Second)
	defer done()
	return true, p.waitReload(task, s, before.info)
}

// FPM drains the old workers and re-executes. The new master can have the same
// PID, but recreates its PID file after initializing configuration and sockets.
// Keep the old file open so the filesystem cannot reuse its inode.
func (p phpConfigController) waitReload(ctx context.Context, s config.Service, before os.FileInfo) error {
	ticker := time.NewTicker(150 * time.Millisecond)
	defer ticker.Stop()
	var observed os.FileInfo
	for ctx.Err() == nil {
		var generation os.FileInfo
		current, err := p.openPIDFile()
		if err == nil {
			current.file.Close()
			if !os.SameFile(before, current.info) {
				master, verifyErr := p.attest(ctx)
				state, statusErr := p.execute(ctx, s, "status")
				if verifyErr == nil && master == current.pid && statusErr == nil && state.State == "running" && ctx.Err() == nil {
					if observed != nil && os.SameFile(observed, current.info) {
						return nil
					}
					generation = current.info
				}
			}
		}
		observed = generation
		select {
		case <-ctx.Done():
		case <-ticker.C:
		}
	}
	return errors.New("未能确认 PHP-FPM 完成重载并恢复运行")
}
