//go:build !linux

package control

import (
	"context"
	"errors"
)

func RootLoggingStatus(context.Context) (LoggingSnapshot, error) {
	return LoggingSnapshot{}, errors.New("日志设置仅支持 Linux")
}
func RootEnableLogging(context.Context, string) (Result, error) {
	return Result{}, errors.New("日志设置仅支持 Linux")
}
