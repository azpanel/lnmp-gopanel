package control

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"os/exec"
	"strings"
	"time"

	"github.com/azpanel/lnmp-gopanel/internal/vhosts"
)

func (h Helper) vhostCommand(ctx context.Context, action string, input any, consume func(*json.Decoder) error) error {
	cmd := exec.CommandContext(ctx, "/usr/bin/sudo", "-n", h.Path, "vhosts", action)
	configureProcess(cmd)
	if action == "create" || action == "ssl-renew" {
		configureProvisionProcess(cmd)
	}
	cmd.Env = []string{"PATH=/usr/bin:/bin", "LANG=C", "LC_ALL=C"}
	cmd.Stderr = io.Discard
	if input != nil {
		data, e := json.Marshal(input)
		if e != nil {
			return e
		}
		cmd.Stdin = strings.NewReader(string(data))
	}
	pipe, e := cmd.StdoutPipe()
	if e != nil {
		return e
	}
	if e := cmd.Start(); e != nil {
		return errors.New("站点辅助程序不可用")
	}
	limit := int64(8 * 1024 * 1024)
	if action == "create" || action == "ssl-renew" {
		limit = 64 * 1024 * 1024
	}
	decoder := json.NewDecoder(io.LimitReader(pipe, limit))
	decodeErr := consume(decoder)
	if decodeErr != nil {
		_ = cmd.Process.Kill()
	}
	waitErr := cmd.Wait()
	if ctx.Err() != nil {
		return errors.New("站点操作超时或取消")
	}
	if decodeErr != nil {
		return errors.New("站点辅助程序返回无效结果，请检查安装与 sudoers 配置")
	}
	if waitErr != nil {
		return errors.New("站点辅助程序执行失败，请检查权限与 Nginx 配置")
	}
	return nil
}

type vhostEnvelope[T any] struct {
	Data    T      `json:"data"`
	Message string `json:"message,omitempty"`
}

func readVhostResponse[T any](decoder *json.Decoder, out *T) error {
	var envelope vhostEnvelope[T]
	if e := decoder.Decode(&envelope); e != nil {
		return e
	}
	if envelope.Message != "" {
		return errors.New(envelope.Message)
	}
	*out = envelope.Data
	return nil
}
func (h Helper) ListVhosts(parent context.Context) ([]vhosts.Site, error) {
	ctx, cancel := context.WithTimeout(parent, 12*time.Second)
	defer cancel()
	sites := []vhosts.Site{}
	var helperErr error
	e := h.vhostCommand(ctx, "list", nil, func(d *json.Decoder) error { helperErr = readVhostResponse(d, &sites); return nil })
	if helperErr != nil {
		return nil, helperErr
	}
	return sites, e
}
func (h Helper) ReadVhost(ctx context.Context, name string) (vhosts.ConfigView, error) {
	var view vhosts.ConfigView
	if !vhosts.ValidName(name) {
		return view, errors.New("配置文件名无效")
	}
	var helperErr error
	e := h.vhostCommand(ctx, "config", map[string]any{"name": name}, func(d *json.Decoder) error { helperErr = readVhostResponse(d, &view); return nil })
	if helperErr != nil {
		return view, helperErr
	}
	return view, e
}
func (h Helper) ToggleVhost(ctx context.Context, name string, enabled bool) (vhosts.ActionResult, error) {
	var result vhosts.ActionResult
	if !vhosts.ValidName(name) {
		return result, errors.New("配置文件名无效")
	}
	var helperErr error
	e := h.vhostCommand(ctx, "toggle", map[string]any{"name": name, "enabled": enabled}, func(d *json.Decoder) error { helperErr = readVhostResponse(d, &result); return nil })
	if helperErr != nil {
		return result, helperErr
	}
	return result, e
}
func (h Helper) VhostUsage(ctx context.Context, report func(vhosts.Usage)) error {
	return h.vhostCommand(ctx, "usage", nil, func(d *json.Decoder) error {
		for {
			var envelope vhostEnvelope[*vhosts.Usage]
			e := d.Decode(&envelope)
			if e == io.EOF {
				return nil
			}
			if e != nil {
				return e
			}
			if envelope.Message != "" {
				return errors.New(envelope.Message)
			}
			if envelope.Data != nil {
				report(*envelope.Data)
			}
		}
	})
}

func (h Helper) VhostOptions(ctx context.Context) (vhosts.CreateOptions, error) {
	var out vhosts.CreateOptions
	var helperErr error
	err := h.vhostCommand(ctx, "options", nil, func(d *json.Decoder) error { helperErr = readVhostResponse(d, &out); return nil })
	if helperErr != nil {
		return out, helperErr
	}
	return out, err
}
func (h Helper) CreateVhost(ctx context.Context, r vhosts.CreateRequest, report func(vhosts.CreateEvent)) (vhosts.CreateResult, error) {
	return h.provisionVhost(ctx, "create", r, report)
}
func (h Helper) provisionVhost(ctx context.Context, action string, input any, report func(vhosts.CreateEvent)) (vhosts.CreateResult, error) {
	result := vhosts.CreateResult{Database: "skipped", SSL: "skipped"}
	if r, ok := input.(vhosts.CreateRequest); ok {
		result.Name = r.Name
	}
	var helperErr error
	complete := false
	err := h.vhostCommand(ctx, action, input, func(d *json.Decoder) error {
		for {
			var envelope struct {
				Data     *vhosts.CreateEvent `json:"data"`
				Message  string              `json:"message"`
				Complete bool                `json:"complete"`
			}
			e := d.Decode(&envelope)
			if e == io.EOF {
				return nil
			}
			if e != nil {
				return e
			}
			if envelope.Message != "" {
				helperErr = errors.New(envelope.Message)
			}
			if envelope.Data != nil {
				if envelope.Data.Result != nil {
					result = *envelope.Data.Result
				}
				report(*envelope.Data)
			}
			complete = complete || envelope.Complete
		}
	})
	if helperErr != nil {
		return result, helperErr
	}
	if err != nil {
		return result, err
	}
	if !complete {
		return result, errors.New("建站辅助程序中断，请刷新站点确认实际状态")
	}
	return result, nil
}

func (h Helper) PreviewRenewal(ctx context.Context, names []string) ([]vhosts.RenewPreview, error) {
	var out []vhosts.RenewPreview
	var helperErr error
	err := h.vhostCommand(ctx, "ssl-renew-preview", map[string]any{"names": names}, func(d *json.Decoder) error { helperErr = readVhostResponse(d, &out); return nil })
	if helperErr != nil {
		return nil, helperErr
	}
	return out, err
}
func (h Helper) RenewVhost(ctx context.Context, id string, item vhosts.RenewItem, report func(vhosts.CreateEvent)) error {
	_, err := h.provisionVhost(ctx, "ssl-renew", vhosts.RenewRequest{RequestID: id, Items: []vhosts.RenewItem{item}}, report)
	return err
}
