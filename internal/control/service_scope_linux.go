//go:build linux

package control

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"os"

	"github.com/azpanel/lnmp-gopanel/internal/config"
)

const serviceScopeProgram = "/usr/bin/systemd-run"
const serviceScopePrefix = "lnmp-gopanel-managed-"

type serviceScope struct {
	enabled func() (bool, error)
	trust   func(string) error
}

func serviceActionRunner(s config.Service, action string, run CommandRunner) CommandRunner {
	return (serviceScope{enabled: serviceScopeEnabled, trust: TrustedPath}).runner(s, action, run)
}

func serviceScopeEnabled() (bool, error) {
	_, err := os.Stat("/run/systemd/system")
	if errors.Is(err, os.ErrNotExist) {
		return false, nil
	}
	return err == nil, err
}

// setsid and double-fork do not leave the panel's cgroup. A separate scope
// keeps init/LNMP daemons alive when the panel is stopped or upgraded, while
// systemd-run still returns the script's exit status synchronously.
func (p serviceScope) runner(s config.Service, action string, run CommandRunner) CommandRunner {
	if (s.Manager != "init" && s.Manager != "lnmp") || action == "status" {
		return run
	}
	return func(ctx context.Context, program string, args ...string) (string, error) {
		// Configuration checks and other probes remain ordinary short commands.
		if program != s.Program {
			return run(ctx, program, args...)
		}
		enabled, err := p.enabled()
		if err != nil {
			return "", fmt.Errorf("无法检查 systemd 服务隔离环境：%w", err)
		}
		if !enabled {
			return run(ctx, program, args...)
		}
		if err := p.trust(serviceScopeProgram); err != nil {
			return "", fmt.Errorf("服务隔离程序不可用或权限不安全：%w", err)
		}
		var id [12]byte
		if _, err := rand.Read(id[:]); err != nil {
			return "", err
		}
		unit := serviceScopePrefix + s.Name + "-" + hex.EncodeToString(id[:]) + ".scope"
		scoped := []string{"--quiet", "--scope", "--collect", "--unit=" + unit, "--", program}
		scoped = append(scoped, args...)
		// Do not retry without isolation if systemd rejects the scope: that would
		// reintroduce the panel restart outage, or execute a command twice.
		return run(ctx, serviceScopeProgram, scoped...)
	}
}
