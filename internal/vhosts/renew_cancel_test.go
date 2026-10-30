package vhosts

import (
	"context"
	"errors"
	"fmt"
	"sync/atomic"
	"testing"
	"time"
)

type cancelRenewBackend struct {
	createBackend
	calls atomic.Int32
	run   func(context.Context) error
}

func (*cancelRenewBackend) PreviewRenewal(context.Context, []string) ([]RenewPreview, error) {
	return nil, nil
}
func (b *cancelRenewBackend) RenewVhost(ctx context.Context, _ string, _ RenewItem, _ func(CreateEvent)) error {
	b.calls.Add(1)
	return b.run(ctx)
}

func cancelRenewRequest(count int) RenewRequest {
	r := RenewRequest{RequestID: "cancel-renew-test"}
	for i := 0; i < count; i++ {
		r.Items = append(r.Items, RenewItem{Name: fmt.Sprintf("site-%d.conf", i), Provider: "letsencrypt", Email: "a@example.com"})
	}
	return r
}

func awaitRenewal(t *testing.T, m *Jobs, id string) Job {
	t.Helper()
	var version uint64
	deadline := time.NewTimer(3 * time.Second)
	defer deadline.Stop()
	for {
		out, ok := m.Events(id, version, 0)
		if !ok {
			t.Fatal("task disappeared")
		}
		if out.Changed && out.Job.Status != "running" {
			return out.Job
		}
		version = out.Version
		select {
		case <-out.Notify:
		case <-deadline.C:
			t.Fatal("task did not terminate")
		}
	}
}

func TestRenewalRejectsCanceledParentAndKeepsIdempotency(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	b := &cancelRenewBackend{run: func(context.Context) error { return nil }}
	m := NewJobs(ctx, b, nil)
	r := cancelRenewRequest(1)
	job, err := m.StartRenewal(r)
	if err != nil {
		t.Fatal(err)
	}
	awaitRenewal(t, m, job.ID)
	cancel()
	if again, err := m.StartRenewal(r); err != nil || again.ID != job.ID {
		t.Fatal("lost idempotent result", err)
	}
	r.RequestID = "canceled-new-task"
	if _, err := m.StartRenewal(r); !errors.Is(err, context.Canceled) {
		t.Fatal("accepted canceled parent", err)
	}
	if len(m.List()) != 1 || b.calls.Load() != 1 {
		t.Fatal("created new work after cancellation")
	}
}

func TestRenewalCancellationWaitsForCurrentAndSkipsRemaining(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	entered, canceled, release := make(chan struct{}), make(chan struct{}), make(chan struct{})
	b := &cancelRenewBackend{run: func(ctx context.Context) error {
		close(entered)
		<-ctx.Done()
		close(canceled)
		<-release
		return ctx.Err()
	}}
	var callbacks atomic.Int32
	m := NewJobs(ctx, b, func() { callbacks.Add(1) })
	job, err := m.StartRenewal(cancelRenewRequest(1000))
	if err != nil {
		t.Fatal(err)
	}
	<-entered
	cancel()
	<-canceled
	m.mu.RLock()
	active := m.active
	m.mu.RUnlock()
	if active != job.ID || callbacks.Load() != 0 {
		t.Fatal("released task before backend cleanup")
	}
	close(release)
	out := awaitRenewal(t, m, job.ID)
	if b.calls.Load() != 1 || callbacks.Load() != 1 || out.Status != "canceled" || out.Completed != 1 || out.Failed != 1 || out.Canceled != 999 {
		t.Fatalf("unexpected cancellation: calls=%d callbacks=%d job=%+v", b.calls.Load(), callbacks.Load(), out)
	}
	for _, item := range out.Items[1:] {
		if item.Status != "canceled" {
			t.Fatal("remaining item was scheduled")
		}
	}
	m.mu.RLock()
	active = m.active
	m.mu.RUnlock()
	if active != "" {
		t.Fatal("active task was not released")
	}
}

func TestRenewalCancellationBetweenItems(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	b := &cancelRenewBackend{run: func(context.Context) error { return nil }}
	m := NewJobs(ctx, b, cancel) // Cancel after the first completed backend call.
	job, err := m.StartRenewal(cancelRenewRequest(3))
	if err != nil {
		t.Fatal(err)
	}
	out := awaitRenewal(t, m, job.ID)
	if out.Status != "canceled" || out.Completed != 1 || out.Succeeded != 1 || out.Failed != 0 || out.Canceled != 2 || b.calls.Load() != 1 {
		t.Fatalf("unexpected counts: %+v", out)
	}
}

func TestRenewalCancellationBeforeDispatch(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	b := &cancelRenewBackend{run: func(context.Context) error { t.Error("unexpected backend call"); return nil }}
	m := NewJobs(ctx, b, func() { t.Error("unexpected callback") })
	r := cancelRenewRequest(3)
	j := &Job{ID: r.RequestID, Status: "running", Items: r.Items}
	m.remember(j)
	cancel()
	m.runRenewal(j.ID, b)
	out, _ := m.Get(j.ID)
	if out.Status != "canceled" || out.Completed != 0 || out.Failed != 0 || out.Canceled != 3 {
		t.Fatal(out)
	}
}

func TestRenewalSingleItemTimeoutContinuesBatch(t *testing.T) {
	var attempts atomic.Int32
	b := &cancelRenewBackend{run: func(context.Context) error {
		if attempts.Add(1) == 1 {
			return context.DeadlineExceeded
		}
		return nil
	}}
	m := NewJobs(context.Background(), b, nil)
	j, err := m.StartRenewal(cancelRenewRequest(2))
	if err != nil {
		t.Fatal(err)
	}
	out := awaitRenewal(t, m, j.ID)
	if out.Status != "partial" || out.Completed != 2 || out.Failed != 1 || out.Succeeded != 1 || out.Canceled != 0 || b.calls.Load() != 2 {
		t.Fatalf("single-item timeout canceled remaining batch: %+v", out)
	}
}
