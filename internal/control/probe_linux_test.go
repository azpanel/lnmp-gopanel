//go:build linux

package control

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestNativeProcessProbe(t *testing.T) {
	root := t.TempDir()
	program := filepath.Join(root, "nginx")
	if err := os.WriteFile(program, []byte("executable"), 0755); err != nil {
		t.Fatal(err)
	}
	proc := filepath.Join(root, "proc")
	_ = os.MkdirAll(filepath.Join(proc, "123"), 0755)
	if got := probeProcesses(context.Background(), proc, []string{program}); got.State != "stopped" {
		t.Fatal(got)
	}
	if err := os.Symlink(program, filepath.Join(proc, "123", "exe")); err != nil {
		t.Fatal(err)
	}
	if got := probeProcesses(context.Background(), proc, []string{program}); got.State != "running" {
		t.Fatal(got)
	}
	if got := probeProcesses(context.Background(), proc, []string{filepath.Join(root, "missing")}); got.State != "unknown" {
		t.Fatal(got)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if got := probeProcesses(ctx, proc, []string{program}); got.State != "unknown" {
		t.Fatal(got)
	}
}
