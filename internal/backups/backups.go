// Package backups defines the website backup component installation contract.
package backups

import (
	"context"
	"errors"
	"time"
)

var ErrBusy = errors.New("已有组件安装任务正在运行")
var ErrInvalid = errors.New("组件安装参数无效")

func Names() []string            { return []string{"gdrive", "rclone"} }
func ValidName(name string) bool { return name == "gdrive" || name == "rclone" }

type Request struct {
	Components []string `json:"components"`
}

func (r Request) Validate() error {
	if len(r.Components) == 0 || len(r.Components) > 2 {
		return ErrInvalid
	}
	seen := map[string]bool{}
	for _, name := range r.Components {
		if !ValidName(name) || seen[name] {
			return ErrInvalid
		}
		seen[name] = true
	}
	return nil
}

type Item struct {
	Name      string `json:"name"`
	Status    string `json:"status"`
	Version   string `json:"version"`
	Supported bool   `json:"supported"`
	Error     string `json:"error,omitempty"`
}
type Result struct {
	Name    string `json:"name"`
	Status  string `json:"status"`
	Version string `json:"version,omitempty"`
	Error   string `json:"error,omitempty"`
}
type Job struct {
	ID        string    `json:"id"`
	Status    string    `json:"status"`
	Items     []Result  `json:"items"`
	Error     string    `json:"error,omitempty"`
	UpdatedAt time.Time `json:"updated_at"`
}
type Snapshot struct {
	Items []Item `json:"items"`
	Job   *Job   `json:"job,omitempty"`
}
type Backend interface {
	BackupComponents(context.Context) (Snapshot, error)
	InstallBackupComponents(context.Context, Request) (Job, error)
}

func Active(j *Job) bool { return j != nil && j.Status == "running" }
