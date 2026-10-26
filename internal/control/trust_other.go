//go:build !linux

package control

import "errors"

func TrustedPath(path string) error { return errors.New("privileged service control requires Linux") }
