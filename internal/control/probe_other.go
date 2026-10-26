//go:build !linux

package control

import (
	"context"
	"github.com/azpanel/lnmp-gopanel/internal/config"
)

func NativeStatus(ctx context.Context, s config.Service) (Result, bool) { return Result{}, false }
