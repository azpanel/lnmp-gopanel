//go:build !linux

package logs

import (
	"errors"
	"os"
)

func (l *LoginLog) open(bool) (*os.File, error) {
	return nil, errors.New("登录日志仅支持 Linux")
}
