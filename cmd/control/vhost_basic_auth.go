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

func handleVhostBasicAuth(action string) error {
	var r vhosts.BasicAuthInput
	d := json.NewDecoder(io.LimitReader(os.Stdin, 4097))
	d.DisallowUnknownFields()
	if err := d.Decode(&r); err != nil || !vhosts.ValidName(r.Name) {
		return errors.New("认证请求无效")
	}
	var extra any
	if err := d.Decode(&extra); err != io.EOF {
		return errors.New("认证请求无效")
	}
	if action != "basic-auth-set" && (r.Username != "" || r.Password != "") {
		return errors.New("读取和关闭认证不接受凭证")
	}
	if err := control.TrustedPath(serviceConfig); err != nil {
		return errors.New("服务配置权限不安全")
	}
	services, err := config.LoadServices(serviceConfig)
	if err != nil {
		return err
	}
	binary := ""
	for _, s := range services {
		if s.Name == "nginx" {
			binary = s.CheckProgram
		}
	}
	if binary == "" {
		return errors.New("未配置 Nginx 检查程序")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 40*time.Second)
	defer cancel()
	state, err := control.RootVhostBasicAuth(ctx, action, r, binary)
	return vhostResponse(state, err)
}
