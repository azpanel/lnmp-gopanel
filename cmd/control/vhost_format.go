package main

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"time"

	"github.com/azpanel/lnmp-gopanel/internal/config"
	"github.com/azpanel/lnmp-gopanel/internal/control"
	"github.com/azpanel/lnmp-gopanel/internal/vhosts"
)

func handleVhostFormat() error {
	var r vhosts.FormatInput
	d := json.NewDecoder(io.LimitReader(os.Stdin, 4097))
	d.DisallowUnknownFields()
	if err := d.Decode(&r); err != nil {
		return errors.New("格式化请求无效")
	}
	var extra any
	if err := d.Decode(&extra); err != io.EOF {
		return errors.New("格式化请求存在多余内容")
	}
	if err := r.Validate(); err != nil {
		return err
	}
	if err := control.TrustedPath(serviceConfig); err != nil {
		return errors.New("服务配置权限不安全")
	}
	services, err := config.LoadServices(serviceConfig)
	if err != nil {
		return err
	}
	binary := ""
	for _, service := range services {
		if service.Name == "nginx" {
			binary = service.CheckProgram
		}
	}
	if binary == "" {
		return errors.New("未配置 Nginx 检查程序")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 35*time.Second)
	defer cancel()
	result, err := control.RootFormatVhost(ctx, r, binary)
	return vhostResponse(result, err)
}
