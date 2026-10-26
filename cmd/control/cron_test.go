package main

import (
	"os"
	"testing"
)

func TestCronRejectsOtherArguments(t *testing.T) {
	original := os.Args
	t.Cleanup(func() { os.Args = original })
	for _, args := range [][]string{
		{"cron"},
		{"cron", "edit"},
		{"cron", "remove"},
		{"cron", "list", "root"},
		{"cron", "list", "--user=other"},
		{"cron", "list", "; touch /tmp/unwanted"},
	} {
		os.Args = append([]string{"lnmp-gopanel-control"}, args...)
		if err := run(); err == nil {
			t.Fatalf("unexpectedly accepted arguments: %v", args)
		}
	}
}
