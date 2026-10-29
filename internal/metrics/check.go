package metrics

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/azpanel/lnmp-gopanel/internal/config"
)

// CheckServices exercises exactly the probes used by the live collector. It
// neither starts the collector nor opens account state or changes a service.
func CheckServices(ctx context.Context, options config.MetricsConfig, ids []string) error {
	enabled := map[string]bool{}
	if len(ids) == 0 {
		return errors.New("请至少选择一个服务指标")
	}
	for _, id := range ids {
		if id != "nginx" && id != "php_fpm" && id != "database" {
			return errors.New("服务指标名称无效")
		}
		if enabled[id] {
			return errors.New("服务指标名称重复")
		}
		enabled[id] = true
	}
	c := New()
	c.Configure(options)
	result := c.sampleExtra(ctx, enabled, 0)
	failures := []string{}
	for _, id := range []string{"nginx", "php_fpm", "database"} {
		if message := result.errors[id]; message != "" {
			failures = append(failures, fmt.Sprintf("%s：%s", id, message))
		}
	}
	if len(failures) > 0 {
		return errors.New(strings.Join(failures, "；"))
	}
	return nil
}
