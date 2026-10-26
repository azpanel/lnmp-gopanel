package vhosts

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

type Usage struct {
	Path      string    `json:"path"`
	Bytes     *uint64   `json:"bytes"`
	Error     string    `json:"error,omitempty"`
	UpdatedAt time.Time `json:"updated_at"`
}

func (s Store) Usage(ctx context.Context, report func(Usage)) error {
	sites, e := s.List(ctx)
	if e != nil {
		return e
	}
	seen := map[string]bool{}
	resolvedCache := map[string]Usage{}
	for _, site := range sites {
		for _, root := range site.Roots {
			if root.Path == "" || seen[root.Path] {
				continue
			}
			seen[root.Path] = true
			u := Usage{Path: root.Path, UpdatedAt: time.Now()}
			resolved, e := filepath.EvalSymlinks(root.Path)
			if e != nil {
				u.Error = "站点目录不存在或不可访问"
				report(u)
				continue
			}
			info, e := os.Stat(resolved)
			if e != nil || !info.IsDir() {
				u.Error = "站点目录不存在或不是目录"
				report(u)
				continue
			}
			if cached, ok := resolvedCache[resolved]; ok {
				cached.Path = root.Path
				report(cached)
				continue
			}
			task, cancel := context.WithTimeout(ctx, 45*time.Second)
			cmd := exec.CommandContext(task, "/usr/bin/du", "-s", "-x", "-B1", "--null", "--", resolved)
			cmd.Env = []string{"PATH=/usr/bin:/bin", "LANG=C", "LC_ALL=C"}
			configureUsageProcess(cmd)
			output, e := cmd.Output()
			cancel()
			if e != nil {
				u.Error = "目录统计超时或部分内容不可读取"
			} else {
				size, _, ok := strings.Cut(string(output), "\t")
				if n, e := strconv.ParseUint(size, 10, 64); e == nil && ok {
					u.Bytes = &n
				} else {
					u.Error = "目录占用统计结果无效"
				}
			}
			resolvedCache[resolved] = u
			report(u)
			if ctx.Err() != nil {
				return errors.New("站点目录统计超时或已取消")
			}
		}
	}
	return nil
}
