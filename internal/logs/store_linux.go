//go:build linux

package logs

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"syscall"

	"golang.org/x/sys/unix"
)

func fileIdentity(info os.FileInfo) string {
	stat := info.Sys().(*syscall.Stat_t)
	return fmt.Sprintf("%d:%d", stat.Dev, stat.Ino)
}

// Walk each component using directory descriptors and O_NOFOLLOW. O_NONBLOCK
// prevents a substituted FIFO from blocking before we can fstat it.
func openPath(path string, directory bool) (*os.File, error) {
	parent, err := unix.Open("/", unix.O_RDONLY|unix.O_DIRECTORY|unix.O_CLOEXEC, 0)
	if err != nil {
		return nil, err
	}
	parts := strings.Split(strings.TrimPrefix(path, "/"), "/")
	for i, part := range parts {
		flags := unix.O_RDONLY | unix.O_CLOEXEC | unix.O_NOFOLLOW | unix.O_NONBLOCK
		if i < len(parts)-1 || directory {
			flags |= unix.O_DIRECTORY
		}
		next, e := unix.Openat(parent, part, flags, 0)
		unix.Close(parent)
		if e != nil {
			return nil, e
		}
		parent = next
	}
	f := os.NewFile(uintptr(parent), path)
	if directory {
		return f, nil
	}
	info, err := f.Stat()
	if err != nil || !info.Mode().IsRegular() {
		f.Close()
		return nil, errors.New("日志不是普通文件或不可读取")
	}
	return f, nil
}

func openRegular(path string) (*os.File, error) { return openPath(path, false) }

type Store struct{}

func (Store) Read(ctx context.Context, req Request) (Window, error) {
	if err := Validate(req); err != nil {
		return Window{}, err
	}
	f, err := openRegular(req.Path)
	if err != nil {
		return Window{}, errors.New("日志不存在、权限不足或路径包含符号链接")
	}
	defer f.Close()
	return ReadFile(ctx, f, req)
}
func (Store) List(ctx context.Context) (Catalog, error) {
	out := Catalog{Files: []File{}, Warnings: []string{}}
	paths := []string{MySQL, PHP}
	// Listing never follows a substituted directory symlink.
	count := 0
	for _, directory := range []string{Directory, ManagedDirectory} {
		if count >= MaxFiles {
			break
		}
		dir, err := openPath(directory, true)
		if err != nil {
			if directory == Directory || !os.IsNotExist(err) {
				out.Warnings = append(out.Warnings, "无法列出 "+directory+"：目录不存在、权限不足或为符号链接")
			}
		} else {
			defer dir.Close()
			for {
				entries, e := dir.ReadDir(128)
				if err := ctx.Err(); err != nil {
					return out, err
				}
				for _, entry := range entries {
					if strings.HasSuffix(entry.Name(), ".log") && entry.Type().IsRegular() {
						paths = append(paths, filepath.Join(directory, entry.Name()))
						count++
						if count >= MaxFiles {
							break
						}
					}
				}
				if count >= MaxFiles {
					out.Warnings = append(out.Warnings, "网站日志超过 4096 个，仅列出前 4096 个")
					break
				}
				if e != nil {
					break
				}
			}
		}
	}
	for _, path := range paths {
		if err := ctx.Err(); err != nil {
			return out, err
		}
		item := File{Path: path}
		f, err := openRegular(path)
		if err != nil {
			item.Error = "文件不存在、权限不足或不是普通文件"
		} else {
			info, e := f.Stat()
			f.Close()
			if e != nil {
				item.Error = "无法读取文件信息"
			} else {
				item.Size, item.Modified, item.Available = info.Size(), info.ModTime(), true
			}
		}
		out.Files = append(out.Files, item)
	}
	return out, nil
}
