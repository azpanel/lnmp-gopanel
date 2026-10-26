package control

import (
	"context"
	"errors"
	"reflect"
	"sync/atomic"
	"testing"
	"time"

	"github.com/azpanel/lnmp-gopanel/internal/config"
)

var nginx = config.Service{Name: "nginx", Manager: "init", Program: "/etc/init.d/nginx", CheckProgram: "/usr/local/nginx/sbin/nginx"}

func TestReloadRequiresPassingConfig(t *testing.T) {
	for _, pass := range []bool{false, true} {
		calls := [][]string{}
		run := func(ctx context.Context, program string, args ...string) (string, error) {
			calls = append(calls, append([]string{program}, args...))
			if program == nginx.CheckProgram && !pass {
				return "bad configuration", errors.New("invalid config")
			}
			return "", nil
		}
		_, err := Execute(context.Background(), nginx, "reload", run)
		if (err == nil) != pass {
			t.Fatal("wrong reload result")
		}
		want := [][]string{{nginx.CheckProgram, "-t"}}
		if pass {
			want = append(want, []string{nginx.Program, "reload"})
		}
		if !reflect.DeepEqual(calls, want) {
			t.Fatalf("commands: %+v", calls)
		}
	}
}
func TestUnsupportedActionsNeverExecute(t *testing.T) {
	for _, action := range []string{"status; id", "reload && id", "delete", "", "--help"} {
		called := false
		_, err := Execute(context.Background(), nginx, action, func(context.Context, string, ...string) (string, error) { called = true; return "", nil })
		if err == nil || called {
			t.Fatal("invalid action reached executor")
		}
	}
	s := config.Service{Name: "mysql", Manager: "init", Program: "/etc/init.d/mysql"}
	_, err := Execute(context.Background(), s, "reload", func(context.Context, string, ...string) (string, error) {
		t.Fatal("unsupported reload executed")
		return "", nil
	})
	if !errors.Is(err, ErrUnsupported) {
		t.Fatal(err)
	}
}
func TestStatusParsing(t *testing.T) {
	for _, tc := range []struct {
		output, want string
		fail         bool
	}{{"active", "running", false}, {"inactive", "stopped", true}, {"failed", "stopped", true}, {"activating", "transitioning", true}, {"unexpected", "unknown", true}} {
		s := config.Service{Name: "nginx", Manager: "systemd", Program: "/usr/bin/systemctl", Target: "nginx.service"}
		r, e := Execute(context.Background(), s, "status", func(context.Context, string, ...string) (string, error) {
			if tc.fail {
				return tc.output, errors.New("exit")
			}
			return tc.output, nil
		})
		if e != nil || r.State != tc.want {
			t.Fatalf("%s: %+v,%v", tc.output, r, e)
		}
	}
}

type fakeExecutor struct {
	started chan struct{}
	release chan struct{}
	calls   atomic.Int32
}

func (f *fakeExecutor) Run(ctx context.Context, name, action string) (Result, error) {
	f.calls.Add(1)
	if action != "status" {
		close(f.started)
		select {
		case <-f.release:
		case <-ctx.Done():
			return Result{}, ctx.Err()
		}
	}
	return Result{State: "running"}, nil
}
func TestSameServiceOperationsAreExclusive(t *testing.T) {
	f := &fakeExecutor{started: make(chan struct{}), release: make(chan struct{})}
	m := New([]config.Service{nginx}, f)
	done := make(chan error, 1)
	go func() { _, e := m.Action(context.Background(), "nginx", "restart"); done <- e }()
	<-f.started
	if _, err := m.Action(context.Background(), "nginx", "stop"); !errors.Is(err, ErrBusy) {
		t.Fatal("concurrent operation accepted")
	}
	if !m.List()[0].Busy {
		t.Fatal("busy state missing")
	}
	m.Refresh(context.Background())
	if f.calls.Load() != 1 {
		t.Fatal("status check raced with operation")
	}
	close(f.release)
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	if m.List()[0].Busy {
		t.Fatal("busy state not cleared")
	}
}
func TestCommandTimeout(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 40*time.Millisecond)
	defer cancel()
	start := time.Now()
	_, err := RunCommand(ctx, "/bin/sleep", "5")
	if !errors.Is(err, context.DeadlineExceeded) || time.Since(start) > 2*time.Second {
		t.Fatalf("command did not terminate: %v", err)
	}
}
func TestCommandOutputIsBounded(t *testing.T) {
	b := &limitedBuffer{}
	data := make([]byte, 64*1024)
	n, err := b.Write(data)
	if n != len(data) || err != nil || len(b.data) != 16*1024 {
		t.Fatal("output is not bounded")
	}
}

func TestGeneratedSysVUnitDoesNotOverrideInitManager(t *testing.T) {
	for _, tc := range []struct {
		state, path string
		want        bool
	}{
		{"loaded", "/usr/lib/systemd/system/nginx.service", true},
		{"loaded", "/etc/systemd/system/nginx.service", true},
		{"loaded", "/run/systemd/generator.late/nginx.service", false},
		{"not-found", "", false},
		{"loaded", "", false},
	} {
		if nativeUnit(map[string]string{"LoadState": tc.state, "FragmentPath": tc.path}) != tc.want {
			t.Fatalf("wrong manager for %s", tc.path)
		}
	}
}

func TestRunningLNMPProcessOutsideNativeUnitKeepsInitManager(t *testing.T) {
	for _, tc := range []struct {
		name, state, process string
		hasInit, wantNative  bool
	}{
		{"failed unit, live daemon", "failed", "running", true, false},
		{"inactive unit, live daemon", "inactive", "running", true, false},
		{"active unit owns daemon", "active", "running", true, true},
		{"inactive unit, stopped daemon", "inactive", "stopped", true, true},
		{"failed unit without init alternative", "failed", "running", false, true},
		{"unreadable process status", "failed", "unknown", true, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			properties := map[string]string{"LoadState": "loaded", "FragmentPath": "/etc/systemd/system/nginx.service", "ActiveState": tc.state}
			if got := useNativeUnit(properties, tc.hasInit, tc.process); got != tc.wantNative {
				t.Fatalf("use native unit=%v, want %v", got, tc.wantNative)
			}
		})
	}
}
