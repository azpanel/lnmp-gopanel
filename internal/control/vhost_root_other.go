//go:build !linux

package control

import "errors"

func prepareSiteRoot(string) error { return errors.New("建站仅支持 Linux") }
