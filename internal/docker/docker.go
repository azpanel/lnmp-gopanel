package docker

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"strings"
	"sync"
	"time"
)

const Timeout = 28 * time.Second

type Container struct {
	ID         string `json:"id"`
	Name       string `json:"name"`
	Image      string `json:"image"`
	Command    string `json:"command"`
	CreatedAt  string `json:"created_at"`
	RunningFor string `json:"running_for"`
	State      string `json:"state"`
	Status     string `json:"status"`
	Ports      string `json:"ports"`
	Size       string `json:"size"`
}

type DiskUsage struct {
	Type        string `json:"type"`
	Total       int64  `json:"total"`
	Active      int64  `json:"active"`
	Size        string `json:"size"`
	Reclaimable string `json:"reclaimable"`
}

type Snapshot struct {
	Status     string            `json:"status"`
	Containers []Container       `json:"containers"`
	DiskUsage  []DiskUsage       `json:"disk_usage"`
	UpdatedAt  *time.Time        `json:"updated_at"`
	Message    string            `json:"message,omitempty"`
	Errors     map[string]string `json:"errors"`
}

type Runner func(context.Context, ...string) (string, error)

// Collect accepts no user-supplied Docker arguments. Both commands are read-only
// and explicitly target the local system daemon, independent of CLI contexts.
func Collect(ctx context.Context, run Runner) Snapshot {
	result := Snapshot{Errors: map[string]string{}}
	output, err := command(ctx, run, "ps", "--size", "--no-trunc", "--format", "{{json .}}")
	if err == nil {
		result.Containers, err = parseContainers(output)
	}
	if err != nil {
		result.Errors["containers"] = "无法读取容器列表，请检查 Docker 服务是否运行"
	}
	output, err = command(ctx, run, "system", "df", "--format", "{{json .}}")
	if err == nil {
		result.DiskUsage, err = parseDiskUsage(output)
	}
	if err != nil {
		result.Errors["disk_usage"] = "Docker 空间统计失败或超时，请稍后刷新"
	}
	result.Status = "ready"
	if len(result.Errors) == 2 {
		result.Status = "failed"
	} else {
		now := time.Now()
		result.UpdatedAt = &now
		if len(result.Errors) > 0 {
			result.Status = "partial"
		}
	}
	return result
}

func command(ctx context.Context, run Runner, args ...string) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, 12*time.Second)
	defer cancel()
	return run(ctx, append([]string{"--host", "unix:///var/run/docker.sock"}, args...)...)
}

func parseRows[T any](output string, consume func(T) error) error {
	decoder := json.NewDecoder(strings.NewReader(output))
	for count := 0; ; count++ {
		var row T
		err := decoder.Decode(&row)
		if err == io.EOF {
			return nil
		}
		if err != nil {
			return err
		}
		if count >= 10000 {
			return errors.New("Docker 返回条目过多")
		}
		if err := consume(row); err != nil {
			return err
		}
	}
}

func parseContainers(output string) ([]Container, error) {
	var rows = []Container{}
	type entry struct{ ID, Names, Image, Command, CreatedAt, RunningFor, State, Status, Ports, Size string }
	err := parseRows(output, func(row entry) error {
		if row.ID == "" || row.Names == "" {
			return errors.New("容器信息不完整")
		}
		rows = append(rows, Container{ID: row.ID, Name: row.Names, Image: row.Image, Command: row.Command, CreatedAt: row.CreatedAt, RunningFor: row.RunningFor, State: row.State, Status: row.Status, Ports: row.Ports, Size: row.Size})
		return nil
	})
	if err != nil {
		return nil, err
	}
	return rows, nil
}

func parseDiskUsage(output string) ([]DiskUsage, error) {
	var rows = []DiskUsage{}
	type entry struct {
		Type, Size, Reclaimable string
		TotalCount, Active      json.Number
	}
	err := parseRows(output, func(row entry) error {
		total, totalErr := row.TotalCount.Int64()
		active, activeErr := row.Active.Int64()
		if row.Type == "" || row.Size == "" || totalErr != nil || activeErr != nil || total < 0 || active < 0 {
			return errors.New("空间信息不完整")
		}
		rows = append(rows, DiskUsage{Type: row.Type, Total: total, Active: active, Size: row.Size, Reclaimable: row.Reclaimable})
		return nil
	})
	if err != nil {
		return nil, err
	}
	if len(rows) == 0 {
		return nil, errors.New("空间信息为空")
	}
	return rows, nil
}

type Backend interface {
	Docker(context.Context) (Snapshot, error)
}

type Manager struct {
	mu        sync.Mutex
	ctx       context.Context
	backend   Backend
	latest    Snapshot
	attempted time.Time
}

func NewManager(ctx context.Context, backend Backend) *Manager {
	return &Manager{ctx: ctx, backend: backend, latest: Snapshot{Status: "idle", Errors: map[string]string{}}}
}

func (m *Manager) Latest(force bool) Snapshot {
	m.mu.Lock()
	defer m.mu.Unlock()
	ttl := 30 * time.Second
	if force {
		ttl = 5 * time.Second
	}
	if m.latest.Status != "loading" && time.Since(m.attempted) >= ttl && m.ctx.Err() == nil {
		m.attempted = time.Now()
		m.latest.Status = "loading"
		go m.collect()
	}
	return m.latest
}

func (m *Manager) collect() {
	ctx, cancel := context.WithTimeout(m.ctx, Timeout+2*time.Second)
	defer cancel()
	result, err := m.backend.Docker(ctx)
	m.mu.Lock()
	defer m.mu.Unlock()
	if err != nil {
		m.latest.Status = "failed"
		m.latest.Message = err.Error()
		return
	}
	// Preserve useful previous data if only one part of a refresh failed.
	if result.Status != "unavailable" {
		if result.Containers == nil {
			result.Containers = m.latest.Containers
		}
		if result.DiskUsage == nil {
			result.DiskUsage = m.latest.DiskUsage
		}
		if result.UpdatedAt == nil {
			result.UpdatedAt = m.latest.UpdatedAt
		}
	}
	m.latest = result
}
