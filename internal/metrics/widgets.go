package metrics

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"slices"
	"sync"
	"syscall"
)

var WidgetIDs = []string{"disk_io", "load", "swap", "memory_breakdown", "processes", "nginx", "php_fpm", "database"}
var ErrWidgetsInput = errors.New("状态指标选择无效")

type Widgets struct {
	Enabled []string `json:"enabled"`
}

func ValidateWidgets(w Widgets) error {
	if w.Enabled == nil || len(w.Enabled) > len(WidgetIDs) {
		return ErrWidgetsInput
	}
	seen := map[string]bool{}
	for _, id := range w.Enabled {
		if !slices.Contains(WidgetIDs, id) || seen[id] {
			return ErrWidgetsInput
		}
		seen[id] = true
	}
	return nil
}
func orderedWidgets(w Widgets) Widgets {
	out := Widgets{Enabled: []string{}}
	for _, id := range WidgetIDs {
		if slices.Contains(w.Enabled, id) {
			out.Enabled = append(out.Enabled, id)
		}
	}
	return out
}

type WidgetStore struct {
	mu      sync.RWMutex
	dir     string
	current Widgets
}

func privateWidgetPath(path string, directory bool) error {
	info, err := os.Lstat(path)
	if err != nil {
		return err
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok || int(stat.Uid) != os.Geteuid() || info.Mode().Perm()&0077 != 0 || info.IsDir() != directory || (!directory && (!info.Mode().IsRegular() || stat.Nlink != 1)) {
		return errors.New("状态指标目录和文件须由面板账号所有，权限为 0700 / 0600，且不能是链接")
	}
	return nil
}
func OpenWidgetStore(dir string) (*WidgetStore, error) {
	if !filepath.IsAbs(dir) || filepath.Clean(dir) != dir {
		return nil, errors.New("状态指标目录必须是规范绝对路径")
	}
	if err := os.MkdirAll(dir, 0700); err != nil {
		return nil, err
	}
	if err := privateWidgetPath(dir, true); err != nil {
		return nil, err
	}
	store := &WidgetStore{dir: dir, current: Widgets{Enabled: []string{}}}
	path := filepath.Join(dir, "dashboard-widgets.json")
	if err := privateWidgetPath(path, false); err != nil {
		if os.IsNotExist(err) {
			return store, nil
		}
		return nil, err
	}
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	data, err := io.ReadAll(io.LimitReader(f, 4097))
	if err != nil || len(data) > 4096 {
		return nil, errors.New("状态指标文件过大或无法读取")
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	var decoded Widgets
	if err = decoder.Decode(&decoded); err != nil {
		return nil, errors.New("状态指标文件损坏，请检查或恢复备份")
	}
	var extra any
	if decoder.Decode(&extra) != io.EOF || ValidateWidgets(decoded) != nil {
		return nil, errors.New("状态指标文件内容无效")
	}
	store.current = orderedWidgets(decoded)
	return store, nil
}
func (s *WidgetStore) Get() Widgets {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return Widgets{Enabled: slices.Clone(s.current.Enabled)}
}
func (s *WidgetStore) Save(w Widgets, apply func([]string)) error {
	if err := ValidateWidgets(w); err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := privateWidgetPath(s.dir, true); err != nil {
		return err
	}
	path := filepath.Join(s.dir, "dashboard-widgets.json")
	if err := privateWidgetPath(path, false); err != nil && !os.IsNotExist(err) {
		return err
	}
	w = orderedWidgets(w)
	data, err := json.Marshal(w)
	if err != nil {
		return err
	}
	f, err := os.CreateTemp(s.dir, ".dashboard-widgets-*")
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
	if err == nil {
		err = os.Rename(f.Name(), path)
	}
	if err != nil {
		return err
	}
	s.current = w
	if apply != nil {
		apply(slices.Clone(w.Enabled))
	}
	return nil
}
