package control

import (
	"context"
	"errors"
	"testing"

	"github.com/azpanel/lnmp-gopanel/internal/config"
)

type loggingManagerStub struct{ calls int }

func (*loggingManagerStub) Run(context.Context, string, string) (Result, error) {
	return Result{State: "running"}, nil
}
func (*loggingManagerStub) LoggingStatus(context.Context) (LoggingSnapshot, error) {
	return LoggingSnapshot{}, nil
}
func (s *loggingManagerStub) EnableLogging(context.Context, string) (Result, error) {
	s.calls++
	return Result{State: "running"}, nil
}
func TestLoggingUsesServiceLocks(t *testing.T) {
	b := &loggingManagerStub{}
	m := New([]config.Service{{Name: "php-fpm"}, {Name: "mysql"}, {Name: "mariadb"}}, b)
	for _, tc := range []struct{ kind, service string }{{"php", "php-fpm"}, {"database", "mysql"}, {"database", "mariadb"}} {
		m.locks[tc.service].Lock()
		if _, err := m.EnableLogging(context.Background(), tc.kind); !errors.Is(err, ErrBusy) {
			t.Fatal(err)
		}
		m.locks[tc.service].Unlock()
		if tc.service == "mariadb" && !m.locks["mysql"].TryLock() {
			t.Fatal("partial lock not released")
		} else if tc.service == "mariadb" {
			m.locks["mysql"].Unlock()
		}
	}
	if b.calls != 0 {
		t.Fatal(b.calls)
	}
	if _, err := m.EnableLogging(context.Background(), "php"); err != nil || b.calls != 1 {
		t.Fatal(err, b.calls)
	}
	for _, v := range m.List() {
		if v.Busy {
			t.Fatal(v)
		}
	}
	if _, err := m.EnableLogging(context.Background(), "php;id"); !errors.Is(err, ErrUnsupported) {
		t.Fatal(err)
	}
}
