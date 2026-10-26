package control

import (
	"context"
	"errors"
	"os/exec"
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestReadRootCrontab(t *testing.T) {
	exitOne := exec.Command("/bin/sh", "-c", "exit 1").Run()
	for _, tc := range []struct {
		name, out, stderr string
		err               error
		wantError         bool
		count             int
	}{
		{name: "tasks", out: "0 2 * * * /root/backup\n", count: 1},
		{name: "empty"},
		{name: "not configured", stderr: "no crontab for root\n", err: exitOne},
		{name: "permission", stderr: "permission denied", err: exitOne, wantError: true},
		{name: "missing", err: exec.ErrNotFound, wantError: true},
		{name: "unknown failure", err: exitOne, wantError: true},
		{name: "diagnostic", out: "@daily /root/backup\n", stderr: "warning", count: 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			view, err := readRootCrontab(context.Background(), "/usr/bin/crontab", func(_ context.Context, program string, args ...string) (string, string, error) {
				if program != "/usr/bin/crontab" || !reflect.DeepEqual(args, []string{"-u", "root", "-l"}) {
					t.Fatalf("unexpected command: %s %v", program, args)
				}
				return tc.out, tc.stderr, tc.err
			})
			if (err != nil) != tc.wantError || len(view.Tasks) != tc.count {
				t.Fatalf("view=%+v err=%v", view, err)
			}
			if tc.name == "diagnostic" && len(view.Warnings) != 1 {
				t.Fatal(view.Warnings)
			}
		})
	}
}

func TestCronCommandLimitsAndStreams(t *testing.T) {
	stdout, stderr, err := cronCommand(context.Background(), 8, "/bin/sh", "-c", "printf task; printf warning >&2")
	if err != nil || stdout != "task" || stderr != "warning" {
		t.Fatalf("stdout=%q stderr=%q err=%v", stdout, stderr, err)
	}
	for _, script := range []string{"printf 123456789", "head -c 16385 /dev/zero >&2"} {
		stdout, stderr, err = cronCommand(context.Background(), 8, "/bin/sh", "-c", script)
		if err == nil || stdout != "" || stderr != "" || !strings.Contains(err.Error(), "过大") {
			t.Fatalf("truncated data escaped: %q %q %v", stdout, stderr, err)
		}
	}
}

func TestCronCommandTimeout(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
	defer cancel()
	_, _, err := cronCommand(ctx, 8, "/bin/sh", "-c", "sleep 5")
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("expected timeout: %v", err)
	}
	_, err = readRootCrontab(ctx, "/usr/bin/crontab", func(context.Context, string, ...string) (string, string, error) {
		return "", "no crontab for root", exec.Command("/bin/sh", "-c", "exit 1").Run()
	})
	if err == nil || !strings.Contains(err.Error(), "超时") {
		t.Fatalf("timeout treated as empty: %v", err)
	}
}
