package docker

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

const containerFixture = `{"ID":"0123456789abcdef","Names":"web","Image":"example/web:latest","Command":"\"sh -c 'echo hello'\"","CreatedAt":"2026-10-26 10:00:00 +0800 CST","RunningFor":"2 hours ago","State":"running","Status":"Up 2 hours (healthy)","Ports":"127.0.0.1:8080->80/tcp","Size":"24.6kB (virtual 276MB)","Labels":"private metadata"}`
const diskFixture = `{"Type":"Images","TotalCount":"2","Active":"1","Size":"378.1MB","Reclaimable":"100MB (26%)"}
{"Type":"Containers","TotalCount":1,"Active":1,"Size":"24.58kB","Reclaimable":"0B (0%)"}`

func TestCollectOnlyRunsFixedLocalReadOnlyCommands(t *testing.T) {
	var commands [][]string
	r := Collect(context.Background(), func(ctx context.Context, args ...string) (string, error) {
		if _, ok := ctx.Deadline(); !ok {
			t.Fatal("command has no timeout")
		}
		commands = append(commands, args)
		if args[2] == "ps" {
			return containerFixture, nil
		}
		return diskFixture, nil
	})
	want := [][]string{
		{"--host", "unix:///var/run/docker.sock", "ps", "--size", "--no-trunc", "--format", "{{json .}}"},
		{"--host", "unix:///var/run/docker.sock", "system", "df", "--format", "{{json .}}"},
	}
	if !reflect.DeepEqual(commands, want) {
		t.Fatalf("unexpected Docker commands: %#v", commands)
	}
	if r.Status != "ready" || r.UpdatedAt == nil || len(r.Containers) != 1 || len(r.DiskUsage) != 2 || len(r.Errors) != 0 {
		t.Fatalf("bad snapshot: %+v", r)
	}
	c := r.Containers[0]
	if c.Name != "web" || c.Command != `"sh -c 'echo hello'"` || c.Size != "24.6kB (virtual 276MB)" || c.Ports != "127.0.0.1:8080->80/tcp" {
		t.Fatalf("fields lost: %+v", c)
	}
	if r.DiskUsage[0].Total != 2 || r.DiskUsage[1].Active != 1 {
		t.Fatal("numeric and quoted counts not decoded")
	}
}

func TestEmptyContainersAndPartialSpaceFailure(t *testing.T) {
	r := Collect(context.Background(), func(ctx context.Context, args ...string) (string, error) {
		if args[2] == "ps" {
			return "", nil
		}
		return "", errors.New("daemon busy")
	})
	if r.Status != "partial" || r.Containers == nil || len(r.Containers) != 0 || r.DiskUsage != nil || r.Errors["disk_usage"] == "" || r.UpdatedAt == nil {
		t.Fatalf("bad partial result: %+v", r)
	}
}

func TestMalformedOrIncompleteOutputNeverLooksSuccessful(t *testing.T) {
	for _, output := range []string{`{"ID":"ok","Names":"web"}` + "\n" + `{"ID":`, `{"message":"error"}`, `null`, `[]`} {
		if _, err := parseContainers(output); err == nil {
			t.Fatalf("invalid containers accepted: %q", output)
		}
	}
	for _, output := range []string{"", `{"Type":"Images","TotalCount":"unknown","Active":"0","Size":"0B"}`, `{"Type":"Images","TotalCount":-1,"Active":0,"Size":"0B"}`} {
		if _, err := parseDiskUsage(output); err == nil {
			t.Fatalf("invalid disk usage accepted: %q", output)
		}
	}
	output := strings.Repeat(containerFixture+"\n", 10001)
	if _, err := parseContainers(output); err == nil {
		t.Fatal("unbounded container inventory accepted")
	}
	r := Collect(context.Background(), func(context.Context, ...string) (string, error) { return "", errors.New("daemon unavailable") })
	if r.Status != "failed" || r.UpdatedAt != nil || len(r.Errors) != 2 {
		t.Fatalf("daemon failure hidden: %+v", r)
	}
}

type backendResult struct {
	snapshot Snapshot
	err      error
}
type blockedBackend struct {
	calls   atomic.Int32
	results chan backendResult
}

func (b *blockedBackend) Docker(ctx context.Context) (Snapshot, error) {
	b.calls.Add(1)
	select {
	case r := <-b.results:
		return r.snapshot, r.err
	case <-ctx.Done():
		return Snapshot{}, ctx.Err()
	}
}
func waitSnapshot(t *testing.T, m *Manager, status string) Snapshot {
	t.Helper()
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		v := m.Latest(false)
		if v.Status == status {
			return v
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatalf("snapshot did not reach %s", status)
	return Snapshot{}
}
func TestManagerDeduplicatesAndKeepsPreviousDataOnFailure(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	b := &blockedBackend{results: make(chan backendResult, 1)}
	m := NewManager(ctx, b)
	for i := 0; i < 20; i++ {
		if m.Latest(true).Status != "loading" {
			t.Fatal("query did not return loading immediately")
		}
	}
	now := time.Now()
	b.results <- backendResult{snapshot: Snapshot{Status: "ready", UpdatedAt: &now, Containers: []Container{{ID: "one", Name: "web"}}, DiskUsage: []DiskUsage{{Type: "Images", Size: "1GB"}}, Errors: map[string]string{}}}
	waitSnapshot(t, m, "ready")
	if b.calls.Load() != 1 {
		t.Fatal("concurrent queries not shared")
	}
	m.Latest(false)
	m.Latest(true)
	if b.calls.Load() != 1 {
		t.Fatal("cache or manual cooldown ignored")
	}
	m.mu.Lock()
	m.attempted = time.Now().Add(-time.Minute)
	m.mu.Unlock()
	m.Latest(false)
	b.results <- backendResult{err: errors.New("helper unavailable")}
	v := waitSnapshot(t, m, "failed")
	if len(v.Containers) != 1 || len(v.DiskUsage) != 1 || v.Message != "helper unavailable" || b.calls.Load() != 2 {
		t.Fatalf("cached result lost: %+v", v)
	}
	m.mu.Lock()
	m.attempted = time.Now().Add(-time.Minute)
	m.mu.Unlock()
	m.Latest(false)
	b.results <- backendResult{snapshot: Snapshot{Status: "partial", Containers: []Container{}, Errors: map[string]string{"disk_usage": "timeout"}, UpdatedAt: &now}}
	v = waitSnapshot(t, m, "partial")
	if v.Containers == nil || len(v.Containers) != 0 || len(v.DiskUsage) != 1 {
		t.Fatalf("partial refresh lost correct empty result or previous disk usage: %+v", v)
	}
}
