package testutil

import (
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"testing"

	"github.com/azpanel/lnmp-gopanel/internal/passwordcheck"
)

var checkerOnce sync.Once
var checkerError error

// PasswordChecker builds the real scorer alongside the current test binary.
// go test removes that build directory after the package finishes. No test
// replacement scorer or dictionary dependency enters the panel's import graph.
func PasswordChecker(t testing.TB) string {
	t.Helper()
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(filepath.Dir(executable), passwordcheck.BinaryName)
	checkerOnce.Do(func() {
		root, err := os.Getwd()
		if err != nil {
			checkerError = err
			return
		}
		for {
			if _, err := os.Stat(filepath.Join(root, "go.mod")); err == nil {
				break
			}
			parent := filepath.Dir(root)
			if parent == root {
				checkerError = os.ErrNotExist
				return
			}
			root = parent
		}
		cmd := exec.Command("go", "build", "-o", path, "./cmd/passwordcheck")
		cmd.Dir = root
		cmd.Stdout, cmd.Stderr = os.Stderr, os.Stderr
		checkerError = cmd.Run()
	})
	if checkerError != nil {
		t.Fatalf("build password checker: %v", checkerError)
	}
	return path
}
