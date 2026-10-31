package control

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"os/exec"
	"time"
)

// Configured describes persisted configuration, not runtime PHP/SQL variables.
type LoggingView struct {
	Kind       string `json:"kind"`
	Service    string `json:"service"`
	Path       string `json:"path"`
	Available  bool   `json:"available"`
	Configured bool   `json:"configured"`
	Message    string `json:"message"`
}
type LoggingSnapshot struct {
	PHP      LoggingView `json:"php"`
	Database LoggingView `json:"database"`
}
type LoggingBackend interface {
	LoggingStatus(context.Context) (LoggingSnapshot, error)
	EnableLogging(context.Context, string) (Result, error)
}

func validLoggingKind(kind string) bool { return kind == "php" || kind == "database" }

func (h Helper) loggingCall(ctx context.Context, action string, output any) error {
	cmd := exec.CommandContext(ctx, "/usr/bin/sudo", "-n", h.Path, "logging", action)
	// Let the privileged helper restore configuration if the client disconnects.
	configureProvisionProcess(cmd)
	cmd.WaitDelay = 35 * time.Second
	cmd.Env = []string{"PATH=/usr/sbin:/usr/bin:/sbin:/bin", "LANG=C", "LC_ALL=C"}
	stdout, stderr := &limitedBuffer{}, &limitedBuffer{}
	cmd.Stdout, cmd.Stderr = stdout, stderr
	if err := cmd.Run(); err != nil {
		var result Result
		if json.Unmarshal(stdout.data, &result) == nil && result.Message != "" {
			return errors.New(result.Message)
		}
		return errors.New("日志设置辅助程序不可用，请同步升级辅助程序和 sudoers 权限")
	}
	if err := json.Unmarshal(stdout.data, output); err != nil {
		return errors.New("日志设置辅助程序返回无效数据")
	}
	return nil
}
func (h Helper) LoggingStatus(ctx context.Context) (LoggingSnapshot, error) {
	var result LoggingSnapshot
	err := h.loggingCall(ctx, "status", &result)
	if err == nil && (result.PHP.Kind != "php" || result.Database.Kind != "database") {
		err = errors.New("日志设置状态无效")
	}
	return result, err
}
func (h Helper) EnableLogging(ctx context.Context, kind string) (Result, error) {
	if !validLoggingKind(kind) {
		return Result{}, ErrUnsupported
	}
	var result Result
	err := h.loggingCall(ctx, "enable-"+kind, &result)
	return result, err
}
func (m *Manager) LoggingStatus(ctx context.Context) (LoggingSnapshot, error) {
	backend, ok := m.executor.(LoggingBackend)
	if !ok {
		return LoggingSnapshot{}, ErrUnsupported
	}
	ctx, done := context.WithTimeout(ctx, 15*time.Second)
	defer done()
	return backend.LoggingStatus(ctx)
}
func (m *Manager) EnableLogging(ctx context.Context, kind string) (Result, error) {
	if !validLoggingKind(kind) {
		return Result{}, ErrUnsupported
	}
	backend, ok := m.executor.(LoggingBackend)
	if !ok {
		return Result{}, ErrUnsupported
	}
	names := []string{"php-fpm"}
	if kind == "database" {
		names = []string{"mysql", "mariadb"}
	}
	// Lock both database aliases, consistently with service actions in the panel.
	acquired := []string{}
	m.mu.RLock()
	for _, name := range names {
		if lock := m.locks[name]; lock != nil {
			if !lock.TryLock() {
				for _, prior := range acquired {
					m.locks[prior].Unlock()
				}
				m.mu.RUnlock()
				return Result{}, ErrBusy
			}
			acquired = append(acquired, name)
		}
	}
	m.mu.RUnlock()
	if len(acquired) == 0 {
		return Result{}, ErrUnsupported
	}
	m.mu.Lock()
	for _, name := range acquired {
		v := m.views[name]
		v.Busy = true
		m.views[name] = v
	}
	m.mu.Unlock()
	defer func() {
		m.mu.Lock()
		defer m.mu.Unlock()
		for _, name := range acquired {
			v := m.views[name]
			v.Busy = false
			m.views[name] = v
			m.locks[name].Unlock()
		}
	}()
	ctx, done := context.WithTimeout(ctx, 100*time.Second)
	defer done()
	result, err := backend.EnableLogging(ctx, kind)
	slog.Info("enable logging", "kind", kind, "success", err == nil)
	refresh, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	for _, name := range acquired {
		m.refreshOne(refresh, name)
	}
	return result, err
}
