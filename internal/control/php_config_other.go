//go:build !linux

package control

import "context"

func LaunchStandardPHPFPM(string) error { return ErrUnsupported }

func RootPHPConfig(context.Context) (PHPConfigSnapshot, error) {
	return PHPConfigSnapshot{}, ErrUnsupported
}
func RootUpdatePHPConfig(context.Context, PHPConfigRequest) (PHPConfigResponse, error) {
	return PHPConfigResponse{}, ErrUnsupported
}
