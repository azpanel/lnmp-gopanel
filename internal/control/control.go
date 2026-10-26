package control

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"os/exec"
	"strings"
	"sync"
	"time"

	"github.com/azpanel/lnmp-gopanel/internal/config"
)

var ErrBusy = errors.New("service operation already in progress")
var ErrUnsupported = errors.New("unsupported service or action")

type Result struct {
	State   string `json:"state"`
	Message string `json:"message"`
}
type View struct {
	Name             string    `json:"name"`
	State            string    `json:"state"`
	Manager          string    `json:"manager"`
	Actions          []string  `json:"actions"`
	Message          string    `json:"message"`
	CheckedAt        time.Time `json:"checked_at"`
	Busy             bool      `json:"busy"`
	Version          string    `json:"version"`
	VersionError     string    `json:"version_error,omitempty"`
	VersionCheckedAt time.Time `json:"version_checked_at"`
}
type Executor interface {
	Run(context.Context, string, string) (Result, error)
}
type Helper struct{ Path string }

func (h Helper) Run(ctx context.Context, name, action string) (Result, error) {
	if !config.ValidName(name) || !config.ValidAction(action) {
		return Result{}, ErrUnsupported
	}
	out, err := RunCommand(ctx, "/usr/bin/sudo", "-n", h.Path, name, action)
	var r Result
	if json.Unmarshal([]byte(out), &r) != nil {
		return Result{}, fmt.Errorf("服务辅助程序不可用，请检查安装与 sudoers 配置")
	}
	if err != nil {
		return r, errors.New(r.Message)
	}
	return r, nil
}

type Manager struct {
	mu       sync.RWMutex
	services map[string]config.Service
	views    map[string]View
	locks    map[string]*sync.Mutex
	executor Executor
}

func New(services []config.Service, e Executor) *Manager {
	m := &Manager{services: map[string]config.Service{}, views: map[string]View{}, locks: map[string]*sync.Mutex{}, executor: e}
	for _, s := range services {
		m.services[s.Name] = s
		m.locks[s.Name] = &sync.Mutex{}
		actions := []string{"start", "stop", "restart"}
		if s.Name == "redis" {
			actions = []string{}
		}
		if s.Name == "nginx" && s.CheckProgram != "" {
			actions = append(actions, "reload")
		}
		m.views[s.Name] = View{Name: s.Name, State: "unknown", Manager: s.Manager, Actions: actions, Message: "等待状态检查"}
	}
	return m
}
func (m *Manager) List() []View {
	m.mu.RLock()
	defer m.mu.RUnlock()
	out := []View{}
	for _, name := range []string{"nginx", "php-fpm", "mysql", "mariadb", "redis"} {
		if v, ok := m.views[name]; ok {
			out = append(out, v)
		}
	}
	return out
}
func (m *Manager) Refresh(ctx context.Context) {
	var wg sync.WaitGroup
	for name := range m.services {
		wg.Add(1)
		go func(name string) {
			defer wg.Done()
			lock := m.locks[name]
			if !lock.TryLock() {
				return
			}
			defer lock.Unlock()
			m.refreshOne(ctx, name)
			m.refreshVersion(ctx, name)
		}(name)
	}
	wg.Wait()
}
func (m *Manager) refreshOne(ctx context.Context, name string) {
	ctx, cancel := context.WithTimeout(ctx, 8*time.Second)
	defer cancel()
	r, e := m.executor.Run(ctx, name, "status")
	m.mu.Lock()
	defer m.mu.Unlock()
	v := m.views[name]
	v.CheckedAt = time.Now()
	if e != nil {
		v.State = "unknown"
		v.Message = e.Error()
	} else {
		v.State = r.State
		v.Message = r.Message
	}
	m.views[name] = v
}
func (m *Manager) Run(ctx context.Context) {
	m.Refresh(ctx)
	t := time.NewTicker(5 * time.Second)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			m.Refresh(ctx)
		}
	}
}
func (m *Manager) Action(ctx context.Context, name, action string) (Result, error) {
	m.mu.RLock()
	v, ok := m.views[name]
	m.mu.RUnlock()
	allowed := false
	for _, a := range v.Actions {
		if a == action {
			allowed = true
		}
	}
	if !ok || !allowed {
		return Result{}, ErrUnsupported
	}
	lock := m.locks[name]
	if !lock.TryLock() {
		return Result{}, ErrBusy
	}
	defer lock.Unlock()
	m.mu.Lock()
	v = m.views[name]
	v.Busy = true
	m.views[name] = v
	m.mu.Unlock()
	defer func() { m.mu.Lock(); v := m.views[name]; v.Busy = false; m.views[name] = v; m.mu.Unlock() }()
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	r, e := m.executor.Run(ctx, name, action)
	slog.Info("service operation", "service", name, "action", action, "success", e == nil)
	m.refreshOne(ctx, name)
	return r, e
}

type limitedBuffer struct{ data []byte }

func (b *limitedBuffer) Write(p []byte) (int, error) {
	n := len(p)
	remaining := 16*1024 - len(b.data)
	if remaining > 0 {
		if len(p) > remaining {
			p = p[:remaining]
		}
		b.data = append(b.data, p...)
	}
	return n, nil
}
func RunCommand(ctx context.Context, program string, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, program, args...)
	cmd.Env = []string{"PATH=/usr/sbin:/usr/bin:/sbin:/bin:/usr/local/sbin:/usr/local/bin", "LANG=C", "LC_ALL=C"}
	b := &limitedBuffer{}
	cmd.Stdout = b
	cmd.Stderr = b
	configureProcess(cmd)
	err := cmd.Run()
	if ctx.Err() != nil {
		return strings.TrimSpace(string(b.data)), ctx.Err()
	}
	return strings.TrimSpace(string(b.data)), err
}
