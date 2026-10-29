package logs

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"sync"
	"syscall"
)

const MaxCustomFiles = 64

type Registry struct {
	mu    sync.RWMutex
	dir   string
	paths []string
}

func privateState(path string, directory bool) error {
	info, err := os.Lstat(path)
	if err != nil {
		return err
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok || int(stat.Uid) != os.Geteuid() || info.Mode().Perm()&0077 != 0 || info.IsDir() != directory ||
		(!directory && (!info.Mode().IsRegular() || stat.Nlink != 1)) {
		return errors.New("自定义日志状态目录及文件必须由面板账号所有，权限为 0700 / 0600，且不能是链接")
	}
	return nil
}

func OpenRegistry(dir string) (*Registry, error) {
	if !filepath.IsAbs(dir) || filepath.Clean(dir) != dir {
		return nil, errors.New("自定义日志状态目录必须是规范的绝对路径")
	}
	if err := os.MkdirAll(dir, 0700); err != nil {
		return nil, err
	}
	if err := privateState(dir, true); err != nil {
		return nil, err
	}
	r := &Registry{dir: dir, paths: []string{}}
	path := filepath.Join(dir, "custom-logs.json")
	if err := privateState(path, false); err != nil {
		if os.IsNotExist(err) {
			return r, nil
		}
		return nil, err
	}
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	data, err := io.ReadAll(io.LimitReader(f, 256*1024+1))
	if err != nil || len(data) > 256*1024 || json.Unmarshal(data, &r.paths) != nil || r.paths == nil || len(r.paths) > MaxCustomFiles {
		return nil, errors.New("自定义日志列表文件无效，请检查或恢复备份")
	}
	seen := map[string]bool{}
	for _, path := range r.paths {
		if err := ValidatePath(path); err != nil || seen[path] {
			return nil, errors.New("自定义日志列表包含重复或禁止读取的路径")
		}
		seen[path] = true
	}
	return r, nil
}

func (r *Registry) Paths() []string {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return slices.Clone(r.paths)
}

func (r *Registry) Contains(path string) bool {
	if r == nil {
		return false
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	return slices.Contains(r.paths, path)
}

func (r *Registry) Add(path string) error {
	if err := ValidatePath(path); err != nil {
		return err
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if slices.Contains(r.paths, path) {
		return nil
	}
	if len(r.paths) >= MaxCustomFiles {
		return fmt.Errorf("%w：最多添加 %d 个自定义日志", ErrInvalid, MaxCustomFiles)
	}
	return r.save(append(slices.Clone(r.paths), path))
}

func (r *Registry) Remove(path string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	paths := slices.DeleteFunc(slices.Clone(r.paths), func(value string) bool { return value == path })
	if len(paths) == len(r.paths) {
		return nil
	}
	return r.save(paths)
}

func (r *Registry) save(paths []string) error {
	if err := privateState(r.dir, true); err != nil {
		return err
	}
	data, err := json.Marshal(paths)
	if err != nil {
		return err
	}
	f, err := os.CreateTemp(r.dir, ".custom-logs-*")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	if _, err = f.Write(data); err == nil {
		err = f.Sync()
	}
	closeErr := f.Close()
	if err == nil {
		err = closeErr
	}
	if err != nil {
		return err
	}
	if err := os.Rename(f.Name(), filepath.Join(r.dir, "custom-logs.json")); err != nil {
		return err
	}
	r.paths = paths
	return nil
}
