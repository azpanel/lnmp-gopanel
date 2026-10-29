package control

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/azpanel/lnmp-gopanel/internal/config"
)

func TestManagedCommandsRejectConflictBeforeExecution(t *testing.T) {
	for _, action := range []string{"status", "start", "stop", "restart", "reload"} {
		t.Run(action, func(t *testing.T) {
			probe := func(context.Context, config.Service) (Result, bool) {
				return Result{State: "unmanaged", Message: "management conflict"}, true
			}
			run := func(context.Context, string, ...string) (string, error) {
				t.Fatal("conflict executed a command")
				return "", nil
			}
			result, err := executeManaged(context.Background(), nginx, action, run, probe, probe)
			if result.State != "unmanaged" || (action == "status" && err != nil) || (action != "status" && !errors.Is(err, ErrManagementConflict)) {
				t.Fatal(result, err)
			}
		})
	}
	for _, state := range []string{"running", "unknown", "transitioning"} {
		probe := func(context.Context, config.Service) (Result, bool) { return Result{State: state}, true }
		unhandled := func(context.Context, config.Service) (Result, bool) { return Result{}, false }
		run := func(context.Context, string, ...string) (string, error) {
			t.Fatal("duplicate start executed")
			return "", nil
		}
		if _, err := executeManaged(context.Background(), nginx, "start", run, unhandled, probe); err == nil {
			t.Fatal("unconfirmed start accepted", state)
		}
	}
}

type managementExecutor struct {
	state            string
	repairs          int
	operations       int
	started, release chan struct{}
}

func (e *managementExecutor) Run(_ context.Context, _, action string) (Result, error) {
	if action != "status" {
		e.operations++
	}
	return Result{State: e.state, Manager: map[bool]string{true: "init", false: "systemd"}[e.state == "running"]}, nil
}
func (e *managementExecutor) PreviewManagementRepair(context.Context) (ManagementRepairPlan, error) {
	return ManagementRepairPlan{Revision: strings.Repeat("a", 64), MasterPID: 123}, nil
}
func (e *managementExecutor) RepairManagement(ctx context.Context, _ ManagementRepairRequest) (Result, error) {
	e.repairs++
	if e.started != nil {
		close(e.started)
		select {
		case <-e.release:
		case <-ctx.Done():
			return Result{}, ctx.Err()
		}
	}
	e.state = "running"
	return Result{State: "running", Manager: "init"}, nil
}

func TestManagementRepairRefreshesPersistedManagerAndLocksActions(t *testing.T) {
	e := &managementExecutor{state: "unmanaged", started: make(chan struct{}), release: make(chan struct{})}
	m := New([]config.Service{nginx}, e)
	m.Refresh(context.Background())
	if _, err := m.Action(context.Background(), "nginx", "restart"); !errors.Is(err, ErrManagementConflict) || e.operations != 0 {
		t.Fatal("conflicting cached state reached executor", err)
	}
	done := make(chan error, 1)
	go func() {
		_, err := m.RepairManagement(context.Background(), "nginx", ManagementRepairRequest{strings.Repeat("a", 64)})
		done <- err
	}()
	<-e.started
	if !m.List()[0].Busy {
		t.Fatal("repair is not busy")
	}
	if _, err := m.Action(context.Background(), "nginx", "restart"); !errors.Is(err, ErrBusy) {
		t.Fatal(err)
	}
	if _, err := m.ManagementRepairPreview(context.Background(), "nginx"); !errors.Is(err, ErrBusy) {
		t.Fatal(err)
	}
	close(e.release)
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	view := m.List()[0]
	if view.State != "running" || view.Manager != "init" || view.Busy {
		t.Fatal(view)
	}
	if _, err := m.ManagementRepairPreview(context.Background(), "mysql"); !errors.Is(err, ErrUnsupported) {
		t.Fatal(err)
	}
}
