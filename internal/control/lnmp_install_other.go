//go:build !linux

package control

import (
	"context"
	"errors"
	"io"
)

func HandleLNMPInstall(context.Context, string, io.Reader, io.Writer) error {
	return errors.New("在线安装仅支持 Linux")
}
func RunLNMPInstallWorker(string, string) error { return errors.New("在线安装仅支持 Linux") }
