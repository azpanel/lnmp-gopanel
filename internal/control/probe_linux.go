//go:build linux

package control

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/azpanel/lnmp-gopanel/internal/config"
)

// LNMP's nginx and PHP-FPM init scripts do not consistently implement status.
// Inspect exact executable paths instead of trusting a script's usage output.
func NativeStatus(ctx context.Context, s config.Service) (Result, bool) {
	if s.Manager == "systemd" || (s.Name != "nginx" && s.Name != "php-fpm" && s.Name != "redis") {
		return Result{}, false
	}
	return probeProcesses(ctx, "/proc", serviceBinaries(s.Name)), true
}
func probeProcesses(ctx context.Context, proc string, programs []string) Result {
	expected := map[string]bool{}
	for _, p := range programs {
		if executable(p) {
			resolved, e := filepath.EvalSymlinks(p)
			if e == nil {
				expected[resolved] = true
			}
		}
	}
	if len(expected) == 0 {
		return Result{State: "unknown", Message: "未找到可识别的服务可执行文件"}
	}
	entries, err := os.ReadDir(proc)
	if err != nil {
		return Result{State: "unknown", Message: "无法读取进程状态"}
	}
	incomplete := false
	for _, entry := range entries {
		if ctx.Err() != nil {
			return Result{State: "unknown", Message: "进程状态检查超时"}
		}
		if !entry.IsDir() {
			continue
		}
		if _, e := strconv.Atoi(entry.Name()); e != nil {
			continue
		}
		target, e := os.Readlink(filepath.Join(proc, entry.Name(), "exe"))
		if e != nil {
			if errors.Is(e, os.ErrPermission) {
				incomplete = true
			}
			continue
		}
		target = strings.TrimSuffix(target, " (deleted)")
		if expected[target] {
			return Result{State: "running", Message: "服务进程运行中"}
		}
	}
	if incomplete {
		return Result{State: "unknown", Message: "部分进程不可读取，无法确认服务状态"}
	}
	return Result{State: "stopped", Message: "未发现服务进程"}
}
