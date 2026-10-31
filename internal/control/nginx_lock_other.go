//go:build !linux

package control

import "errors"

func LockNginx() (func(), error)         { return nil, errors.New("Nginx 配置操作仅支持 Linux") }
func LockService(string) (func(), error) { return nil, errors.New("服务配置操作仅支持 Linux") }
