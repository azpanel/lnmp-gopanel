//go:build !linux

package control

import (
	"context"
	"errors"
	"io"
)

func HandleBackupComponents(context.Context, string, io.Reader, io.Writer) error {
	return errors.New("备份组件安装仅支持 Linux")
}
func RunBackupComponentsWorker(string) error { return errors.New("备份组件安装仅支持 Linux") }
