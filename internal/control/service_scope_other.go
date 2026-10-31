//go:build !linux

package control

import "github.com/azpanel/lnmp-gopanel/internal/config"

func serviceActionRunner(_ config.Service, _ string, run CommandRunner) CommandRunner { return run }
