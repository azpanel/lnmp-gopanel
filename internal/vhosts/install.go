package vhosts

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"time"
)

// Install validates a new configuration before reloading and restores the exact
// previous bytes on failure. Callers hold the shared Nginx lock throughout.
// expected == nil means create-only; existing disabled files also conflict.
func (s Store) Install(ctx context.Context, name string, content, expected []byte, check, reload func(context.Context) error) error {
	if !ValidName(name) || len(content) > MaxConfig {
		return errors.New("站点配置无效")
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	target := filepath.Join(s.Directory, name)
	if expected == nil {
		for _, path := range []string{target, target + ".disabled"} {
			if _, err := os.Lstat(path); !os.IsNotExist(err) {
				return errors.New("同名站点配置已存在，不会覆盖")
			}
		}
	} else {
		path, enabled, err := s.file(name)
		if err != nil || !enabled {
			return errors.New("站点不存在或已停用，请刷新后重试")
		}
		current, err := readBounded(path, MaxConfig)
		if err != nil || !bytes.Equal(current, expected) {
			return errors.New("站点配置已被修改，请检查配置后重试")
		}
	}
	write := func(data []byte, exclusive bool) error {
		f, err := os.CreateTemp(s.Directory, ".gopanel-*.tmp")
		if err != nil {
			return err
		}
		tmp := f.Name()
		defer os.Remove(tmp)
		if err = f.Chmod(0644); err == nil {
			_, err = f.Write(data)
		}
		if err == nil {
			err = f.Sync()
		}
		closeErr := f.Close()
		if err != nil {
			return err
		}
		if closeErr != nil {
			return closeErr
		}
		if exclusive {
			return renameExclusive(tmp, target)
		}
		return os.Rename(tmp, target)
	}
	if err := write(content, expected == nil); err != nil {
		return errors.New("写入站点配置失败")
	}
	rollback := func(reloadOld bool) error {
		current, err := readBounded(target, MaxConfig)
		if err != nil || !bytes.Equal(current, content) {
			return errors.New("配置失败且文件已被外部修改，无法自动回滚，请立即检查 Nginx")
		}
		if expected == nil {
			err = os.Remove(target)
		} else {
			err = write(expected, false)
		}
		if err != nil {
			return errors.New("配置失败且恢复原配置失败，请立即检查 Nginx")
		}
		if reloadOld {
			restore, done := context.WithTimeout(context.Background(), 10*time.Second)
			defer done()
			if check(restore) != nil || reload(restore) != nil {
				return errors.New("已恢复配置，但重新加载原配置失败，请检查 Nginx")
			}
		}
		return nil
	}
	if err := check(ctx); err != nil {
		if e := rollback(false); e != nil {
			return e
		}
		return errors.New("Nginx 配置检查失败，已恢复原配置，未重载")
	}
	if err := reload(ctx); err != nil {
		if e := rollback(true); e != nil {
			return e
		}
		return errors.New("Nginx 重载失败，已恢复并重新加载原配置")
	}
	return nil
}
