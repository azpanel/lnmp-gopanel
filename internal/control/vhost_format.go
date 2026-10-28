package control

import (
	"context"
	"encoding/json"
	"errors"
	"path/filepath"
	"time"

	"github.com/azpanel/lnmp-gopanel/internal/vhosts"
)

func (h Helper) FormatVhost(ctx context.Context, r vhosts.FormatInput) (vhosts.FormatResult, error) {
	var result vhosts.FormatResult
	if err := r.Validate(); err != nil {
		return result, err
	}
	var helperErr error
	err := h.vhostCommand(ctx, "format", r, func(d *json.Decoder) error {
		helperErr = readVhostResponse(d, &result)
		return nil
	})
	if helperErr != nil {
		return result, helperErr
	}
	return result, err
}

func RootFormatVhost(ctx context.Context, r vhosts.FormatInput, binary string) (vhosts.FormatResult, error) {
	if err := TrustedPath(binary); err != nil {
		return vhosts.FormatResult{}, errors.New("Nginx 程序权限不安全")
	}
	p := siteProvisioner{store: vhosts.DefaultStore(), stateDir: managedSites, trust: TrustedPath, lock: LockNginx, run: RunCommand}
	return p.format(ctx, r, binary)
}

func (p siteProvisioner) format(ctx context.Context, r vhosts.FormatInput, binary string) (vhosts.FormatResult, error) {
	var result vhosts.FormatResult
	if err := r.Validate(); err != nil {
		return result, err
	}
	if err := p.trust(p.store.Directory); err != nil {
		return result, errors.New("vhost 目录权限不安全")
	}
	unlock, err := p.lock()
	if err != nil {
		return result, err
	}
	defer unlock()
	view, err := p.store.Read(r.Name)
	if err != nil {
		return result, err
	}
	if err := p.trust(filepath.Join(p.store.Directory, view.Filename)); err != nil {
		return result, errors.New("站点配置权限不安全")
	}
	if view.Revision != r.Revision {
		return result, errors.New("配置已被修改，请重新读取后再格式化")
	}
	original, credential, err := p.store.StripBasicAuth(r.Name, view.Content)
	if err != nil {
		return result, err
	}
	base, err := vhosts.FormatNginx(original)
	if err != nil {
		return result, err
	}
	content := base
	if credential != "" {
		content, err = p.store.AttachBasicAuth(r.Name, base, credential)
		if err != nil {
			return result, err
		}
	}
	result.ConfigView = view
	result.Message = "配置缩进已规范，无需修改"
	if content == view.Content {
		return result, nil
	}
	check := func(parent context.Context) error {
		task, done := context.WithTimeout(parent, 8*time.Second)
		defer done()
		_, err := p.run(task, binary, "-t")
		return err
	}
	if err := p.store.SaveFormatted(ctx, r.Name, []byte(content), []byte(view.Content), check); err != nil {
		return result, err
	}
	// Preserve ownership of panel-created sites so a later SSL retry still works.
	if state, err := p.loadManaged(r.Name); err == nil && state.Hash == configHash([]byte(original)) {
		state.Hash = configHash([]byte(base))
		if err := p.saveManaged(state); err != nil {
			restore, done := context.WithTimeout(context.Background(), 15*time.Second)
			defer done()
			if rollback := p.store.SaveFormatted(restore, r.Name, []byte(view.Content), []byte(content), check); rollback != nil {
				return result, errors.New("站点记录更新失败且恢复配置失败，请检查 Nginx 配置")
			}
			return result, errors.New("站点记录更新失败，已恢复原配置")
		}
	}
	result.Content = content
	result.Revision = vhosts.ConfigRevision(result.ConfigView)
	result.Changed = true
	result.Message = "配置缩进已格式化并保存"
	return result, nil
}
