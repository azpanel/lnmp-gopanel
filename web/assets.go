//go:build !embed

package web

import (
	"io/fs"
	"os"
)

// During development run from the repository root after npm run build.
func Assets() fs.FS { return os.DirFS("web/dist") }
