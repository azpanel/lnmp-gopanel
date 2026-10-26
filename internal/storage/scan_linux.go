//go:build linux

package storage

import (
	"bufio"
	"context"
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/shirou/gopsutil/v4/disk"
)

// ScanFiles reads metadata only. There are no caller-supplied shell expressions;
// the privileged entrypoint always passes "/" as root. NUL records preserve
// spaces, tabs and newlines in file paths, unlike an awk whitespace pipeline.
func ScanFiles(ctx context.Context, root string, report func(Progress)) error {
	if _, err := os.Stat(root); err != nil {
		return errors.New("扫描目录不可读取")
	}
	mounts, err := disk.PartitionsWithContext(ctx, true)
	if err != nil {
		return errors.New("无法确认文件系统边界，未开始扫描")
	}
	cmd := exec.CommandContext(ctx, "/usr/bin/find", findArgs(root, mounts)...)
	cmd.Env = []string{"PATH=/usr/bin:/bin", "LANG=C", "LC_ALL=C"}
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Cancel = func() error {
		if cmd.Process == nil {
			return nil
		}
		return syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
	}
	cmd.WaitDelay = time.Second
	cmd.Stderr = io.Discard
	pipe, err := cmd.StdoutPipe()
	if err != nil {
		return errors.New("无法创建扫描管道")
	}
	if err := cmd.Start(); err != nil {
		return errors.New("无法启动扫描，请检查 find 是否已安装")
	}
	reader := bufio.NewReaderSize(pipe, 64*1024)
	top := topFiles{}
	progress := Progress{Files: []File{}}
	last := time.Now()
	var readErr error
	for {
		record, e := reader.ReadString(0)
		if e != nil {
			if e != io.EOF {
				readErr = e
			}
			break
		}
		file, e := parseRecord(strings.TrimSuffix(record, "\x00"))
		if e != nil {
			continue
		}
		progress.Scanned++
		progress.TotalBytes += uint64(file.Size)
		top.Add(file)
		if time.Since(last) >= 500*time.Millisecond {
			progress.Files = top.Sorted()
			report(progress)
			last = time.Now()
		}
	}
	waitErr := cmd.Wait()
	progress.Files = top.Sorted()
	progress.Complete = true
	progress.Partial = waitErr != nil || readErr != nil
	report(progress)
	if ctx.Err() != nil {
		return errors.New("扫描已超时或取消，保留已扫描结果")
	}
	// find can exit 1 when a file vanishes during the walk. Preserve useful data,
	// marking it partial rather than claiming the inventory is complete.
	if waitErr != nil {
		var exit *exec.ExitError
		if !errors.As(waitErr, &exit) || exit.ExitCode() != 1 {
			return errors.New("扫描进程异常退出，请重新扫描")
		}
	}
	if readErr != nil {
		return errors.New("读取扫描结果中断，请重新扫描")
	}
	return nil
}
func parseRecord(record string) (File, error) {
	size, path, ok := strings.Cut(record, " ")
	if !ok || path == "" {
		return File{}, errors.New("invalid file record")
	}
	n, err := strconv.ParseInt(size, 10, 64)
	if err != nil || n < 0 {
		return File{}, errors.New("invalid file size")
	}
	return File{Path: path, Size: n}, nil
}
func findArgs(root string, mounts []disk.PartitionStat) []string {
	root = filepath.Clean(root)
	args := []string{root, "-xdev"}
	exclusions := []string{}
	seen := map[string]bool{}
	for _, m := range mounts {
		path := filepath.Clean(m.Mountpoint)
		relative, err := filepath.Rel(root, path)
		if err != nil || relative == "." || relative == ".." || strings.HasPrefix(relative, "../") || seen[path] {
			continue
		}
		seen[path] = true
		exclusions = append(exclusions, path)
	}
	if len(exclusions) > 0 {
		args = append(args, "(")
		for i, p := range exclusions {
			if i > 0 {
				args = append(args, "-o")
			}
			escaped := strings.NewReplacer("\\", "\\\\", "*", "\\*", "?", "\\?", "[", "\\[", "]", "\\]").Replace(p)
			args = append(args, "-path", escaped)
		}
		args = append(args, ")", "-prune", "-o")
	}
	return append(args, "-type", "f", "-printf", "%s %p\\000")
}
