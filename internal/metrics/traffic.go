package metrics

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"sync"
	"time"
)

type TrafficPoint struct {
	Time  time.Time `json:"time"`
	Label string    `json:"label"`
	RX    *uint64   `json:"rx"`
	TX    *uint64   `json:"tx"`
}

type Traffic struct {
	Interface string         `json:"interface"`
	Period    string         `json:"period"`
	UpdatedAt time.Time      `json:"updated_at"`
	Points    []TrafficPoint `json:"points"`
}

type trafficResult struct {
	data *Traffic
	err  error
	at   time.Time
	done chan struct{}
}

// TrafficReader queries vnstat independently of the two-second metric stream.
type TrafficReader struct {
	mu            sync.Mutex
	cache         map[string]*trafficResult
	interfaceName func() (string, error)
	run           func(context.Context, string, string) ([]byte, error)
	now           func() time.Time
}

func NewTrafficReader() *TrafficReader {
	return &TrafficReader{
		cache: map[string]*trafficResult{}, now: time.Now, run: runVNStat,
		interfaceName: func() (string, error) {
			if runtime.GOOS != "linux" {
				return "", errors.New("网站流量统计仅支持 Linux")
			}
			interfaces, err := net.Interfaces()
			if err != nil {
				return "", errors.New("网络接口读取失败")
			}
			primary := primaryInterface(interfaces, defaultRouteInterfaces())
			if primary == nil {
				return "", errors.New("未发现非回环网卡")
			}
			return primary.Name, nil
		},
	}
}

func (r *TrafficReader) Read(ctx context.Context, period string) (*Traffic, error) {
	if period != "day" && period != "hour" {
		return nil, errors.New("流量统计粒度无效")
	}
	name, err := r.interfaceName()
	if err != nil {
		return nil, err
	}
	key := name + "/" + period
	r.mu.Lock()
	result := r.cache[key]
	if result == nil || result.done == nil && r.now().Sub(result.at) >= time.Minute {
		// Keep only the current interface, including when routes change.
		for k := range r.cache {
			if !strings.HasPrefix(k, name+"/") {
				delete(r.cache, k)
			}
		}
		result = &trafficResult{done: make(chan struct{})}
		r.cache[key] = result
		go func(result *trafficResult) {
			// One disconnected browser must not cancel a query shared by others.
			query, cancel := context.WithTimeout(context.Background(), 3*time.Second)
			defer cancel()
			body, readErr := r.run(query, name, period)
			var data *Traffic
			if readErr == nil {
				data, readErr = parseTraffic(body, name, period, r.now())
			}
			r.mu.Lock()
			defer r.mu.Unlock()
			result.data, result.err, result.at = data, readErr, r.now()
			close(result.done)
			result.done = nil
		}(result)
	}
	done := result.done
	if done == nil {
		data, readErr := result.data, result.err
		r.mu.Unlock()
		return data, readErr
	}
	r.mu.Unlock()
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-done:
		return result.data, result.err
	}
}

type trafficOutput struct{ bytes.Buffer }

func (w *trafficOutput) Write(p []byte) (int, error) {
	if len(p) > 1024*1024-w.Len() {
		return 0, errors.New("vnstat 输出超过限制")
	}
	return w.Buffer.Write(p)
}

func runVNStat(ctx context.Context, name, period string) ([]byte, error) {
	program, err := exec.LookPath("vnstat")
	if err != nil {
		return nil, errors.New("未安装 vnstat，请安装 vnstat 2.x 并启动 vnstat 服务")
	}
	mode, limit := "d", "30"
	if period == "hour" {
		mode, limit = "h", "24"
	}
	cmd := exec.CommandContext(ctx, program, "--json", mode, limit, "-i", name)
	cmd.WaitDelay = 100 * time.Millisecond
	var output, stderr trafficOutput
	cmd.Stdout, cmd.Stderr = &output, &stderr
	if err := cmd.Run(); err != nil {
		if ctx.Err() != nil {
			return nil, errors.New("vnstat 查询超时，请检查 vnstat 服务")
		}
		if errors.Is(err, os.ErrPermission) || strings.Contains(strings.ToLower(stderr.String()), "permission denied") {
			return nil, errors.New("vnstat 数据读取权限不足，请检查面板账号对 vnstat 数据库的读取权限")
		}
		return nil, fmt.Errorf("vnstat 查询失败，请检查服务、数据库及网卡 %s 的监控配置", name)
	}
	return output.Bytes(), nil
}

func parseTraffic(body []byte, name, period string, now time.Time) (*Traffic, error) {
	type entry struct {
		Timestamp int64   `json:"timestamp"`
		RX        *uint64 `json:"rx"`
		TX        *uint64 `json:"tx"`
	}
	var document struct {
		Version    string `json:"vnstatversion"`
		JSON       string `json:"jsonversion"`
		Interfaces []struct {
			Name    string `json:"name"`
			Updated struct {
				Timestamp int64 `json:"timestamp"`
			} `json:"updated"`
			Traffic struct {
				Day  []entry `json:"day"`
				Hour []entry `json:"hour"`
			} `json:"traffic"`
		} `json:"interfaces"`
	}
	decoder := json.NewDecoder(bytes.NewReader(body))
	if err := decoder.Decode(&document); err != nil {
		return nil, errors.New("vnstat 数据格式异常，请确认已安装 vnstat 2.x")
	}
	var extra any
	if decoder.Decode(&extra) != io.EOF || document.JSON != "2" || !strings.HasPrefix(document.Version, "2.") {
		return nil, errors.New("vnstat 版本不兼容，请使用支持 JSON v2 的 vnstat 2.x")
	}
	for _, iface := range document.Interfaces {
		if iface.Name != name {
			continue
		}
		if iface.Updated.Timestamp <= 0 {
			return nil, errors.New("vnstat 数据库更新时间缺失")
		}
		result := &Traffic{Interface: name, Period: period, UpdatedAt: time.Unix(iface.Updated.Timestamp, 0).In(now.Location()), Points: []TrafficPoint{}}
		entries, count, format := iface.Traffic.Day, 30, "2006-01-02"
		end := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location())
		advance := func(t time.Time, n int) time.Time { return t.AddDate(0, 0, n) }
		if period == "hour" {
			entries, count, format = iface.Traffic.Hour, 24, "2006-01-02 15:04 -07:00"
			// Preserve the UTC offset in the repeated hour when DST ends.
			end = now.Add(-time.Duration(now.Minute())*time.Minute - time.Duration(now.Second())*time.Second - time.Duration(now.Nanosecond()))
			advance = func(t time.Time, n int) time.Time { return t.Add(time.Duration(n) * time.Hour) }
		}
		start := advance(end, 1-count)
		values := map[int64]entry{}
		for _, item := range entries {
			at := time.Unix(item.Timestamp, 0).In(now.Location())
			if item.Timestamp <= 0 || item.RX == nil || item.TX == nil {
				return nil, errors.New("vnstat 流量记录不完整")
			}
			if !at.Before(start) && !at.After(end) {
				values[at.Unix()] = item
			}
		}
		if len(values) == 0 {
			return result, nil
		}
		for i := 0; i < count; i++ {
			at := advance(start, i)
			item := values[at.Unix()]
			result.Points = append(result.Points, TrafficPoint{Time: at, Label: at.Format(format), RX: item.RX, TX: item.TX})
		}
		return result, nil
	}
	return nil, fmt.Errorf("vnstat 尚未监控网卡 %s，请检查 vnstat 监控配置", name)
}
