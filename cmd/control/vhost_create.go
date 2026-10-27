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

func handleVhostProvision(action string) error {
	parent, stop := signal.NotifyContext(context.Background(), syscall.SIGTERM, syscall.SIGINT)
	defer stop()
	ctx, cancel := context.WithTimeout(parent, vhosts.CreateTimeout)
	defer cancel()
	if action == "options" {
		task, done := context.WithTimeout(ctx, 12*time.Second)
		defer done()
		out, err := control.RootVhostOptions(task)
		return vhostResponse(out, err)
	}
	var req vhosts.CreateRequest
	decoder := json.NewDecoder(io.LimitReader(os.Stdin, 32*1024+1))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&req); err != nil {
		return errors.New("建站请求无效或过大")
	}
	var extra any
	if decoder.Decode(&extra) != io.EOF {
		return errors.New("建站请求存在多余内容")
	}
	if err := req.Validate(); err != nil {
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
	for _, svc := range services {
		if svc.Name == "nginx" {
			binary = svc.CheckProgram
		}
	}
	if binary == "" {
		return errors.New("未配置 Nginx 检查程序")
	}
	encoder := json.NewEncoder(os.Stdout)
	result, err := control.RootCreateVhost(ctx, req, binary, func(event vhosts.CreateEvent) { _ = encoder.Encode(map[string]any{"data": event}) })
	envelope := map[string]any{"data": vhosts.CreateEvent{Result: &result}, "complete": true}
	if err != nil {
		envelope["message"] = err.Error()
	}
	return encoder.Encode(envelope)
}
