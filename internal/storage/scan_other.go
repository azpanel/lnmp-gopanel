//go:build !linux

package storage

import (
	"context"
	"errors"
)

func ScanFiles(ctx context.Context, root string, report func(Progress)) error {
	return errors.New("文件排行扫描仅支持 Linux")
}
