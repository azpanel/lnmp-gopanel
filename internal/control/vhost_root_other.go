//go:build !linux

package control

import "errors"

func prepareSiteRoot(string) (siteRootPreparation, error) {
	return nil, errors.New("建站仅支持 Linux")
}
