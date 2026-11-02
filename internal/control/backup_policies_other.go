//go:build !linux

package control

import (
	"context"
	"errors"
	"io"
)

func HandleBackupPolicy(context.Context, string, io.Reader, io.Writer) error {
	return errors.New("备份政策仅支持 Linux")
}
func DispatchBackupPolicies() error      { return errors.New("备份政策仅支持 Linux") }
func RunBackupPolicyWorker(string) error { return errors.New("备份政策仅支持 Linux") }
