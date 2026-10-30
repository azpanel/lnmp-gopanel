package vhosts

import (
	"context"
	"fmt"
	"testing"
	"time"
)

func eventFixture(lines, items int) (*Jobs, *Job) {
	m := NewJobs(context.Background(), nil, nil)
	j := &Job{ID: "events-test", Status: "running", Lines: []JobLine{}}
	for i := 1; i <= lines; i++ {
		j.Lines = append(j.Lines, JobLine{ID: uint64(i), Text: "output"})
	}
	j.Items = make([]RenewItem, items)
	m.remember(j)
	return m, j
}

func TestJobEventsIncrementalAndNotification(t *testing.T) {
	m, j := eventFixture(1000, 1000)
	initial, ok := m.Events(j.ID, 0, 0)
	if !ok || !initial.Snapshot || len(initial.Job.Lines) != 1000 {
		t.Fatal("missing initial snapshot")
	}
	initial.Job.Items[0].Status = "external"
	initial.Job.Lines[0].Text = "external"
	if j.Items[0].Status != "" || j.Lines[0].Text != "output" {
		t.Fatal("snapshot aliases mutable state")
	}
	idle, _ := m.Events(j.ID, initial.Version, 1000)
	if idle.Changed || idle.Job.Items != nil || idle.Job.Lines != nil {
		t.Fatal("idle read copied state")
	}
	if allocations := testing.AllocsPerRun(100, func() { m.Events(j.ID, initial.Version, 1000) }); allocations != 0 {
		t.Fatalf("idle read allocates: %v", allocations)
	}
	m.mu.Lock()
	j.Lines = append(j.Lines[1:], JobLine{ID: 1001, Text: "new"})
	touchJob(j)
	m.mu.Unlock()
	select {
	case <-idle.Notify:
	default:
		t.Fatal("update did not wake reader")
	}
	delta, _ := m.Events(j.ID, initial.Version, 1000)
	if !delta.Changed || delta.Snapshot || len(delta.Job.Lines) != 1 || delta.Job.Lines[0].ID != 1001 {
		t.Fatal("did not read only incremental lines", delta)
	}
	// Status-only changes must wake streams even when no line was emitted.
	m.mu.Lock()
	j.Status = "canceled"
	touchJob(j)
	m.mu.Unlock()
	terminal, _ := m.Events(j.ID, delta.Version, 1001)
	if !terminal.Changed || terminal.Snapshot || len(terminal.Job.Lines) != 0 || terminal.Job.Status != "canceled" {
		t.Fatal("lost terminal status")
	}
}

func TestJobEventsRepairCursorAndEviction(t *testing.T) {
	m, j := eventFixture(3, 0)
	for i := range j.Lines {
		j.Lines[i].ID += 1000
	}
	for _, cursor := range []uint64{0, 1, 999, 1004, ^uint64(0)} {
		out, _ := m.Events(j.ID, 0, cursor)
		if !out.Snapshot || len(out.Job.Lines) != 3 {
			t.Fatalf("cursor %d did not repair window", cursor)
		}
	}
	out, _ := m.Events(j.ID, 0, 1000)
	if out.Snapshot || len(out.Job.Lines) != 3 {
		t.Fatal("cursor immediately before window needs only a delta")
	}
	for i := 0; i < 20; i++ {
		m.remember(&Job{ID: fmt.Sprint(i)})
	}
	select {
	case <-out.Notify:
	case <-time.After(time.Second):
		t.Fatal("eviction did not wake readers")
	}
	if _, ok := m.Events(j.ID, out.Version, 1003); ok {
		t.Fatal("evicted task remains available")
	}
}

func BenchmarkJobEventsUnchanged(b *testing.B) {
	m, j := eventFixture(1000, 1000)
	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		m.Events(j.ID, j.version, 1000)
	}
}

func BenchmarkJobEventsDelta(b *testing.B) {
	m, j := eventFixture(1000, 0)
	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		m.Events(j.ID, 0, 999)
	}
}
