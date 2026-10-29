package metricssetup

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/azpanel/lnmp-gopanel/internal/config"
	"github.com/azpanel/lnmp-gopanel/internal/control"
)

type fileSnapshot struct {
	Path   string      `json:"path"`
	Exists bool        `json:"exists"`
	Data   []byte      `json:"-"`
	Mode   os.FileMode `json:"mode"`
	GID    int         `json:"gid"`
}
type change struct {
	before fileSnapshot
	after  []byte
	mode   os.FileMode
	gid    int
}
type managedState struct {
	Format    string `json:"format"`
	Port      int    `json:"port"`
	Nginx     bool   `json:"nginx"`
	PHPPath   string `json:"php_path,omitempty"`
	PHPListen string `json:"php_listen,omitempty"`
	Hash      string `json:"hash"`
}
type pool struct {
	Name, Path, Listen, Status string
	data                       []byte
	insert                     int
	sources                    []fileSnapshot
	statusSet                  bool
	statusStart, statusEnd     int
}

type environment struct {
	fileGroup  int
	trust      func(string) error
	write      func(string, []byte, os.FileMode, int) error
	root       string
	run        func(context.Context, string, ...string) error
	active     func(context.Context, string) bool
	probe      func(context.Context, config.MetricsConfig, []string) error
	admin      func(context.Context, config.MetricsMySQLConfig) (accountAdmin, error)
	userCheck  func(context.Context, string, []string) error
	panelCheck func(context.Context, string) error
	portFree   func(int) bool
	lock       func() (func(), error)
}

func systemEnvironment() *environment {
	e := &environment{root: "/", admin: connectAdmin, trust: control.TrustedPath, write: atomicWrite}
	e.run = func(ctx context.Context, program string, args ...string) error {
		if err := control.TrustedPath(program); err != nil {
			return fmt.Errorf("程序路径不可信：%s", program)
		}
		command := exec.CommandContext(ctx, program, args...)
		command.Env = []string{"PATH=/usr/sbin:/usr/bin:/sbin:/bin", "LANG=C", "HOME=/nonexistent"}
		// Configuration checks and service commands never receive credentials. Do
		// not forward raw output that might contain administrator configuration.
		command.Stdout, command.Stderr = io.Discard, io.Discard
		return command.Run()
	}
	e.active = func(ctx context.Context, name string) bool {
		if name == "panel" {
			return e.run(ctx, "/usr/bin/systemctl", "is-active", "--quiet", "lnmp-gopanel") == nil
		}
		result, handled := control.NativeStatus(ctx, config.Service{Name: name, Manager: "init"})
		return handled && result.State == "running"
	}
	e.portFree = func(port int) bool {
		listener, err := net.Listen("tcp4", net.JoinHostPort("127.0.0.1", strconv.Itoa(port)))
		if err != nil {
			return false
		}
		listener.Close()
		return true
	}
	e.lock = control.LockNginx
	e.userCheck = func(ctx context.Context, path string, ids []string) error {
		return e.run(ctx, "/usr/bin/sudo", "-n", "-u", "lnmp-panel", "--", "/usr/local/bin/lnmp-gopanel", "metrics", "check", "--config", path, "--only", strings.Join(ids, ","))
	}
	e.panelCheck = func(ctx context.Context, path string) error {
		cfg, err := config.Load(path)
		if err != nil {
			return errors.New("无法读取面板监听地址")
		}
		address, port, err := net.SplitHostPort(cfg.Listen)
		if err != nil {
			return errors.New("面板监听地址无效")
		}
		if address == "" || address == "0.0.0.0" {
			address = "127.0.0.1"
		}
		if address == "::" {
			address = "::1"
		}
		// Probe only this host's configured panel listener. Its TLS certificate can
		// be self-signed or cover a domain instead of the local bind address.
		transport := &http.Transport{Proxy: nil, TLSClientConfig: &tls.Config{InsecureSkipVerify: true}, DisableKeepAlives: true}
		defer transport.CloseIdleConnections()
		client := &http.Client{Timeout: 2 * time.Second, Transport: transport, CheckRedirect: func(*http.Request, []*http.Request) error { return errors.New("面板健康检查不跟随跳转") }}
		task, cancel := context.WithTimeout(ctx, 8*time.Second)
		defer cancel()
		endpoint := "https://" + net.JoinHostPort(address, port) + "/api/v1/auth/session"
		for {
			request, err := http.NewRequestWithContext(task, http.MethodGet, endpoint, nil)
			if err != nil {
				return errors.New("面板健康检查地址无效")
			}
			response, probeErr := client.Do(request)
			if probeErr == nil {
				response.Body.Close()
				if response.StatusCode == http.StatusUnauthorized && e.active(task, "panel") {
					return nil
				}
			}
			select {
			case <-task.Done():
				return errors.New("面板 HTTPS 健康检查失败")
			case <-time.After(200 * time.Millisecond):
			}
		}
	}
	return e
}
func (e *environment) path(path string) string {
	if e.root == "/" {
		return path
	}
	return filepath.Join(e.root, path)
}
func (e *environment) read(path string, optional bool) (fileSnapshot, error) {
	full := e.path(path)
	s := fileSnapshot{Path: path}
	if err := e.trust(filepath.Dir(full)); err != nil {
		return s, fmt.Errorf("配置目录不可信：%s", filepath.Dir(path))
	}
	info, err := os.Lstat(full)
	if os.IsNotExist(err) && optional {
		return s, nil
	}
	if err != nil {
		return s, fmt.Errorf("无法读取配置：%s", path)
	}
	if !info.Mode().IsRegular() || info.Size() > 1024*1024 || e.trust(full) != nil {
		return s, fmt.Errorf("配置必须是可信的普通文件且不超过 1 MiB：%s", path)
	}
	data, err := os.ReadFile(full)
	if err != nil {
		return s, fmt.Errorf("无法读取配置：%s", path)
	}
	s.Exists, s.Data, s.Mode, s.GID = true, data, info.Mode().Perm(), int(info.Sys().(*syscall.Stat_t).Gid)
	return s, nil
}
func atomicWrite(path string, data []byte, mode os.FileMode, gid int) error {
	return atomicWriteOwned(path, data, mode, 0, gid)
}
func atomicWriteOwned(path string, data []byte, mode os.FileMode, uid, gid int) error {
	f, err := os.CreateTemp(filepath.Dir(path), ".metrics-setup-*")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	defer f.Close()
	if err = f.Chown(uid, gid); err != nil {
		return err
	}
	if err = f.Chmod(mode); err != nil {
		return err
	}
	if _, err = f.Write(data); err != nil {
		return err
	}
	if err = f.Sync(); err != nil {
		return err
	}
	if err = f.Close(); err != nil {
		return err
	}
	if err = os.Rename(f.Name(), path); err != nil {
		return err
	}
	directory, err := os.Open(filepath.Dir(path))
	if err != nil {
		return err
	}
	defer directory.Close()
	return directory.Sync()
}
func (e *environment) unchanged(s fileSnapshot) bool {
	actual, err := e.read(s.Path, !s.Exists)
	return err == nil && actual.Exists == s.Exists && bytes.Equal(actual.Data, s.Data) && actual.Mode == s.Mode && actual.GID == s.GID
}
func (e *environment) readState(configPath string) (managedState, fileSnapshot, error) {
	path := filepath.Join(filepath.Dir(configPath), "metrics-setup.json")
	s, err := e.read(path, true)
	state := managedState{Format: "lnmp-gopanel-metrics-v1", Port: 18080}
	if err != nil {
		return state, s, err
	}
	if s.Exists {
		decoder := json.NewDecoder(bytes.NewReader(s.Data))
		decoder.DisallowUnknownFields()
		var decoded managedState
		if decoder.Decode(&decoded) != nil {
			return state, s, errors.New("监控向导记录无效，请检查 metrics-setup.json")
		}
		state = decoded
		checksum, hashErr := hex.DecodeString(state.Hash)
		if hashErr != nil || len(checksum) != 32 || state.Format != "lnmp-gopanel-metrics-v1" || state.Port < 1024 || state.Port > 65535 {
			return state, s, errors.New("监控向导记录无效，请检查 metrics-setup.json")
		}
		var extra any
		if decoder.Decode(&extra) != io.EOF {
			return state, s, errors.New("监控向导记录包含多余内容")
		}
	}
	return state, s, nil
}

func fpmValue(value string) string {
	value = strings.TrimSpace(value)
	if len(value) >= 2 && (value[0] == '\'' || value[0] == '"') {
		quote := value[0]
		end := strings.IndexByte(value[1:], quote)
		if end >= 0 {
			return value[1 : end+1]
		}
		return value
	}
	if end := strings.IndexByte(value, ';'); end >= 0 {
		value = value[:end]
	}
	return strings.TrimSpace(value)
}
func (e *environment) pools() (out []pool, err error) {
	seen := map[string]bool{}
	names := map[string]bool{}
	sources := []fileSnapshot{}
	var visit func(string, int) error
	visit = func(path string, depth int) error {
		if depth > 8 || seen[path] {
			return errors.New("PHP-FPM include 循环或嵌套过深")
		}
		seen[path] = true
		snapshot, err := e.read(path, false)
		if err != nil {
			return err
		}
		sources = append(sources, snapshot)
		var current *pool
		offset := 0
		for _, line := range strings.SplitAfter(string(snapshot.Data), "\n") {
			trimmed := strings.TrimSpace(line)
			if strings.HasPrefix(trimmed, "[") {
				section, _, found := strings.Cut(trimmed[1:], "]")
				if !found {
					return errors.New("PHP-FPM 段名无效")
				}
				current = nil
				if section != "global" {
					if names[section] {
						return errors.New("PHP-FPM 池分散在多个同名配置段，请使用已有状态 URL")
					}
					names[section] = true
					out = append(out, pool{Name: section, Path: path, data: snapshot.Data, insert: offset + len(line)})
					current = &out[len(out)-1]
				}
			} else if trimmed != "" && !strings.HasPrefix(trimmed, ";") && !strings.HasPrefix(trimmed, "#") {
				key, value, ok := strings.Cut(trimmed, "=")
				key = strings.TrimSpace(key)
				value = fpmValue(value)
				if ok && key == "include" {
					if current != nil {
						return errors.New("PHP-FPM 池内 include 无法自动处理，请使用已有状态 URL")
					}
					if !filepath.IsAbs(value) {
						return errors.New("PHP-FPM 相对 include 无法自动处理，请使用已有状态 URL")
					}
					matches, err := filepath.Glob(e.path(value))
					if err != nil {
						return errors.New("PHP-FPM include 无效")
					}
					sort.Strings(matches)
					for _, match := range matches {
						relative, err := filepath.Rel(e.root, match)
						if err != nil {
							return err
						}
						if err = visit("/"+relative, depth+1); err != nil {
							return err
						}
					}
					// Recursive traversal can reallocate out; avoid keeping stale pointers.
					current = nil
				} else if ok && current != nil {
					if key == "listen" {
						current.Listen = value
					}
					if key == "pm.status_path" {
						if current.statusSet {
							return errors.New("PHP-FPM 状态路径重复")
						}
						current.Status = value
						current.statusSet = true
						current.statusStart, current.statusEnd = offset, offset+len(line)
					}
				}
			}
			offset += len(line)
		}
		return nil
	}
	err = visit(fpmConfig, 0)
	for i := range out {
		out[i].sources = sources
	}
	return
}
func addFPMStatus(p pool, path string) []byte {
	if p.Status != "" {
		return bytes.Clone(p.data)
	}
	newline := "\n"
	if bytes.Contains(p.data, []byte("\r\n")) {
		newline = "\r\n"
	}
	if p.statusSet {
		insertion := "pm.status_path = " + path + newline
		return append(append(bytes.Clone(p.data[:p.statusStart]), []byte(insertion)...), p.data[p.statusEnd:]...)
	}
	prefix := ""
	if p.insert > 0 && p.data[p.insert-1] != '\n' {
		prefix = newline
	}
	insertion := prefix + "; LNMP GoPanel status endpoint" + newline + "pm.status_path = " + path + newline
	return append(append(bytes.Clone(p.data[:p.insert]), []byte(insertion)...), p.data[p.insert:]...)
}
func (e *environment) command(ctx context.Context, name string, args ...string) error {
	task, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	return e.run(task, e.path(name), args...)
}
