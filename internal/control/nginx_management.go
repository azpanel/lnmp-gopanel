package control

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"os/exec"
	"time"

	"github.com/azpanel/lnmp-gopanel/internal/config"
)

type serviceProbe func(context.Context, config.Service) (Result, bool)

// ExecuteManaged repeats management checks inside the privileged command path;
// cached panel state and disabled UI controls are not sufficient authorization.
func ExecuteManaged(ctx context.Context, s config.Service, action string, run CommandRunner) (Result, error) {
	return executeManaged(ctx, s, action, run, NginxManagementStatus, NativeStatus)
}

func executeManaged(ctx context.Context, s config.Service, action string, run CommandRunner, management, native serviceProbe) (Result, error) {
	if err := s.Validate(); err != nil {
		return Result{}, err
	}
	if !config.ValidAction(action) {
		return Result{}, ErrUnsupported
	}
	if result, handled := management(ctx, s); handled {
		if action == "status" {
			return result, nil
		}
		if result.State == "unmanaged" {
			return result, fmt.Errorf("%w：%s", ErrManagementConflict, result.Message)
		}
		return result, errors.New(result.Message)
	}
	if action == "status" {
		if result, handled := native(ctx, s); handled {
			result.Manager = s.Manager
			return result, nil
		}
	}
	if s.Name == "nginx" && action == "start" {
		probe := s
		probe.Manager = "init"
		if result, handled := native(ctx, probe); handled && result.State != "stopped" {
			return Result{State: result.State, Message: "Nginx 已在运行或无法确认进程状态，未执行重复启动"}, errors.New("Nginx 已在运行或无法确认进程状态，未执行重复启动")
		}
	}
	result, err := Execute(ctx, s, action, run)
	result.Manager = s.Manager
	return result, err
}

// ManagementRepairPlan binds confirmation to the exact configuration, unit and
// running master inspected by the privileged helper. No paths come from clients.
type ManagementRepairPlan struct {
	Revision        string `json:"revision"`
	CurrentManager  string `json:"current_manager"`
	ProposedManager string `json:"proposed_manager"`
	MasterPID       int    `json:"master_pid"`
	RestorePID      bool   `json:"restore_pid"`
	Message         string `json:"message"`
}

type ManagementRepairRequest struct {
	Revision string `json:"revision"`
}

type ManagementRepairer interface {
	PreviewManagementRepair(context.Context) (ManagementRepairPlan, error)
	RepairManagement(context.Context, ManagementRepairRequest) (Result, error)
}

func (h Helper) managementCall(ctx context.Context, action string, input, output any) error {
	b, err := json.Marshal(input)
	if err != nil {
		return err
	}
	cmd := exec.CommandContext(ctx, "/usr/bin/sudo", "-n", h.Path, "nginx", action)
	configureProcess(cmd)
	cmd.Stdin = bytes.NewReader(b)
	out := &limitedBuffer{}
	cmd.Stdout, cmd.Stderr = out, &limitedBuffer{}
	if err := cmd.Run(); err != nil {
		var result Result
		if json.Unmarshal(out.data, &result) == nil && result.Message != "" {
			return errors.New(result.Message)
		}
		return errors.New("管理方式修复辅助程序不可用，请检查安装与 sudoers 配置")
	}
	return json.Unmarshal(out.data, output)
}

func (h Helper) PreviewManagementRepair(ctx context.Context) (ManagementRepairPlan, error) {
	var plan ManagementRepairPlan
	err := h.managementCall(ctx, "repair-preview", struct{}{}, &plan)
	return plan, err
}

func (h Helper) RepairManagement(ctx context.Context, r ManagementRepairRequest) (Result, error) {
	var result Result
	err := h.managementCall(ctx, "repair", r, &result)
	return result, err
}

func (m *Manager) ManagementRepairPreview(ctx context.Context, name string) (ManagementRepairPlan, error) {
	backend, ok := m.executor.(ManagementRepairer)
	if name != "nginx" || !ok {
		return ManagementRepairPlan{}, ErrUnsupported
	}
	m.mu.RLock()
	lock := m.locks[name]
	m.mu.RUnlock()
	if lock == nil {
		return ManagementRepairPlan{}, ErrUnsupported
	}
	if !lock.TryLock() {
		return ManagementRepairPlan{}, ErrBusy
	}
	defer lock.Unlock()
	return backend.PreviewManagementRepair(ctx)
}

func (m *Manager) RepairManagement(ctx context.Context, name string, r ManagementRepairRequest) (Result, error) {
	backend, ok := m.executor.(ManagementRepairer)
	if name != "nginx" || !ok {
		return Result{}, ErrUnsupported
	}
	m.mu.RLock()
	lock := m.locks[name]
	m.mu.RUnlock()
	if lock == nil {
		return Result{}, ErrUnsupported
	}
	if !lock.TryLock() {
		return Result{}, ErrBusy
	}
	defer lock.Unlock()
	m.mu.Lock()
	v := m.views[name]
	v.Busy = true
	m.views[name] = v
	m.mu.Unlock()
	defer func() {
		m.mu.Lock()
		v := m.views[name]
		v.Busy = false
		m.views[name] = v
		m.mu.Unlock()
	}()
	ctx, cancel := context.WithTimeout(ctx, 25*time.Second)
	defer cancel()
	result, err := backend.RepairManagement(ctx, r)
	slog.Info("service management repair", "service", name, "success", err == nil)
	// Refresh even after rollback so the UI describes the actual current state.
	refresh, done := context.WithTimeout(context.Background(), 8*time.Second)
	defer done()
	m.refreshOne(refresh, name)
	return result, err
}
