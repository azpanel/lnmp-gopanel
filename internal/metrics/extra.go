package metrics

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"os"
	"regexp"
	"runtime"
	"sort"
	"strconv"
	"sync"
	"time"

	"github.com/azpanel/lnmp-gopanel/internal/config"
	"github.com/go-sql-driver/mysql"
	"github.com/shirou/gopsutil/v4/process"
)

type Nginx struct {
	Active uint64 `json:"active"`
}
type PHPFPM struct {
	Active uint64 `json:"active"`
	Idle   uint64 `json:"idle"`
	Queue  uint64 `json:"queue"`
}
type Database struct {
	Connections uint64 `json:"connections"`
	SlowQueries uint64 `json:"slow_queries"`
}
type Process struct {
	PID           int32    `json:"pid"`
	Name          string   `json:"name"`
	CPU           *float64 `json:"cpu"`
	RSS           uint64   `json:"rss"`
	MemoryPercent *float64 `json:"memory_percent"`
}
type Processes struct {
	CPU     []Process `json:"cpu"`
	Memory  []Process `json:"memory"`
	Partial bool      `json:"partial"`
}
type processCounter struct {
	created int64
	seconds float64
	at      time.Time
}
type extraSnapshot struct {
	processes *Processes
	nginx     *Nginx
	php       *PHPFPM
	database  *Database
	errors    map[string]string
}

// Configure sets read-only connections before Run starts.
func (c *Collector) Configure(m config.MetricsConfig) { c.options = m }
func (c *Collector) SetEnabled(ids []string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.enabled = map[string]bool{}
	for _, id := range ids {
		c.enabled[id] = true
	}
}
func (c *Collector) enabledWidgets() map[string]bool {
	c.mu.RLock()
	defer c.mu.RUnlock()
	out := map[string]bool{}
	for k, v := range c.enabled {
		out[k] = v
	}
	return out
}

// Optional probes share one deadline and run independently of base sampling.
func (c *Collector) sampleExtra(ctx context.Context, enabled map[string]bool, totalMemory uint64) extraSnapshot {
	result := extraSnapshot{errors: map[string]string{}}
	var wg sync.WaitGroup
	var mu sync.Mutex
	run := func(key string, probe func() error) {
		if !enabled[key] {
			return
		}
		wg.Go(func() {
			if err := probe(); err != nil {
				mu.Lock()
				result.errors[key] = err.Error()
				mu.Unlock()
			}
		})
	}
	run("processes", func() error { var err error; result.processes, err = c.sampleProcesses(ctx, totalMemory); return err })
	if !enabled["processes"] {
		c.prevProcesses = nil
	}
	run("nginx", func() error {
		if c.options.NginxStatusURL == "" {
			return errors.New("请配置 metrics.nginx_status_url")
		}
		data, err := statusBody(ctx, c.options.NginxStatusURL, false)
		if err == nil {
			result.nginx, err = parseNginx(data)
		}
		if err != nil {
			return errors.New("Nginx 状态采集失败，请检查本机 stub_status 地址")
		}
		return nil
	})
	run("php_fpm", func() error {
		if c.options.PHPFPMStatusURL == "" {
			return errors.New("请配置 metrics.php_fpm_status_url")
		}
		data, err := statusBody(ctx, c.options.PHPFPMStatusURL, true)
		if err == nil {
			result.php, err = parsePHPFPM(data)
		}
		if err != nil {
			return errors.New("PHP-FPM 状态采集失败，请检查本机 JSON 状态地址")
		}
		return nil
	})
	run("database", func() error {
		cfg := c.options.MySQL
		if cfg.User == "" || (cfg.Socket == "" && cfg.Address == "") {
			return errors.New("请配置 metrics.mysql 的连接地址和监控账号")
		}
		var err error
		result.database, err = sampleDatabase(ctx, cfg)
		if err != nil {
			return errors.New("数据库状态采集失败，请检查本机连接、凭据和状态查询权限")
		}
		return nil
	})
	wg.Wait()
	return result
}

var statusClient = &http.Client{
	Timeout:       1400 * time.Millisecond,
	Transport:     &http.Transport{Proxy: nil, DisableKeepAlives: true, ResponseHeaderTimeout: time.Second},
	CheckRedirect: func(_ *http.Request, _ []*http.Request) error { return errors.New("状态接口不允许重定向") },
}

func statusBody(ctx context.Context, raw string, php bool) ([]byte, error) {
	if !config.LoopbackURL(raw) {
		return nil, errors.New("状态接口仅支持本机 HTTP")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, raw, nil)
	if err != nil {
		return nil, err
	}
	if php {
		q := req.URL.Query()
		q.Set("json", "")
		q.Del("full")
		req.URL.RawQuery = q.Encode()
	}
	response, err := statusClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return nil, errors.New("状态接口响应失败")
	}
	body, err := io.ReadAll(io.LimitReader(response.Body, 65537))
	if err != nil || len(body) > 65536 {
		return nil, errors.New("状态接口响应无效或过大")
	}
	return body, nil
}

var nginxPattern = regexp.MustCompile(`(?s)^\s*Active connections:\s*(\d+)\s+server accepts handled requests\s+\d+\s+\d+\s+\d+\s+Reading:\s*\d+\s+Writing:\s*\d+\s+Waiting:\s*\d+\s*$`)

func parseNginx(data []byte) (*Nginx, error) {
	matches := nginxPattern.FindSubmatch(data)
	if len(matches) != 2 {
		return nil, errors.New("无效的 stub_status 响应")
	}
	n, err := strconv.ParseUint(string(matches[1]), 10, 64)
	if err != nil {
		return nil, err
	}
	return &Nginx{Active: n}, nil
}
func parsePHPFPM(data []byte) (*PHPFPM, error) {
	var fields map[string]json.RawMessage
	if json.Unmarshal(data, &fields) != nil {
		return nil, errors.New("无效的 PHP-FPM JSON")
	}
	out := &PHPFPM{}
	for key, target := range map[string]*uint64{"active processes": &out.Active, "idle processes": &out.Idle, "listen queue": &out.Queue} {
		value, ok := fields[key]
		if !ok || string(value) == "null" || json.Unmarshal(value, target) != nil {
			return nil, errors.New("PHP-FPM 状态字段缺失或无效")
		}
	}
	return out, nil
}
func sampleDatabase(ctx context.Context, cfg config.MetricsMySQLConfig) (*Database, error) {
	opts := mysql.NewConfig()
	opts.User, opts.Passwd = cfg.User, cfg.Password
	opts.Net, opts.Addr = "unix", cfg.Socket
	if cfg.Address != "" {
		opts.Net, opts.Addr = "tcp", cfg.Address
	}
	opts.Timeout, opts.ReadTimeout, opts.WriteTimeout = time.Second, time.Second, time.Second
	connector, err := mysql.NewConnector(opts)
	if err != nil {
		return nil, err
	}
	db := sql.OpenDB(connector)
	defer db.Close()
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(0)
	return queryDatabase(ctx, db)
}
func queryDatabase(ctx context.Context, db *sql.DB) (*Database, error) {
	rows, err := db.QueryContext(ctx, "SHOW GLOBAL STATUS WHERE Variable_name IN ('Threads_connected', 'Slow_queries')")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := &Database{}
	seen := map[string]bool{}
	for rows.Next() {
		var key, value string
		if err = rows.Scan(&key, &value); err != nil {
			return nil, err
		}
		count, e := strconv.ParseUint(value, 10, 64)
		if e != nil {
			return nil, e
		}
		switch key {
		case "Threads_connected":
			result.Connections = count
		case "Slow_queries":
			result.SlowQueries = count
		default:
			continue
		}
		seen[key] = true
	}
	if err = rows.Err(); err != nil {
		return nil, err
	}
	if !seen["Threads_connected"] || !seen["Slow_queries"] {
		return nil, errors.New("数据库状态字段缺失")
	}
	return result, nil
}
func processCPU(old, current processCounter, cores int) *float64 {
	elapsed := current.at.Sub(old.at).Seconds()
	if old.at.IsZero() || old.created != current.created || elapsed <= 0 || current.seconds < old.seconds || cores <= 0 {
		return nil
	}
	value := (current.seconds - old.seconds) / elapsed / float64(cores) * 100
	if value > 100 {
		value = 100
	}
	return &value
}
func rankProcesses(all []Process) *Processes {
	result := &Processes{CPU: []Process{}, Memory: append([]Process{}, all...)}
	for _, p := range all {
		if p.CPU != nil {
			result.CPU = append(result.CPU, p)
		}
	}
	sort.Slice(result.CPU, func(i, j int) bool {
		if *result.CPU[i].CPU == *result.CPU[j].CPU {
			return result.CPU[i].PID < result.CPU[j].PID
		}
		return *result.CPU[i].CPU > *result.CPU[j].CPU
	})
	sort.Slice(result.Memory, func(i, j int) bool {
		if result.Memory[i].RSS == result.Memory[j].RSS {
			return result.Memory[i].PID < result.Memory[j].PID
		}
		return result.Memory[i].RSS > result.Memory[j].RSS
	})
	if len(result.CPU) > 10 {
		result.CPU = result.CPU[:10]
	}
	if len(result.Memory) > 10 {
		result.Memory = result.Memory[:10]
	}
	return result
}
func (c *Collector) sampleProcesses(ctx context.Context, totalMemory uint64) (*Processes, error) {
	ids, err := process.PidsWithContext(ctx)
	if err != nil {
		c.prevProcesses = nil
		return nil, errors.New("无法读取进程列表")
	}
	next := map[int32]processCounter{}
	all := []Process{}
	partial := false
	for _, id := range ids {
		if ctx.Err() != nil {
			partial = true
			break
		}
		p, e := process.NewProcessWithContext(ctx, id)
		if e != nil {
			partial = partial || errors.Is(e, os.ErrPermission)
			continue
		}
		created, e := p.CreateTimeWithContext(ctx)
		if e != nil {
			partial = partial || errors.Is(e, os.ErrPermission)
			continue
		}
		times, e := p.TimesWithContext(ctx)
		if e != nil {
			partial = partial || errors.Is(e, os.ErrPermission)
			continue
		}
		memory, e := p.MemoryInfoWithContext(ctx)
		if e != nil {
			partial = partial || errors.Is(e, os.ErrPermission)
			continue
		}
		name, e := p.NameWithContext(ctx)
		if e != nil {
			name = "未知进程"
			partial = true
		}
		current := processCounter{created: created, seconds: times.User + times.System, at: time.Now()}
		next[id] = current
		item := Process{PID: id, Name: name, CPU: processCPU(c.prevProcesses[id], current, runtime.NumCPU()), RSS: memory.RSS}
		if totalMemory > 0 {
			percentage := float64(memory.RSS) / float64(totalMemory) * 100
			item.MemoryPercent = &percentage
		}
		all = append(all, item)
	}
	c.prevProcesses = next
	if len(all) == 0 && partial {
		return nil, errors.New("进程数据不可读取或采集超时")
	}
	result := rankProcesses(all)
	result.Partial = partial
	return result, nil
}

// MemoryParts creates non-overlapping segments even with inconsistent kernel counters.
func MemoryParts(total, free, buffers, cached uint64) (other, boundedFree, boundedBuffers, boundedCached uint64) {
	remaining := total
	take := func(value uint64) uint64 {
		if value > remaining {
			value = remaining
		}
		remaining -= value
		return value
	}
	boundedFree = take(free)
	boundedBuffers = take(buffers)
	boundedCached = take(cached)
	return remaining, boundedFree, boundedBuffers, boundedCached
}
