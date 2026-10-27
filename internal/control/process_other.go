//go:build !linux

package control

import (
	"os/exec"
	"time"
)

func configureProcess(cmd *exec.Cmd)          { cmd.WaitDelay = time.Second }
func configureProvisionProcess(cmd *exec.Cmd) { configureProcess(cmd) }
