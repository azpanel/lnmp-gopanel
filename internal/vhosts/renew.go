package vhosts

import (
	"context"
	"errors"
	"fmt"
	"time"
)

type RenewItem struct {
	Name     string `json:"name"`
	Provider string `json:"provider"`
	Email    string `json:"email"`
	Status   string `json:"status,omitempty"`
	Phase    string `json:"phase,omitempty"`
	Error    string `json:"error,omitempty"`
}
type RenewRequest struct {
	RequestID string      `json:"request_id"`
	Items     []RenewItem `json:"items"`
}
type RenewPreview struct {
	Name     string   `json:"name"`
	Domains  []string `json:"domains"`
	Provider string   `json:"provider"`
	Email    string   `json:"email"`
	Eligible bool     `json:"eligible"`
	Reason   string   `json:"reason,omitempty"`
}
type RenewBackend interface {
	PreviewRenewal(context.Context, []string) ([]RenewPreview, error)
	RenewVhost(context.Context, string, RenewItem, func(CreateEvent)) error
}

func ValidateRenewNames(names []string) error {
	if len(names) == 0 || len(names) > 1000 {
		return errors.New("请选择 1–1000 个站点")
	}
	seen := map[string]bool{}
	for _, name := range names {
		if !ValidName(name) || seen[name] {
			return errors.New("站点名称无效或重复")
		}
		seen[name] = true
	}
	return nil
}
func (r RenewRequest) Validate() error {
	if !safeID.MatchString(r.RequestID) {
		return errors.New("提交标识无效")
	}
	names := make([]string, 0, len(r.Items))
	for _, item := range r.Items {
		names = append(names, item.Name)
		if item.Provider != "letsencrypt" && item.Provider != "zerossl" {
			return errors.New("签发机构无效")
		}
		if err := ValidateSSL(SSLInput{Mode: item.Provider, Email: item.Email}); err != nil {
			return err
		}
	}
	return ValidateRenewNames(names)
}
func (m *Jobs) PreviewRenewal(ctx context.Context, names []string) ([]RenewPreview, error) {
	if err := ValidateRenewNames(names); err != nil {
		return nil, err
	}
	backend, ok := m.backend.(RenewBackend)
	if !ok {
		return nil, errors.New("辅助程序不支持续签，请升级")
	}
	return backend.PreviewRenewal(ctx, names)
}
func (m *Jobs) StartRenewal(r RenewRequest) (Job, error) {
	if err := r.Validate(); err != nil {
		return Job{}, err
	}
	backend, ok := m.backend.(RenewBackend)
	if !ok {
		return Job{}, errors.New("辅助程序不支持续签，请升级")
	}
	m.mu.Lock()
	if j, ok := m.jobs[r.RequestID]; ok {
		out := cloneJob(j)
		m.mu.Unlock()
		if out.Kind != "ssl-renew" {
			return Job{}, errors.New("提交标识已用于其他任务")
		}
		return out, nil
	}
	if err := m.ctx.Err(); err != nil {
		m.mu.Unlock()
		return Job{}, err
	}
	if m.active != "" {
		m.mu.Unlock()
		return Job{}, ErrJobBusy
	}
	now := time.Now()
	items := append([]RenewItem{}, r.Items...)
	for i := range items {
		items[i].Status = "queued"
		items[i].Phase = ""
		items[i].Error = ""
	}
	j := &Job{ID: r.RequestID, Kind: "ssl-renew", Name: fmt.Sprintf("批量续签 SSL · %d 个站点", len(items)), Status: "running", Phase: "preflight", CreatedAt: now, UpdatedAt: now, Lines: []JobLine{}, Items: items}
	m.remember(j)
	out := cloneJob(j)
	m.mu.Unlock()
	go m.runRenewal(r.RequestID, backend)
	return out, nil
}
func (m *Jobs) runRenewal(id string, backend RenewBackend) {
	var seq uint64
	for index := 0; ; index++ {
		m.mu.Lock()
		j := m.jobs[id]
		if m.ctx.Err() != nil && index < len(j.Items) {
			for i := index; i < len(j.Items); i++ {
				j.Items[i].Status = "canceled"
				j.Items[i].Phase = ""
				j.Items[i].Error = "面板停止，未执行续签"
				j.Canceled++
			}
			j.Status, j.Phase = "canceled", "done"
			j.Error = "批量续签已取消，请刷新站点确认已执行项目的实际状态"
			m.active = ""
			touchJob(j)
			m.mu.Unlock()
			return
		}
		if index >= len(j.Items) {
			j.Status = "succeeded"
			if j.Failed > 0 {
				j.Status = "partial"
				if j.Failed == len(j.Items) {
					j.Status = "failed"
				}
			}
			j.Phase = "done"
			if m.ctx.Err() != nil {
				j.Status = "canceled"
				j.Error = "批量续签已取消，请刷新站点确认实际状态"
			}
			touchJob(j)
			m.active = ""
			m.mu.Unlock()
			return
		}
		item := j.Items[index]
		j.Items[index].Status = "running"
		j.Items[index].Phase = "preflight"
		touchJob(j)
		m.mu.Unlock()
		ctx, cancel := context.WithTimeout(m.ctx, CreateTimeout)
		redact := newJobRedactor(nil)
		emit := func(e CreateEvent) {
			m.mu.Lock()
			defer m.mu.Unlock()
			j := m.jobs[id]
			defer touchJob(j)
			if e.Phase != "" {
				j.Phase = e.Phase
				j.Items[index].Phase = e.Phase
			}
			if e.Line != "" {
				seq++
				j.Lines = append(j.Lines, JobLine{ID: seq, Time: time.Now(), Name: item.Name, Phase: j.Phase, Text: redact(e.Line)})
				if len(j.Lines) > 1000 {
					j.Lines = j.Lines[len(j.Lines)-1000:]
				}
			}
		}
		emit(CreateEvent{Phase: "preflight", Line: "开始提前续签：" + item.Name + " · " + item.Provider})
		err := backend.RenewVhost(ctx, id, item, emit)
		if ctx.Err() != nil {
			err = fmt.Errorf("任务超时或面板停止，请刷新站点确认实际状态：%v", ctx.Err())
		}
		cancel()
		if err != nil {
			emit(CreateEvent{Line: err.Error()})
		} else {
			emit(CreateEvent{Phase: "done", Line: "证书已签发并部署"})
		}
		if m.onComplete != nil {
			m.onComplete()
		}
		m.mu.Lock()
		j = m.jobs[id]
		j.Completed++
		j.Items[index].Status = "succeeded"
		if err != nil {
			j.Items[index].Status = "failed"
			j.Items[index].Error = redact(err.Error())
			j.Failed++
		} else {
			j.Succeeded++
		}
		touchJob(j)
		m.mu.Unlock()
	}
}
