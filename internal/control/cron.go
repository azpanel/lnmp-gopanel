package control

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os/exec"
	"strings"
	"time"

	"github.com/azpanel/lnmp-gopanel/internal/cron"
)

const cronOutputLimit = 1024 * 1024

type cronBuffer struct {
	data     bytes.Buffer
	limit    int
	overflow bool
}

func (b *cronBuffer) Write(p []byte) (int, error) {
	n := len(p)
	if len(p) > b.limit-b.data.Len() {
		b.overflow = true
		p = p[:b.limit-b.data.Len()]
	}
	_, _ = b.data.Write(p)
	return n, nil
}

type cronRunner func(context.Context, string, ...string) (string, string, error)

func cronCommand(ctx context.Context, limit int, program string, args ...string) (string, string, error) {
	cmd := exec.CommandContext(ctx, program, args...)
	cmd.Env = []string{"PATH=/usr/bin:/bin", "LANG=C", "LC_ALL=C"}
	configureProcess(cmd)
	stdout, stderr := &cronBuffer{limit: limit}, &cronBuffer{limit: 16 * 1024}
	cmd.Stdout, cmd.Stderr = stdout, stderr
	err := cmd.Run()
	if ctx.Err() != nil {
		return "", "", ctx.Err()
	}
	if stdout.overflow || stderr.overflow {
		return "", "", errors.New("定时任务返回数据过大")
	}
	return stdout.data.String(), stderr.data.String(), err
}

func RootCrontab(ctx context.Context) (cron.Snapshot, error) {
	program := firstExecutable("/usr/bin/crontab", "/bin/crontab", "/usr/sbin/crontab")
	if program == "" {
		return cron.Snapshot{}, errors.New("本机未安装 crontab")
	}
	if err := TrustedPath(program); err != nil {
		return cron.Snapshot{}, errors.New("crontab 程序权限不安全")
	}
	return readRootCrontab(ctx, program, func(ctx context.Context, program string, args ...string) (string, string, error) {
		return cronCommand(ctx, cronOutputLimit, program, args...)
	})
}

func readRootCrontab(ctx context.Context, program string, run cronRunner) (cron.Snapshot, error) {
	out, stderr, err := run(ctx, program, "-u", "root", "-l")
	if ctx.Err() != nil {
		return cron.Snapshot{}, errors.New("定时任务读取超时或已取消")
	}
	if err != nil {
		var exit *exec.ExitError
		// Cronie and Debian cron use exit 1 with this diagnostic for an absent
		// crontab. Other failures (including permission errors) must stay errors.
		if errors.As(err, &exit) && exit.ExitCode() == 1 && out == "" && strings.TrimSpace(stderr) == "no crontab for root" {
			return cron.Parse(""), nil
		}
		return cron.Snapshot{}, fmt.Errorf("无法读取 root crontab：%w", err)
	}
	result := cron.Parse(out)
	if strings.TrimSpace(stderr) != "" {
		result.Warnings = append(result.Warnings, "crontab 读取产生诊断信息，请检查系统 Cron 配置。")
	}
	return result, nil
}

func (h Helper) Cron(parent context.Context) (cron.Snapshot, error) {
	ctx, cancel := context.WithTimeout(parent, cron.Timeout+time.Second)
	defer cancel()
	// JSON escaping and the parsed task list can expand the original content.
	out, _, err := cronCommand(ctx, 16*cronOutputLimit, "/usr/bin/sudo", "-n", h.Path, "cron", "list")
	if err != nil {
		var result Result
		if json.Unmarshal([]byte(out), &result) == nil && result.Message != "" {
			return cron.Snapshot{}, errors.New(result.Message)
		}
		if ctx.Err() != nil {
			return cron.Snapshot{}, errors.New("定时任务读取超时或已取消")
		}
		return cron.Snapshot{}, fmt.Errorf("定时任务辅助程序不可用，请检查安装与 cron list 的 sudoers 权限：%w", err)
	}
	var result cron.Snapshot
	if json.Unmarshal([]byte(out), &result) != nil || result.Tasks == nil || result.Warnings == nil || result.UpdatedAt.IsZero() {
		return cron.Snapshot{}, errors.New("定时任务辅助程序返回无效数据")
	}
	return result, nil
}
