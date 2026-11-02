package backups

import (
	"errors"
	"time"
)

var ErrRunBusy = errors.New("该政策已有备份正在排队或执行，请等待完成")

// Run records execution only; policy configuration and credentials are never
// embedded in public responses.
type Run struct {
	ID           string           `json:"id"`
	PolicyID     string           `json:"policy_id"`
	PolicyName   string           `json:"policy_name"`
	Revision     uint64           `json:"revision"`
	Trigger      string           `json:"trigger"`
	Status       string           `json:"status"`
	Phase        string           `json:"phase"`
	CreatedAt    time.Time        `json:"created_at"`
	StartedAt    *time.Time       `json:"started_at,omitempty"`
	FinishedAt   *time.Time       `json:"finished_at,omitempty"`
	UpdatedAt    time.Time        `json:"updated_at"`
	ScheduledFor *time.Time       `json:"scheduled_for,omitempty"`
	Error        string           `json:"error,omitempty"`
	Categories   []CategoryResult `json:"categories"`
}

type CategoryResult struct {
	Category   string     `json:"category"`
	Status     string     `json:"status"`
	Error      string     `json:"error,omitempty"`
	Name       string     `json:"name,omitempty"`
	Bytes      int64      `json:"bytes"`
	SHA256     string     `json:"sha256,omitempty"`
	Deliveries []Delivery `json:"deliveries"`
}

type Delivery struct {
	Destination  Destination `json:"destination"`
	Status       string      `json:"status"`
	Object       string      `json:"object,omitempty"`
	Error        string      `json:"error,omitempty"`
	CleanupError string      `json:"cleanup_error,omitempty"`
}

type RunQuery struct {
	ID   string `json:"id"`
	Page int    `json:"page"`
}
type RunList struct {
	Items    []Run `json:"items"`
	Total    int   `json:"total"`
	Page     int   `json:"page"`
	PageSize int   `json:"page_size"`
}
type Dependency struct {
	Name  string `json:"name"`
	Ready bool   `json:"ready"`
	Error string `json:"error,omitempty"`
}
type Runtime struct {
	Scheduler      bool         `json:"scheduler"`
	SchedulerError string       `json:"scheduler_error,omitempty"`
	Dependencies   []Dependency `json:"dependencies"`
}

func ActiveRun(status string) bool { return status == "queued" || status == "running" }
