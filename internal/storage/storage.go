package storage

import (
	"container/heap"
	"context"
	"errors"
	"sort"
	"sync"
	"time"

	"github.com/shirou/gopsutil/v4/disk"
)

const Limit = 30
const ScanTimeout = 5 * time.Minute

type File struct {
	Path string `json:"path"`
	Size int64  `json:"size"`
}
type Progress struct {
	Files      []File `json:"files"`
	Scanned    uint64 `json:"scanned"`
	TotalBytes uint64 `json:"total_bytes"`
	Complete   bool   `json:"complete"`
	Partial    bool   `json:"partial"`
}
type Job struct {
	Status     string     `json:"status"`
	Root       string     `json:"root"`
	StartedAt  *time.Time `json:"started_at"`
	FinishedAt *time.Time `json:"finished_at"`
	Message    string     `json:"message"`
	Progress
}
type Scanner interface {
	Scan(context.Context, func(Progress)) error
}
type Manager struct {
	mu      sync.RWMutex
	ctx     context.Context
	scanner Scanner
	job     Job
}

func NewManager(ctx context.Context, scanner Scanner) *Manager {
	return &Manager{ctx: ctx, scanner: scanner, job: Job{Status: "idle", Root: "/", Progress: Progress{Files: []File{}}}}
}
func (m *Manager) Latest() Job { m.mu.RLock(); defer m.mu.RUnlock(); return m.job }
func (m *Manager) Start() Job {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.job.Status == "running" {
		return m.job
	}
	now := time.Now()
	m.job = Job{Status: "running", Root: "/", StartedAt: &now, Message: "正在扫描主文件系统…", Progress: Progress{Files: []File{}}}
	go m.run()
	return m.job
}
func (m *Manager) run() {
	ctx, cancel := context.WithTimeout(m.ctx, ScanTimeout+5*time.Second)
	defer cancel()
	err := m.scanner.Scan(ctx, func(p Progress) {
		p.Files = append([]File{}, p.Files...)
		m.mu.Lock()
		defer m.mu.Unlock()
		m.job.Progress = p
	})
	m.mu.Lock()
	defer m.mu.Unlock()
	now := time.Now()
	m.job.FinishedAt = &now
	switch {
	case err != nil:
		m.job.Status = "failed"
		m.job.Message = err.Error()
		if len(m.job.Files) > 0 {
			m.job.Status = "partial"
			m.job.Partial = true
		}
	case !m.job.Complete:
		m.job.Status = "failed"
		m.job.Message = "扫描中断，请重新扫描"
	case m.job.Partial:
		m.job.Status = "partial"
		m.job.Message = "部分文件无法访问或在扫描中发生变化，当前结果可能不完整"
	default:
		m.job.Status = "completed"
		m.job.Message = "扫描完成"
	}
}

type Mount struct {
	Path    string   `json:"path"`
	Device  string   `json:"device"`
	FSType  string   `json:"fs_type"`
	Options []string `json:"options"`
	Total   *uint64  `json:"total"`
	Used    *uint64  `json:"used"`
	Free    *uint64  `json:"free"`
	Percent *float64 `json:"percent"`
	Error   string   `json:"error,omitempty"`
}
type MountList struct {
	Timestamp time.Time `json:"timestamp"`
	Mounts    []Mount   `json:"mounts"`
}

func Mounts(ctx context.Context) (MountList, error) {
	result := MountList{Timestamp: time.Now(), Mounts: []Mount{}}
	partitions, err := disk.PartitionsWithContext(ctx, true)
	if err != nil {
		return result, errors.New("挂载列表读取失败")
	}
	for _, p := range partitions {
		mount := Mount{Path: p.Mountpoint, Device: p.Device, FSType: p.Fstype, Options: p.Opts}
		if u, err := disk.UsageWithContext(ctx, p.Mountpoint); err == nil {
			mount.Total = &u.Total
			mount.Used = &u.Used
			mount.Free = &u.Free
			mount.Percent = &u.UsedPercent
		} else {
			mount.Error = "容量暂不可读取"
		}
		result.Mounts = append(result.Mounts, mount)
	}
	sort.SliceStable(result.Mounts, func(i, j int) bool {
		a, b := result.Mounts[i], result.Mounts[j]
		if a.Path == "/" && b.Path != "/" {
			return true
		}
		if b.Path == "/" && a.Path != "/" {
			return false
		}
		return a.Path < b.Path
	})
	return result, nil
}

// The heap retains only the largest files; it never sorts or stores the full
// filesystem inventory. Equal-size paths have a deterministic order.
type topFiles []File

func (h topFiles) Len() int { return len(h) }
func (h topFiles) Less(i, j int) bool {
	if h[i].Size == h[j].Size {
		return h[i].Path > h[j].Path
	}
	return h[i].Size < h[j].Size
}
func (h topFiles) Swap(i, j int) { h[i], h[j] = h[j], h[i] }
func (h *topFiles) Push(x any)   { *h = append(*h, x.(File)) }
func (h *topFiles) Pop() any     { old := *h; v := old[len(old)-1]; *h = old[:len(old)-1]; return v }
func (h *topFiles) Add(f File) {
	if len(*h) < Limit {
		heap.Push(h, f)
		return
	}
	smallest := (*h)[0]
	if f.Size > smallest.Size || (f.Size == smallest.Size && f.Path < smallest.Path) {
		(*h)[0] = f
		heap.Fix(h, 0)
	}
}
func (h topFiles) Sorted() []File {
	files := append([]File{}, h...)
	sort.Slice(files, func(i, j int) bool {
		if files[i].Size == files[j].Size {
			return files[i].Path < files[j].Path
		}
		return files[i].Size > files[j].Size
	})
	return files
}
