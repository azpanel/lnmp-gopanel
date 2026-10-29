package main

import (
	"os"
	"strings"
	"testing"
)

func TestNginxManagementRejectsInvalidArguments(t *testing.T) {
	if os.Geteuid() != 0 {
		t.Skip("privileged helper argument checks")
	}
	original := os.Args
	t.Cleanup(func() { os.Args = original })
	for _, args := range [][]string{{"nginx", "repair", "extra"}, {"nginx", "repair-preview", "extra"}, {"php-fpm", "repair"}, {"nginx", "repair;id"}} {
		os.Args = append([]string{"lnmp-gopanel-control"}, args...)
		if err := run(); err == nil {
			t.Fatal("unsafe arguments accepted", args)
		}
	}
}

func TestNginxManagementRejectsInvalidRequestsBeforeAccessingServices(t *testing.T) {
	original := os.Stdin
	t.Cleanup(func() { os.Stdin = original })
	valid := `{"revision":"` + strings.Repeat("a", 64) + `"}`
	for _, body := range []string{`{}`, `{"revision":"short"}`, valid + `{}`, `{"revision":"` + strings.Repeat("a", 64) + `","program":"/bin/sh"}`, valid + strings.Repeat(" ", 1025)} {
		file, err := os.CreateTemp(t.TempDir(), "request")
		if err != nil {
			t.Fatal(err)
		}
		if _, err := file.WriteString(body); err != nil {
			t.Fatal(err)
		}
		if _, err := file.Seek(0, 0); err != nil {
			t.Fatal(err)
		}
		os.Stdin = file
		if err := handleNginxManagement("repair"); err == nil {
			t.Fatal("invalid request accepted")
		}
		file.Close()
	}
}
