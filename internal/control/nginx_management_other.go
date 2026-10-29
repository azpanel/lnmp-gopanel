//go:build !linux

package control

import (
	"context"
	"errors"

	"github.com/azpanel/lnmp-gopanel/internal/config"
)

func NginxManagementStatus(context.Context, config.Service) (Result, bool) { return Result{}, false }
func RootNginxManagementPreview(context.Context) (ManagementRepairPlan, error) {
	return ManagementRepairPlan{}, errors.New("管理方式修复仅支持 Linux")
}
func RootNginxManagementRepair(context.Context, ManagementRepairRequest) (Result, error) {
	return Result{}, errors.New("管理方式修复仅支持 Linux")
}
