package main

import (
	"os"
	"testing"
)

func TestLoggingRejectsInvalidArguments(t *testing.T) {
	if os.Geteuid() != 0 {
		t.Skip("privileged helper argument checks")
	}
	original := os.Args
	t.Cleanup(func() { os.Args = original })
	for _, args := range [][]string{{"logging", "enable"}, {"logging", "enable-redis"}, {"logging", "enable-php;id"}, {"logging", "enable-php", "extra"}, {"logging", "status", "extra"}} {
		os.Args = append([]string{"lnmp-gopanel-control"}, args...)
		if err := run(); err == nil {
			t.Fatal("unsafe arguments accepted", args)
		}
	}
}
