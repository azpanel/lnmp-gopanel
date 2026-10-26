package control

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"os/exec"

	"github.com/azpanel/lnmp-gopanel/internal/storage"
)

func (h Helper) Scan(ctx context.Context, report func(storage.Progress)) error {
	cmd := exec.CommandContext(ctx, "/usr/bin/sudo", "-n", h.Path, "storage", "scan")
	cmd.Env = []string{"PATH=/usr/bin:/bin", "LANG=C", "LC_ALL=C"}
	configureProcess(cmd)
	cmd.Stderr = io.Discard
	pipe, err := cmd.StdoutPipe()
	if err != nil {
		return errors.New("无法创建文件扫描管道")
	}
	if err := cmd.Start(); err != nil {
		return errors.New("无法启动文件扫描辅助程序")
	}
	decoder := json.NewDecoder(pipe)
	complete := false
	for {
		var progress storage.Progress
		if err := decoder.Decode(&progress); err != nil {
			if err != io.EOF {
				_ = cmd.Process.Kill()
			}
			break
		}
		// Error records from the helper are not progress snapshots. Keep any
		// completed partial inventory instead of replacing it with empty fields.
		if progress.Files == nil {
			continue
		}
		report(progress)
		complete = progress.Complete
	}
	err = cmd.Wait()
	if ctx.Err() != nil {
		return errors.New("文件扫描超时或已取消")
	}
	if err != nil && !complete {
		return errors.New("文件扫描辅助程序不可用，请检查安装与 storage scan 的 sudoers 权限")
	}
	if !complete {
		return errors.New("扫描未完整返回，请重试")
	}
	return nil
}
