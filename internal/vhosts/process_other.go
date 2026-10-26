//go:build !linux

package vhosts

import (
	"errors"
	"os/exec"
)

func configureUsageProcess(cmd *exec.Cmd)   {}
func renameExclusive(old, new string) error { return errors.New("站点启停仅支持 Linux") }
