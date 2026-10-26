package main

import (
	"os"
	"testing"
)

func TestComponentsRejectOtherArguments(t *testing.T) {
	original := os.Args
	t.Cleanup(func() { os.Args = original })
	for _, args := range [][]string{
		{"components"}, {"components", "version"}, {"components", "snapshot", "node"},
		{"components", "snapshot", "/tmp/node"}, {"node", "start"}, {"codex", "version"},
	} {
		os.Args = append([]string{"lnmp-gopanel-control"}, args...)
		if err := run(); err == nil {
			t.Fatalf("accepted %v", args)
		}
	}
}
