//go:build linux

package control

import (
	"fmt"
	"os"
	"path/filepath"
	"syscall"
)

// Check every ancestor as well as the resolved target: an unprivileged user must
// not be able to replace either the config or an executable via its directory.
func TrustedPath(path string) error {
	if !filepath.IsAbs(path) {
		return fmt.Errorf("path must be absolute")
	}
	resolved, err := filepath.EvalSymlinks(path)
	if err != nil {
		return err
	}
	for _, start := range []string{filepath.Clean(path), resolved} {
		for p := start; ; p = filepath.Dir(p) {
			info, err := os.Stat(p)
			if err != nil {
				return err
			}
			st, ok := info.Sys().(*syscall.Stat_t)
			if !ok || st.Uid != 0 || info.Mode().Perm()&0022 != 0 {
				return fmt.Errorf("untrusted root-owned path: %s", p)
			}
			if p == "/" {
				break
			}
		}
	}
	return nil
}
