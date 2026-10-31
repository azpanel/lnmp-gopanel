package control

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os/exec"

	"github.com/azpanel/lnmp-gopanel/internal/backups"
)

func (h Helper) backupCall(ctx context.Context, action string, input, output any) error {
	data, err := json.Marshal(input)
	if err != nil {
		return err
	}
	cmd := exec.CommandContext(ctx, "/usr/bin/sudo", "-n", h.Path, "backup-components", action)
	configureProcess(cmd)
	cmd.Stdin = bytes.NewReader(data)
	out, stderr := &limitedBuffer{}, &limitedBuffer{}
	cmd.Stdout, cmd.Stderr = out, stderr
	if err = cmd.Run(); err != nil {
		return errors.New("备份组件辅助程序不可用，请检查安装及 sudoers 授权")
	}
	var failure struct {
		Error string `json:"error"`
		Kind  string `json:"kind"`
	}
	if json.Unmarshal(out.data, &failure) != nil {
		return errors.New("备份组件辅助程序响应无效")
	}
	if failure.Error != "" {
		switch failure.Kind {
		case "busy":
			return fmt.Errorf("%w：%s", backups.ErrBusy, failure.Error)
		case "invalid":
			return fmt.Errorf("%w：%s", backups.ErrInvalid, failure.Error)
		}
		return errors.New(failure.Error)
	}
	return json.Unmarshal(out.data, output)
}
func (h Helper) BackupComponents(ctx context.Context) (v backups.Snapshot, err error) {
	err = h.backupCall(ctx, "inspect", struct{}{}, &v)
	return
}
func (h Helper) InstallBackupComponents(ctx context.Context, r backups.Request) (v backups.Job, err error) {
	err = h.backupCall(ctx, "install", r, &v)
	return
}
