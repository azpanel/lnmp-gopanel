package main

import (
	"errors"
	"github.com/azpanel/lnmp-gopanel/internal/logs"
	"os"
	"strings"
	"testing"
)

func TestDecodeLogRequest(t *testing.T) {
	valid := `{"path":"/home/wwwlogs/example.log","mode":"tail","rules":[]}`
	if req, err := decodeLogRequest(strings.NewReader(valid)); err != nil || req.Path != "/home/wwwlogs/example.log" {
		t.Fatal(req, err)
	}
	for _, raw := range []string{valid + `{}`, valid + strings.Repeat(" ", 4096), `{"path":"/etc/shadow","mode":"tail"}`, `{"path":"/srv/app/.env","mode":"stat"}`, `{"path":"/srv/app/.ssh/id_rsa","mode":"tail"}`, `{"path":"/var/lib/mysql/table.ibd","mode":"tail"}`, `{"path":"/srv/app/secret.pem","mode":"stat"}`, `{"path":"/home/wwwlogs/a.log","mode":"tail","command":"cat"}`, `{"path":"/home/wwwlogs/a.log","mode":"after","end":"bad"}`} {
		if _, err := decodeLogRequest(strings.NewReader(raw)); !errors.Is(err, logs.ErrInvalid) {
			t.Fatal("invalid stdin accepted", err)
		}
	}
}
func TestLogsRejectOtherArguments(t *testing.T) {
	original := os.Args
	t.Cleanup(func() { os.Args = original })
	for _, args := range [][]string{{"logs"}, {"logs", "delete"}, {"logs", "read", "/etc/shadow"}, {"logs", "list", "*"}} {
		os.Args = append([]string{"control"}, args...)
		if err := run(); err == nil {
			t.Fatal("unexpectedly accepted", args)
		}
	}
}

func TestDecodeLogRequestExtensionRestriction(t *testing.T) {
	for i, name := range []string{"app.log", "app.txt", "app.LOG", "app.TxT", "syslog", "database.php", "app.log.1", "app.txt.bak"} {
		for _, mode := range []string{"stat", "tail"} {
			raw := `{"path":"/srv/` + name + `","mode":"` + mode + `"}`
			_, err := decodeLogRequest(strings.NewReader(raw))
			if i < 4 {
				if err != nil {
					t.Fatal("valid extension rejected", name, err)
				}
			} else if !errors.Is(err, logs.ErrInvalid) {
				t.Fatal("invalid extension accepted", name, err)
			}
		}
	}
}
