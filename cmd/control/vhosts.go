package main

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"syscall"
	"time"

	"github.com/azpanel/lnmp-gopanel/internal/config"
	"github.com/azpanel/lnmp-gopanel/internal/control"
	"github.com/azpanel/lnmp-gopanel/internal/vhosts"
)

type vhostRequest struct {
	Name    string `json:"name"`
	Enabled *bool  `json:"enabled,omitempty"`
}

func decodeVhostRequest() (vhostRequest, error) {
	var r vhostRequest
	d := json.NewDecoder(io.LimitReader(os.Stdin, 4097))
	d.DisallowUnknownFields()
	if e := d.Decode(&r); e != nil || !vhosts.ValidName(r.Name) {
		return r, errors.New("站点请求无效")
	}
	var extra any
	if e := d.Decode(&extra); e != io.EOF {
		return r, errors.New("站点请求无效")
	}
	return r, nil
}
func vhostResponse(data any, err error) error {
	if err != nil {
		return json.NewEncoder(os.Stdout).Encode(map[string]any{"message": err.Error()})
	}
	return json.NewEncoder(os.Stdout).Encode(map[string]any{"data": data})
}
func handleVhosts(action string) error {
	if action == "format" {
		return handleVhostFormat()
	}
	if action == "basic-auth-get" || action == "basic-auth-set" || action == "basic-auth-disable" {
		return handleVhostBasicAuth(action)
	}
	if action == "ssl-renew-preview" || action == "ssl-renew" {
		return handleVhostRenewal(action)
	}
	if action == "options" || action == "create" {
		return handleVhostProvision(action)
	}
	if action != "list" && action != "config" && action != "usage" && action != "toggle" {
		return errors.New("站点操作不受支持")
	}
	if e := control.TrustedPath(vhosts.Directory); e != nil {
		return errors.New("vhost 目录权限不安全或目录不存在")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 28*time.Second)
	defer cancel()
	store := vhosts.DefaultStore()
	switch action {
	case "list":
		sites, e := store.List(ctx)
		return vhostResponse(sites, e)
	case "config":
		r, e := decodeVhostRequest()
		if e != nil {
			return e
		}
		view, e := store.Read(r.Name)
		return vhostResponse(view, e)
	case "usage":
		if e := control.TrustedPath("/usr/bin/du"); e != nil {
			return errors.New("du 程序权限不安全")
		}
		_ = syscall.Setpriority(syscall.PRIO_PROCESS, 0, 19)
		scan, done := context.WithTimeout(context.Background(), 5*time.Minute)
		defer done()
		e := store.Usage(scan, func(u vhosts.Usage) { _ = vhostResponse(u, nil) })
		if e != nil {
			return vhostResponse(nil, e)
		}
		return nil
	case "toggle":
		r, e := decodeVhostRequest()
		if e != nil {
			return e
		}
		if r.Enabled == nil {
			return errors.New("缺少启用状态")
		}
		if e := control.TrustedPath(serviceConfig); e != nil {
			return errors.New("服务配置权限不安全")
		}
		services, e := config.LoadServices(serviceConfig)
		if e != nil {
			return e
		}
		binary := ""
		for _, s := range services {
			if s.Name == "nginx" {
				binary = s.CheckProgram
			}
		}
		if binary == "" {
			return errors.New("未配置 Nginx 检查程序，无法重载")
		}
		if e := control.TrustedPath(binary); e != nil {
			return errors.New("Nginx 程序权限不安全")
		}
		release, e := control.LockNginx()
		if e != nil {
			return e
		}
		defer release()
		source := vhosts.Directory + "/" + r.Name
		if _, e := os.Lstat(source); os.IsNotExist(e) {
			source += ".disabled"
		}
		if e := control.TrustedPath(source); e != nil {
			return errors.New("站点配置权限不安全或不存在")
		}
		check := func(parent context.Context) error {
			task, done := context.WithTimeout(parent, 8*time.Second)
			defer done()
			_, e := control.RunCommand(task, binary, "-t")
			return e
		}
		reload := func(parent context.Context) error {
			task, done := context.WithTimeout(parent, 8*time.Second)
			defer done()
			_, e := control.RunCommand(task, binary, "-s", "reload")
			return e
		}
		result, e := store.Toggle(ctx, r.Name, *r.Enabled, check, reload)
		return vhostResponse(result, e)
	}
	return nil
}
