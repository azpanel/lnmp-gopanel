package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/azpanel/lnmp-gopanel/internal/config"
	"github.com/azpanel/lnmp-gopanel/internal/metrics"
	"github.com/azpanel/lnmp-gopanel/internal/metricssetup"
	"golang.org/x/sys/unix"
	"golang.org/x/term"
)

func configureMetrics(args []string) error {
	if len(args) == 0 || (args[0] != "configure" && args[0] != "check") {
		return errors.New("用法：lnmp-gopanel metrics {configure|check} --config PATH")
	}
	flags := flag.NewFlagSet("metrics "+args[0], flag.ContinueOnError)
	path := flags.String("config", "/etc/lnmp-gopanel/config.yaml", "panel configuration")
	var only *string
	if args[0] == "check" {
		only = flags.String("only", "nginx,php_fpm,database", "comma-separated service metrics to check")
	}
	// Help must remain available to the installer before the root/TTY checks.
	if err := flags.Parse(args[1:]); err != nil {
		return err
	}
	if flags.NArg() != 0 {
		return errors.New("不接受额外参数")
	}
	if only != nil {
		c, err := config.Load(*path)
		if err != nil {
			return errors.New("无法读取有效的面板配置")
		}
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err = metrics.CheckServices(ctx, c.Metrics, strings.Split(*only, ",")); err != nil {
			return err
		}
		fmt.Println("所选服务指标采集检查通过。")
		return nil
	}
	if os.Geteuid() != 0 {
		return errors.New("监控配置向导需要 root 权限")
	}
	if !term.IsTerminal(int(os.Stdin.Fd())) {
		return errors.New("监控配置向导需要交互终端，请运行 sudo bash install.sh --configure-metrics")
	}
	// The installer already holds this same lock on inherited descriptor 9.
	// Validate that inherited descriptor before reusing it. Direct CLI callers
	// acquire the lock themselves, without replacing or unlinking its inode.
	release, err := metricsInstallLock()
	if err != nil {
		return err
	}
	defer release()
	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer cancel()
	terminalState, err := term.GetState(int(os.Stdin.Fd()))
	if err != nil {
		return errors.New("无法读取终端状态")
	}
	defer term.Restore(int(os.Stdin.Fd()), terminalState)
	console := &metricssetup.Console{Input: os.Stdin, Output: os.Stdout, Secret: func(prompt string) ([]byte, error) {
		fmt.Fprint(os.Stdout, prompt)
		type secretResult struct {
			data []byte
			err  error
		}
		result := make(chan secretResult, 1)
		go func() {
			password, readErr := term.ReadPassword(int(os.Stdin.Fd()))
			result <- secretResult{data: password, err: readErr}
		}()
		select {
		case value := <-result:
			fmt.Fprintln(os.Stdout)
			return value.data, value.err
		case <-ctx.Done():
			fmt.Fprintln(os.Stdout)
			return nil, ctx.Err()
		}
	}}
	return metricssetup.Configure(ctx, *path, console)
}
func metricsInstallLock() (func(), error) {
	return metricsInstallLockAt("/run/lnmp-gopanel-install.lock", 9)
}
func metricsInstallLockAt(path string, inheritedFD int) (func(), error) {
	fd, err := unix.Open(path, unix.O_CREAT|unix.O_RDWR|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0600)
	if err != nil {
		return nil, errors.New("无法打开安装锁")
	}
	failed := true
	defer func() {
		if failed {
			unix.Close(fd)
		}
	}()
	var actual unix.Stat_t
	if unix.Fstat(fd, &actual) != nil || actual.Uid != 0 || actual.Mode&unix.S_IFMT != unix.S_IFREG || actual.Mode&0022 != 0 {
		return nil, errors.New("安装锁文件权限不安全")
	}
	var inherited unix.Stat_t
	if fd != inheritedFD && unix.Fstat(inheritedFD, &inherited) == nil && inherited.Dev == actual.Dev && inherited.Ino == actual.Ino {
		// flock on the inherited open-file description verifies that it is locked
		// by this invocation, rather than trusting an environment-variable bypass.
		if unix.Flock(inheritedFD, unix.LOCK_EX|unix.LOCK_NB) == nil {
			unix.Close(fd)
			failed = false
			return func() {}, nil
		}
	}
	if unix.Flock(fd, unix.LOCK_EX|unix.LOCK_NB) != nil {
		return nil, errors.New("另一个面板安装、更新或配置操作正在执行")
	}
	failed = false
	return func() { unix.Flock(fd, unix.LOCK_UN); unix.Close(fd) }, nil
}
