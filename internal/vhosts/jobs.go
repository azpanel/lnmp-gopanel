package vhosts

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"
	"unicode/utf8"
)

var ErrJobBusy = errors.New("另一个建站或证书任务正在执行，请稍后再试")

type JobLine struct {
	ID    uint64    `json:"id"`
	Time  time.Time `json:"time"`
	Phase string    `json:"phase"`
	Text  string    `json:"text"`
}
type Job struct {
	ID        string        `json:"id"`
	Name      string        `json:"name"`
	Status    string        `json:"status"`
	Phase     string        `json:"phase"`
	CreatedAt time.Time     `json:"created_at"`
	UpdatedAt time.Time     `json:"updated_at"`
	Lines     []JobLine     `json:"lines"`
	Result    *CreateResult `json:"result,omitempty"`
	Error     string        `json:"error,omitempty"`
}
type Jobs struct {
	mu         sync.RWMutex
	ctx        context.Context
	backend    ProvisionBackend
	jobs       map[string]*Job
	order      []string
	active     string
	onComplete func()
}

func NewJobs(ctx context.Context, b ProvisionBackend, complete func()) *Jobs {
	return &Jobs{ctx: ctx, backend: b, jobs: map[string]*Job{}, onComplete: complete}
}
func (m *Jobs) Options(ctx context.Context) (CreateOptions, error) {
	return m.backend.VhostOptions(ctx)
}
func cloneJob(j *Job) Job {
	copy := *j
	copy.Lines = append([]JobLine{}, j.Lines...)
	if j.Result != nil {
		r := *j.Result
		copy.Result = &r
	}
	return copy
}
func (m *Jobs) List() []Job {
	m.mu.RLock()
	defer m.mu.RUnlock()
	out := []Job{}
	for i := len(m.order) - 1; i >= 0; i-- {
		job := *m.jobs[m.order[i]]
		job.Lines = []JobLine{}
		if job.Result != nil {
			result := *job.Result
			job.Result = &result
		}
		out = append(out, job)
	}
	return out
}
func (m *Jobs) Get(id string) (Job, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	j, ok := m.jobs[id]
	if !ok {
		return Job{}, false
	}
	return cloneJob(j), true
}
func (m *Jobs) Start(r CreateRequest) (Job, error) {
	if err := r.Validate(); err != nil {
		return Job{}, err
	}
	m.mu.Lock()
	if j, ok := m.jobs[r.RequestID]; ok {
		out := cloneJob(j)
		m.mu.Unlock()
		return out, nil
	}
	if m.active != "" {
		m.mu.Unlock()
		return Job{}, ErrJobBusy
	}
	now := time.Now()
	j := &Job{ID: r.RequestID, Name: r.Name, Status: "running", Phase: "preflight", CreatedAt: now, UpdatedAt: now, Lines: []JobLine{}}
	m.jobs[j.ID] = j
	m.order = append(m.order, j.ID)
	m.active = j.ID
	for len(m.order) > 20 {
		delete(m.jobs, m.order[0])
		m.order = m.order[1:]
	}
	out := cloneJob(j)
	m.mu.Unlock()
	go m.run(r)
	return out, nil
}
func (m *Jobs) run(r CreateRequest) {
	ctx, cancel := context.WithTimeout(m.ctx, CreateTimeout)
	defer cancel()
	secrets := []string{r.Database.Password, r.Database.AdminPassword}
	inPrivateKey := false
	redact := func(line string) string {
		for _, secret := range secrets {
			if secret != "" {
				line = strings.ReplaceAll(line, secret, "[已隐藏]")
			}
		}
		upper := strings.ToUpper(line)
		if strings.Contains(upper, "BEGIN ") && strings.Contains(upper, "PRIVATE KEY") {
			inPrivateKey = true
		}
		if inPrivateKey {
			if strings.Contains(upper, "END ") && strings.Contains(upper, "PRIVATE KEY") {
				inPrivateKey = false
			}
			return "[私钥输出已隐藏]"
		}
		for _, marker := range []string{"PRIVATE KEY", "HMAC", "API_KEY", "ACCOUNT_KEY", "ACCOUNT_JSON", "SAVED_", "TOKEN=", "PASSWORD=", "SECRET="} {
			if strings.Contains(upper, marker) {
				return "[敏感输出已隐藏]"
			}
		}
		line = strings.ToValidUTF8(line, "")
		if len(line) > 4096 {
			line = line[:4096]
			for !utf8.ValidString(line) {
				line = line[:len(line)-1]
			}
			line += "…"
		}
		return strings.Map(func(c rune) rune {
			if c == '\t' || c >= 32 && c != 127 {
				return c
			}
			return -1
		}, line)
	}
	var seq uint64
	result, err := m.backend.CreateVhost(ctx, r, func(e CreateEvent) {
		m.mu.Lock()
		defer m.mu.Unlock()
		j := m.jobs[r.RequestID]
		j.UpdatedAt = time.Now()
		if e.Phase != "" {
			j.Phase = e.Phase
		}
		if e.Result != nil {
			copy := *e.Result
			j.Result = &copy
		}
		if e.Line != "" {
			seq++
			j.Lines = append(j.Lines, JobLine{ID: seq, Time: time.Now(), Phase: j.Phase, Text: redact(e.Line)})
			if len(j.Lines) > 1000 {
				j.Lines = j.Lines[len(j.Lines)-1000:]
			}
		}
	})
	if m.onComplete != nil {
		m.onComplete()
	}
	m.mu.Lock()
	j := m.jobs[r.RequestID]
	j.Result = &result
	j.Status = "succeeded"
	j.Phase = "done"
	j.UpdatedAt = time.Now()
	if err != nil {
		j.Status = "failed"
		if result.SiteCreated {
			j.Status = "partial"
		}
		j.Error = redact(err.Error())
		if ctx.Err() != nil {
			j.Error = fmt.Sprintf("任务超时或面板停止，请刷新站点确认实际状态：%s", j.Error)
		}
	}
	m.active = ""
	m.mu.Unlock()
}
