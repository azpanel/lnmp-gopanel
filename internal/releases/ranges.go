package releases

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"syscall"
)

var ErrRangeInput = errors.New("版本范围无效：仅支持 Nginx、PHP、MariaDB，范围请留空或填写大版本／大版本.中版本")
var ErrRangeStorage = errors.New("版本范围配置不可用，请检查状态目录及文件后重试；读取失败时请重启面板")
var rangePattern = regexp.MustCompile(`^(0|[1-9][0-9]{0,8})(\.(0|[1-9][0-9]{0,8}))?$`)

func RangeService(name string) bool {
	return name == "nginx" || name == "php-fpm" || name == "mariadb"
}

func ValidRange(scope string) bool { return scope == "" || rangePattern.MatchString(scope) }

func matchesRange(version, scope string) bool {
	return scope == "" || version == scope || strings.HasPrefix(version, scope+".")
}

type rangeStore struct {
	dir    string
	values map[string]string
}

func privateRangePath(path string, directory bool) error {
	info, err := os.Lstat(path)
	if err != nil {
		return err
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok || int(stat.Uid) != os.Geteuid() || info.Mode().Perm()&0077 != 0 || info.IsDir() != directory || (!directory && (!info.Mode().IsRegular() || stat.Nlink != 1)) {
		return ErrRangeStorage
	}
	return nil
}

func openRangeStore(dir string) (*rangeStore, error) {
	if !filepath.IsAbs(dir) || filepath.Clean(dir) != dir {
		return nil, ErrRangeStorage
	}
	if err := os.MkdirAll(dir, 0700); err != nil {
		return nil, err
	}
	if err := privateRangePath(dir, true); err != nil {
		return nil, err
	}
	store := &rangeStore{dir: dir, values: map[string]string{}}
	path := filepath.Join(dir, "service-release-ranges.json")
	if err := privateRangePath(path, false); err != nil {
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
		return nil, ErrRangeStorage
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	var values map[string]*string
	if decoder.Decode(&values) != nil || values == nil {
		return nil, ErrRangeStorage
	}
	var extra any
	if decoder.Decode(&extra) != io.EOF {
		return nil, ErrRangeStorage
	}
	for name, scope := range values {
		if !RangeService(name) || scope == nil || !ValidRange(*scope) {
			return nil, ErrRangeStorage
		}
		store.values[name] = *scope
	}
	return store, nil
}

// save is called under the manager lock. Memory is updated only after atomic replacement.
func (s *rangeStore) save(name, scope string) error {
	if err := privateRangePath(s.dir, true); err != nil {
		return err
	}
	path := filepath.Join(s.dir, "service-release-ranges.json")
	if err := privateRangePath(path, false); err != nil && !os.IsNotExist(err) {
		return err
	}
	values := make(map[string]string, len(s.values)+1)
	for k, v := range s.values {
		values[k] = v
	}
	values[name] = scope
	data, err := json.Marshal(values)
	if err != nil {
		return err
	}
	f, err := os.CreateTemp(s.dir, ".service-release-ranges-*")
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
	s.values = values
	return nil
}

// ConfigureRanges is called once at startup. A load failure blocks scoped services
// rather than silently checking unrestricted versions with lost settings.
func (m *Manager) ConfigureRanges(dir string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	store, err := openRangeStore(dir)
	m.ranges, m.rangeError = store, err
	return err
}

func (m *Manager) SaveRange(name, scope string) (Release, error) {
	if !RangeService(name) || !ValidRange(scope) {
		return Release{}, ErrRangeInput
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.ranges == nil || m.rangeError != nil {
		return Release{}, ErrRangeStorage
	}
	if err := m.ranges.save(name, scope); err != nil {
		return Release{}, ErrRangeStorage
	}
	return m.latestLocked([]string{name}, true)[name], nil
}
