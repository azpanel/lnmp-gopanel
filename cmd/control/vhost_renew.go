package main

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/azpanel/lnmp-gopanel/internal/config"
	"github.com/azpanel/lnmp-gopanel/internal/control"
	"github.com/azpanel/lnmp-gopanel/internal/vhosts"
)

func handleVhostRenewal(action string) error {
	parent, stop := signal.NotifyContext(context.Background(), syscall.SIGTERM, syscall.SIGINT)
	defer stop()
	timeout := vhosts.CreateTimeout
	if action == "ssl-renew-preview" {
		timeout = 60 * time.Second
	}
	ctx, cancel := context.WithTimeout(parent, timeout)
	defer cancel()
	d := json.NewDecoder(io.LimitReader(os.Stdin, 1024*1024+1))
	d.DisallowUnknownFields()
	if action == "ssl-renew-preview" {
		var input struct {
			Names []string `json:"names"`
		}
		if err := d.Decode(&input); err != nil {
			return errors.New("续签检查参数无效")
		}
		var extra any
		if d.Decode(&extra) != io.EOF {
			return errors.New("续签检查参数存在多余内容")
		}
		out, err := control.RootPreviewRenewal(ctx, input.Names)
		return vhostResponse(out, err)
	}
	var input vhosts.RenewRequest
	if err := d.Decode(&input); err != nil {
		return errors.New("续签参数无效")
	}
	var extra any
	if d.Decode(&extra) != io.EOF {
		return errors.New("续签参数存在多余内容")
	}
	if err := input.Validate(); err != nil {
		return err
	}
	if len(input.Items) != 1 {
		return errors.New("辅助程序每次只处理一个站点")
	}
	if err := control.TrustedPath(serviceConfig); err != nil {
		return err
	}
	services, err := config.LoadServices(serviceConfig)
	if err != nil {
		return err
	}
	binary := ""
	for _, svc := range services {
		if svc.Name == "nginx" {
			binary = svc.CheckProgram
		}
	}
	if binary == "" {
		return errors.New("未配置 Nginx 检查程序")
	}
	encoder := json.NewEncoder(os.Stdout)
	err = control.RootRenewVhost(ctx, input.RequestID, input.Items[0], func(event vhosts.CreateEvent) { _ = encoder.Encode(map[string]any{"data": event}) }, binary)
	final := map[string]any{"complete": true}
	if err != nil {
		final["message"] = err.Error()
	}
	return encoder.Encode(final)
}
