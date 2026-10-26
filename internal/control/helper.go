package control

import (
	"context"
	"errors"
	"fmt"
	"os/exec"
	"strings"

	"github.com/azpanel/lnmp-gopanel/internal/config"
)

type CommandRunner func(context.Context, string, ...string) (string, error)

func Execute(ctx context.Context, s config.Service, action string, run CommandRunner) (Result, error) {
	if err := s.Validate(); err != nil {
		return Result{}, err
	}
	if !config.ValidAction(action) {
		return Result{}, ErrUnsupported
	}
	if s.Name == "redis" && action != "status" {
		return Result{}, ErrUnsupported
	}
	if action == "reload" {
		if s.Name != "nginx" || s.CheckProgram == "" {
			return Result{}, ErrUnsupported
		}
		if _, err := run(ctx, s.CheckProgram, "-t"); err != nil {
			return Result{State: "unknown", Message: "Nginx 配置检查失败，未执行重载"}, fmt.Errorf("nginx config test: %w", err)
		}
	}
	var args []string
	switch s.Manager {
	case "systemd":
		if action == "status" {
			args = []string{"is-active", s.Target}
		} else {
			args = []string{action, s.Target}
		}
	case "init":
		args = []string{action}
	case "lnmp":
		args = []string{s.Target, action}
	}
	output, err := run(ctx, s.Program, args...)
	if action == "status" {
		if ctx.Err() != nil {
			return Result{State: "unknown", Message: "状态检查超时"}, ctx.Err()
		}
		if s.Manager == "systemd" {
			switch strings.TrimSpace(output) {
			case "active":
				return Result{State: "running", Message: "服务运行中"}, nil
			case "inactive", "failed":
				return Result{State: "stopped", Message: output}, nil
			case "activating", "deactivating", "reloading":
				return Result{State: "transitioning", Message: output}, nil
			}
			return Result{State: "unknown", Message: "无法确认服务状态"}, nil
		}
		lower := strings.ToLower(output)
		if strings.Contains(lower, "not running") || strings.Contains(lower, "stopped") || strings.Contains(lower, "is dead") {
			return Result{State: "stopped", Message: "服务已停止"}, nil
		}
		if err == nil && (s.Manager == "init" || strings.Contains(lower, "running")) {
			return Result{State: "running", Message: "服务运行中"}, nil
		}
		var exit *exec.ExitError
		if s.Manager == "init" && errors.As(err, &exit) && exit.ExitCode() == 3 {
			return Result{State: "stopped", Message: "服务已停止"}, nil
		}
		return Result{State: "unknown", Message: "状态检查失败或脚本不支持 status"}, nil
	}
	if err != nil {
		msg := "服务操作失败，请检查系统服务日志"
		if ctx.Err() != nil {
			msg = "操作超时，请检查服务实际状态"
		}
		return Result{State: "unknown", Message: msg}, err
	}
	return Result{State: "unknown", Message: "操作已执行，正在更新服务状态"}, nil
}
