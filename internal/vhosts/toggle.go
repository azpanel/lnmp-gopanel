package vhosts

import (
	"context"
	"errors"
	"path/filepath"
	"time"
)

type ActionResult struct {
	Name    string `json:"name"`
	Enabled bool   `json:"enabled"`
	Changed bool   `json:"changed"`
	Message string `json:"message"`
}

// A toggle renames the existing inode without rewriting the administrator's
// configuration. No-overwrite renames also protect the rollback destination.
func (s Store) Toggle(ctx context.Context, name string, enabled bool, check, reload func(context.Context) error) (ActionResult, error) {
	result := ActionResult{Name: name, Enabled: enabled}
	if ctx.Err() != nil {
		return result, ctx.Err()
	}
	source, current, e := s.file(name)
	if e != nil {
		return result, e
	}
	if current == enabled {
		result.Message = "站点状态未改变"
		return result, nil
	}
	target := filepath.Join(s.Directory, name)
	if !enabled {
		target += ".disabled"
	}
	if e := renameExclusive(source, target); e != nil {
		return result, errors.New("重命名失败，请检查文件权限及同名配置")
	}
	rollback := func(reloadOld bool) error {
		if e := renameExclusive(target, source); e != nil {
			return errors.New("操作失败且无法恢复文件名，请立即检查站点配置")
		}
		if reloadOld {
			restore, cancel := context.WithTimeout(context.Background(), 8*time.Second)
			defer cancel()
			if check(restore) != nil || reload(restore) != nil {
				return errors.New("已恢复原文件名，但恢复重载失败，请检查 Nginx 状态")
			}
		}
		return nil
	}
	if e := check(ctx); e != nil {
		if err := rollback(false); err != nil {
			return result, err
		}
		return result, errors.New("Nginx 配置检查失败，已恢复原文件名，未执行重载")
	}
	if e := reload(ctx); e != nil {
		if err := rollback(true); err != nil {
			return result, err
		}
		return result, errors.New("Nginx 重载失败，已恢复原文件名并重新加载原配置")
	}
	result.Changed = true
	result.Message = "站点已启用，Nginx 已重载"
	if !enabled {
		result.Message = "站点已停用，Nginx 已重载"
	}
	return result, nil
}
