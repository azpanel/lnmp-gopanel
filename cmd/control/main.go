package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"time"

	"github.com/azpanel/lnmp-gopanel/internal/components"
	"github.com/azpanel/lnmp-gopanel/internal/config"
	"github.com/azpanel/lnmp-gopanel/internal/control"
	"github.com/azpanel/lnmp-gopanel/internal/cron"
	"github.com/azpanel/lnmp-gopanel/internal/docker"
	"github.com/azpanel/lnmp-gopanel/internal/storage"
	"syscall"
)

const serviceConfig = "/etc/lnmp-gopanel/services.yaml"

func main() {
	if err := run(); err != nil {
		state := "unknown"
		if errors.Is(err, control.ErrManagementConflict) {
			state = "unmanaged"
		}
		_ = json.NewEncoder(os.Stdout).Encode(control.Result{State: state, Message: err.Error()})
		os.Exit(1)
	}
}
func run() error {
	if os.Geteuid() != 0 {
		return fmt.Errorf("辅助程序需要 root 权限")
	}
	if len(os.Args) == 3 && os.Args[1] == "nginx" && (os.Args[2] == "repair-preview" || os.Args[2] == "repair") {
		return handleNginxManagement(os.Args[2])
	}
	if len(os.Args) == 4 && os.Args[1] == "lnmp-install-worker" {
		return control.RunLNMPInstallWorker(os.Args[2], os.Args[3])
	}
	if len(os.Args) == 3 && os.Args[1] == "lnmp-install" {
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()
		return control.HandleLNMPInstall(ctx, os.Args[2], os.Stdin, os.Stdout)
	}
	if len(os.Args) == 3 && os.Args[1] == "components" && os.Args[2] == "snapshot" {
		ctx, cancel := context.WithTimeout(context.Background(), components.Timeout)
		defer cancel()
		return json.NewEncoder(os.Stdout).Encode(control.ComponentSnapshot(ctx))
	}
	if len(os.Args) == 3 && os.Args[1] == "logs" {
		return handleLogs(os.Args[2])
	}
	if len(os.Args) == 3 && os.Args[1] == "cron" && os.Args[2] == "list" {
		ctx, cancel := context.WithTimeout(context.Background(), cron.Timeout)
		defer cancel()
		result, err := control.RootCrontab(ctx)
		if err != nil {
			return err
		}
		return json.NewEncoder(os.Stdout).Encode(result)
	}
	if len(os.Args) == 3 && os.Args[1] == "docker" && os.Args[2] == "snapshot" {
		ctx, cancel := context.WithTimeout(context.Background(), docker.Timeout)
		defer cancel()
		result, err := control.DockerSnapshot(ctx)
		if err != nil {
			return err
		}
		return json.NewEncoder(os.Stdout).Encode(result)
	}
	if len(os.Args) == 3 && os.Args[1] == "vhosts" {
		return handleVhosts(os.Args[2])
	}
	if len(os.Args) == 3 && os.Args[1] == "storage" && os.Args[2] == "scan" {
		if err := control.TrustedPath("/usr/bin/find"); err != nil {
			return fmt.Errorf("find 程序权限不安全")
		}
		// Low CPU scheduling priority for a metadata scan of the main filesystem.
		_ = syscall.Setpriority(syscall.PRIO_PROCESS, 0, 19)
		ctx, cancel := context.WithTimeout(context.Background(), storage.ScanTimeout)
		defer cancel()
		encoder := json.NewEncoder(os.Stdout)
		return storage.ScanFiles(ctx, "/", func(progress storage.Progress) { _ = encoder.Encode(progress) })
	}
	if len(os.Args) != 3 || !config.ValidName(os.Args[1]) || !config.ValidAction(os.Args[2]) {
		return fmt.Errorf("服务或操作不受支持")
	}
	if err := control.TrustedPath(serviceConfig); err != nil {
		return fmt.Errorf("辅助程序配置权限不安全")
	}
	services, err := config.LoadServices(serviceConfig)
	if err != nil {
		return fmt.Errorf("无法读取服务配置")
	}
	for _, s := range services {
		if s.Name != os.Args[1] {
			continue
		}
		if s.Name == "redis" && os.Args[2] != "status" {
			return fmt.Errorf("Redis 仅支持只读状态查询")
		}
		if err := control.TrustedPath(s.Program); err != nil {
			return fmt.Errorf("服务管理程序权限不安全")
		}
		if os.Args[2] == "reload" {
			if err := control.TrustedPath(s.CheckProgram); err != nil {
				return fmt.Errorf("Nginx 检查程序权限不安全")
			}
		}
		timeout := 28 * time.Second
		if os.Args[2] == "status" {
			timeout = 6 * time.Second
		}
		if s.Name == "nginx" && os.Args[2] != "status" {
			release, e := control.LockNginx()
			if e != nil {
				return e
			}
			defer release()
		}
		ctx, cancel := context.WithTimeout(context.Background(), timeout)
		defer cancel()
		result, err := control.ExecuteManaged(ctx, s, os.Args[2], control.RunCommand)
		if err != nil {
			if errors.Is(err, control.ErrManagementConflict) {
				return err
			}
			return fmt.Errorf("%s", result.Message)
		}
		return json.NewEncoder(os.Stdout).Encode(result)
	}
	return fmt.Errorf("服务未配置")
}
