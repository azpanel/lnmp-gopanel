//go:build !linux

package control

func trustedNginxConfigPath(path string) error { return TrustedPath(path) }
