//go:build !linux

package logs

import (
	"context"
	"errors"
	"os"
)

func fileIdentity(info os.FileInfo) string { return info.Name() }

type Store struct{}

func (Store) List(context.Context) (Catalog, error) {
	return Catalog{}, errors.New("日志读取仅支持 Linux")
}
func (Store) Read(context.Context, Request) (Window, error) {
	return Window{}, errors.New("日志读取仅支持 Linux")
}
