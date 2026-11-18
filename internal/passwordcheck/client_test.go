package passwordcheck

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func fakeChecker(t *testing.T, script string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), BinaryName)
	if err := os.WriteFile(path, []byte("#!/bin/sh\n"+script), 0700); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestScoreValidatesResponsesAndDoesNotLeakFailures(t *testing.T) {
	request := Request{Password: "N7!vQ2_zK9@pL5#sT8$xR4", Username: "admin"}
	t.Setenv("CHECKER_PRIVATE_TEST", request.Password)
	for _, tc := range []struct {
		name, script string
		valid        bool
	}{
		{"valid zero", `cat >/dev/null; printf '{"version":1,"score":0}'`, true},
		{"valid score", `[ "$#" -eq 0 ] && [ -z "$CHECKER_PRIVATE_TEST" ] || exit 1; cat >/dev/null; printf '{"version":1,"score":4}'`, true},
		{"missing score", `printf '{"version":1}'`, false},
		{"wrong version", `printf '{"version":2,"score":4}'`, false},
		{"negative score", `printf '{"version":1,"score":-1}'`, false},
		{"excessive score", `printf '{"version":1,"score":5}'`, false},
		{"null", `printf null`, false},
		{"trailing json", `printf '{"version":1,"score":4} {}'`, false},
		{"overflow", `head -c 129 /dev/zero`, false},
		{"failed", `cat >&2; printf '{"version":1,"score":4}'; exit 1`, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := score(context.Background(), fakeChecker(t, tc.script), request)
			if tc.valid {
				if err != nil {
					t.Fatal(err)
				}
			} else if !errors.Is(err, ErrUnavailable) || strings.Contains(err.Error(), request.Password) {
				t.Fatalf("failure not sanitized: %v", err)
			}
		})
	}
}

func TestScoreMissingProcessAndCancellationFailClosed(t *testing.T) {
	request := Request{Password: "N7!vQ2_zK9@pL5#sT8$xR4", Username: "admin"}
	if _, err := score(context.Background(), filepath.Join(t.TempDir(), "missing"), request); !errors.Is(err, ErrUnavailable) {
		t.Fatal("missing checker accepted")
	}
	for _, cancelEarly := range []bool{true, false} {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
		if cancelEarly {
			cancel()
		}
		_, err := score(ctx, fakeChecker(t, "exec sleep 5"), request)
		cancel()
		if !errors.Is(err, ErrUnavailable) {
			t.Fatal("cancelled checker accepted")
		}
	}
}
