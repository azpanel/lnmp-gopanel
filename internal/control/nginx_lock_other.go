//go:build !linux

package control

import "errors"

func LockNginx() (func(), error) { return nil, errors.New("Nginx 配置操作仅支持 Linux") }
