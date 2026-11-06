package control

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os/exec"
	"time"

	"github.com/azpanel/lnmp-gopanel/internal/backups"
)

type policyBuffer struct {
	bytes.Buffer
	overflow bool
	limit    int
}

func (b *policyBuffer) Write(p []byte) (int, error) {
	n := len(p)
	limit := b.limit
	if limit == 0 {
		limit = 1024 * 1024
	}
	remaining := limit - b.Len()
	if len(p) > remaining {
		b.overflow = true
		p = p[:remaining]
	}
	_, _ = b.Buffer.Write(p)
	return n, nil
}
func (h Helper) BackupPolicy(ctx context.Context, action string, input any) (json.RawMessage, error) {
	switch action {
	case "notification-test", "list", "get", "save", "delete", "databases", "tables", "files", "paths", "run", "runs", "run-get", "runtime", "artifacts":
	default:
		return nil, backups.ErrInvalid
	}
	data, err := json.Marshal(input)
	if err != nil || len(data) > backups.MaxPolicyBytes {
		return nil, backups.ErrInvalid
	}
	cmd := exec.CommandContext(ctx, "/usr/bin/sudo", "-n", h.Path, "backup-policies", action)
	configureProcess(cmd)
	if action == "artifacts" {
		// Allow the helper to cancel its remote child, release the account lock,
		// and remove the temporary credential environment on dialog close.
		configureProvisionProcess(cmd)
		cmd.WaitDelay = 3 * time.Second
	}
	cmd.Stdin = bytes.NewReader(data)
	out := &policyBuffer{}
	if action == "run-get" {
		out.limit = 2 * 1024 * 1024
	}
	cmd.Stdout = out
	cmd.Stderr = &limitedBuffer{}
	if err = cmd.Run(); err != nil {
		return nil, errors.New("备份政策辅助程序不可用，请检查安装及 sudoers 授权")
	}
	if out.overflow {
		return nil, errors.New("备份政策辅助程序响应过大")
	}
	var failure struct {
		Error string `json:"error"`
		Kind  string `json:"kind"`
	}
	if len(out.Bytes()) == 0 || !json.Valid(out.Bytes()) {
		return nil, errors.New("备份政策辅助程序响应无效")
	}
	// Successful list responses are arrays, so only decode error envelopes for objects.
	if out.Bytes()[0] == '{' {
		if err = json.Unmarshal(out.Bytes(), &failure); err != nil {
			return nil, err
		}
		if failure.Error != "" && failure.Kind != "" {
			var sentinel error
			switch failure.Kind {
			case "busy":
				sentinel = backups.ErrRunBusy
			case "invalid":
				sentinel = backups.ErrInvalid
			case "not_found":
				sentinel = backups.ErrNotFound
			case "conflict":
				sentinel = backups.ErrConflict
			}
			if sentinel != nil {
				if failure.Error == sentinel.Error() {
					return nil, sentinel
				}
				return nil, fmt.Errorf("%w：%s", sentinel, failure.Error)
			}
			return nil, errors.New(failure.Error)
		}
	}
	return append(json.RawMessage(nil), out.Bytes()...), nil
}
