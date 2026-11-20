package main

import (
	"os"
	"testing"
)

func TestPHPConfigRejectsInvalidArguments(t *testing.T) {
	if os.Geteuid() != 0 {
		t.Skip("privileged helper argument checks")
	}
	original := os.Args
	t.Cleanup(func() { os.Args = original })
	for _, args := range [][]string{
		{"php-config", "delete"}, {"php-config", "get", "extra"}, {"php-config", "update", "extra"}, {"php-config", "update;id"},
		{"php-fpm-launch", "-c"}, {"php-fpm-launch", "foreground", "-d", "memory_limit=64M"},
	} {
		os.Args = append([]string{"lnmp-gopanel-control"}, args...)
		if err := run(); err == nil {
			t.Fatal("unsafe arguments accepted", args)
		}
	}
}
