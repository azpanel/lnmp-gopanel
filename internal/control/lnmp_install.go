package control

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os/exec"

	"github.com/azpanel/lnmp-gopanel/internal/lnmpinstall"
)

// Installation reads need larger bounded responses than the service helper's
// 16 KiB buffer; compilation output is paginated, never returned in one response.
func (h Helper) installCall(ctx context.Context, action string, input, output any) error {
	data, err := json.Marshal(input)
	if err != nil {
		return err
	}
	cmd := exec.CommandContext(ctx, "/usr/bin/sudo", "-n", h.Path, "lnmp-install", action)
	configureProcess(cmd)
	cmd.Stdin = bytes.NewReader(data)
	var out bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &limitedBuffer{}
	if err := cmd.Run(); err != nil {
		return errors.New("安装辅助程序不可用，请检查 lnmp-install 的 sudoers 授权")
	}
	if out.Len() > 1024*1024 {
		return errors.New("安装辅助程序响应过大")
	}
	var failure struct {
		Error    string `json:"error"`
		Conflict bool   `json:"conflict"`
	}
	if json.Unmarshal(out.Bytes(), &failure) != nil {
		return errors.New("安装辅助程序响应无效")
	}
	if failure.Error != "" {
		if failure.Conflict {
			return fmt.Errorf("%w：%s", lnmpinstall.ErrConflict, failure.Error)
		}
		return errors.New(failure.Error)
	}
	return json.Unmarshal(out.Bytes(), output)
}
func (h Helper) InstallInspect(ctx context.Context) (v lnmpinstall.Snapshot, err error) {
	err = h.installCall(ctx, "inspect", struct{}{}, &v)
	return
}
func (h Helper) InstallPrepare(ctx context.Context, id string) (v lnmpinstall.Job, err error) {
	err = h.installCall(ctx, "prepare", struct {
		ID string `json:"id"`
	}{id}, &v)
	return
}
func (h Helper) InstallStart(ctx context.Context, r lnmpinstall.Request) (v lnmpinstall.Job, err error) {
	err = h.installCall(ctx, "start", r, &v)
	return
}
func (h Helper) InstallRead(ctx context.Context, r lnmpinstall.ReadRequest) (v lnmpinstall.LogPage, err error) {
	err = h.installCall(ctx, "read", r, &v)
	return
}
