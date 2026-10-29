package metrics

import (
	"context"
	"runtime"
	"sync"
	"time"

	"github.com/azpanel/lnmp-gopanel/internal/config"
	"github.com/shirou/gopsutil/v4/cpu"
	"github.com/shirou/gopsutil/v4/disk"
	"github.com/shirou/gopsutil/v4/host"
	"github.com/shirou/gopsutil/v4/load"
	"github.com/shirou/gopsutil/v4/mem"
	gnet "github.com/shirou/gopsutil/v4/net"
)

type Memory struct {
	Available uint64  `json:"available"`
	Free      uint64  `json:"free"`
	Buffers   uint64  `json:"buffers"`
	Cached    uint64  `json:"cached"`
	Other     uint64  `json:"other"`
	Used      uint64  `json:"used"`
	Total     uint64  `json:"total"`
	Percent   float64 `json:"percent"`
}
type Rates struct {
	Read  float64 `json:"read"`
	Write float64 `json:"write"`
}
type Disk struct {
	Path    string  `json:"path"`
	Device  string  `json:"device"`
	Total   uint64  `json:"total"`
	Used    uint64  `json:"used"`
	Percent float64 `json:"percent"`
}
type Load struct {
	One     float64 `json:"one"`
	Five    float64 `json:"five"`
	Fifteen float64 `json:"fifteen"`
}
type Snapshot struct {
	Processes *Processes        `json:"processes,omitempty"`
	Nginx     *Nginx            `json:"nginx,omitempty"`
	PHPFPM    *PHPFPM           `json:"php_fpm,omitempty"`
	Database  *Database         `json:"database,omitempty"`
	Timestamp time.Time         `json:"timestamp"`
	CPU       *float64          `json:"cpu"`
	Memory    *Memory           `json:"memory"`
	Swap      *Memory           `json:"swap"`
	Load      *Load             `json:"load"`
	Uptime    *uint64           `json:"uptime"`
	Network   *Rates            `json:"network"`
	DiskIO    *Rates            `json:"disk_io"`
	Disks     []Disk            `json:"disks"`
	Errors    map[string]string `json:"errors"`
}
type Host struct {
	Hostname string `json:"hostname"`
	OS       string `json:"os"`
	Platform string `json:"platform"`
	Version  string `json:"version"`
	Kernel   string `json:"kernel"`
	Arch     string `json:"arch"`
	Cores    int    `json:"cores"`
	CPUModel string `json:"cpu_model"`
}

func HostInfo(ctx context.Context) Host {
	h := Host{OS: runtime.GOOS, Arch: runtime.GOARCH, Cores: runtime.NumCPU()}
	if v, e := host.InfoWithContext(ctx); e == nil {
		h.Hostname = v.Hostname
		h.Platform = v.Platform
		h.Version = v.PlatformVersion
		h.Kernel = v.KernelVersion
	}
	if v, e := cpu.InfoWithContext(ctx); e == nil && len(v) > 0 {
		h.CPUModel = v[0].ModelName
	}
	return h
}

// Rate returns unavailable after a reset or until two observations exist.
func Rate(previous, current uint64, seconds float64) (float64, bool) {
	if seconds <= 0 || current < previous {
		return 0, false
	}
	return float64(current-previous) / seconds, true
}

type counter struct {
	read, write uint64
	at          time.Time
}

func rates(prev, current counter) *Rates {
	a, ok := Rate(prev.read, current.read, current.at.Sub(prev.at).Seconds())
	b, ok2 := Rate(prev.write, current.write, current.at.Sub(prev.at).Seconds())
	if prev.at.IsZero() || !ok || !ok2 {
		return nil
	}
	return &Rates{Read: a, Write: b}
}

type Collector struct {
	options       config.MetricsConfig
	enabled       map[string]bool
	prevProcesses map[int32]processCounter
	mu            sync.RWMutex
	latest        Snapshot
	clients       map[chan Snapshot]struct{}
	prevCPU       *cpu.TimesStat
	prevNet       map[string]counter
	prevDisk      map[string]counter
	disks         []Disk
	diskErrors    map[string]string
	diskAt        time.Time
}

func New() *Collector {
	return &Collector{clients: map[chan Snapshot]struct{}{}, prevNet: map[string]counter{}, prevDisk: map[string]counter{}, latest: Snapshot{Disks: []Disk{}, Errors: map[string]string{}}}
}
func (c *Collector) Latest() Snapshot { c.mu.RLock(); defer c.mu.RUnlock(); return c.latest }
func (c *Collector) Subscribe() (chan Snapshot, func()) {
	c.mu.Lock()
	defer c.mu.Unlock()
	ch := make(chan Snapshot, 1)
	c.clients[ch] = struct{}{}
	if !c.latest.Timestamp.IsZero() {
		ch <- c.latest
	}
	return ch, func() { c.mu.Lock(); defer c.mu.Unlock(); delete(c.clients, ch) }
}
func (c *Collector) publish(s Snapshot) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.latest = s
	for ch := range c.clients {
		select {
		case ch <- s:
		default:
			select {
			case <-ch:
			default:
			}
			ch <- s
		}
	}
}
func (c *Collector) Run(ctx context.Context) {
	c.sample(ctx)
	t := time.NewTicker(2 * time.Second)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			c.sample(ctx)
		}
	}
}
func (c *Collector) sample(parent context.Context) {
	ctx, cancel := context.WithTimeout(parent, 1800*time.Millisecond)
	defer cancel()
	now := time.Now()
	s := Snapshot{Timestamp: now, Disks: []Disk{}, Errors: map[string]string{}}
	if v, e := cpu.TimesWithContext(ctx, false); e == nil && len(v) > 0 {
		if c.prevCPU != nil {
			old := c.prevCPU
			curr := v[0]
			total := cpuTotal(curr) - cpuTotal(*old)
			idle := curr.Idle + curr.Iowait - old.Idle - old.Iowait
			if total > 0 && idle >= 0 && idle <= total {
				p := (total - idle) / total * 100
				s.CPU = &p
			}
		}
		c.prevCPU = &v[0]
	} else {
		s.Errors["cpu"] = "CPU 采集失败"
		c.prevCPU = nil
	}
	if v, e := mem.VirtualMemoryWithContext(ctx); e == nil {
		other, free, buffers, cached := MemoryParts(v.Total, v.Free, v.Buffers, v.Cached)
		s.Memory = &Memory{Used: v.Used, Total: v.Total, Percent: v.UsedPercent, Available: v.Available, Free: free, Buffers: buffers, Cached: cached, Other: other}
	} else {
		s.Errors["memory"] = "内存采集失败"
	}
	enabled := c.enabledWidgets()
	extraCh := make(chan extraSnapshot, 1)
	var totalMemory uint64
	if s.Memory != nil {
		totalMemory = s.Memory.Total
	}
	go func() {
		extraCtx, done := context.WithTimeout(ctx, 1500*time.Millisecond)
		defer done()
		extraCh <- c.sampleExtra(extraCtx, enabled, totalMemory)
	}()
	if v, e := mem.SwapMemoryWithContext(ctx); e == nil {
		s.Swap = &Memory{Used: v.Used, Total: v.Total, Percent: v.UsedPercent}
	} else {
		s.Errors["swap"] = "Swap 采集失败"
	}
	if v, e := load.AvgWithContext(ctx); e == nil {
		s.Load = &Load{One: v.Load1, Five: v.Load5, Fifteen: v.Load15}
	} else {
		s.Errors["load"] = "负载采集失败"
	}
	if v, e := host.UptimeWithContext(ctx); e == nil {
		s.Uptime = &v
	} else {
		s.Errors["uptime"] = "运行时间采集失败"
	}
	if v, e := gnet.IOCountersWithContext(ctx, true); e == nil {
		next := map[string]counter{}
		sum := Rates{}
		valid := true
		count := 0
		for _, n := range v {
			if n.Name == "lo" {
				continue
			}
			curr := counter{n.BytesRecv, n.BytesSent, now}
			next[n.Name] = curr
			r := rates(c.prevNet[n.Name], curr)
			if r == nil {
				valid = false
			} else {
				sum.Read += r.Read
				sum.Write += r.Write
			}
			count++
		}
		if valid && count > 0 {
			s.Network = &sum
		}
		c.prevNet = next
	} else {
		s.Errors["network"] = "网络采集失败"
		c.prevNet = map[string]counter{}
	}
	if v, e := disk.IOCountersWithContext(ctx); e == nil {
		next := map[string]counter{}
		sum := Rates{}
		valid := true
		count := 0
		for name, n := range v {
			if !wholeDevice(name) {
				continue
			}
			curr := counter{n.ReadBytes, n.WriteBytes, now}
			next[name] = curr
			r := rates(c.prevDisk[name], curr)
			if r == nil {
				valid = false
			} else {
				sum.Read += r.Read
				sum.Write += r.Write
			}
			count++
		}
		if valid && count > 0 {
			s.DiskIO = &sum
		}
		c.prevDisk = next
	} else {
		s.Errors["disk_io"] = "磁盘 I/O 采集失败"
		c.prevDisk = map[string]counter{}
	}
	if now.Sub(c.diskAt) >= 30*time.Second {
		c.disks = []Disk{}
		c.diskErrors = map[string]string{}
		v, e := disk.PartitionsWithContext(ctx, false)
		if e != nil {
			c.diskErrors["disks"] = "挂载点采集失败"
		} else {
			seen := map[string]bool{}
			for _, p := range v {
				if seen[p.Mountpoint] {
					continue
				}
				seen[p.Mountpoint] = true
				u, e := disk.UsageWithContext(ctx, p.Mountpoint)
				if e != nil {
					c.diskErrors["disk:"+p.Mountpoint] = "磁盘容量采集失败"
					continue
				}
				c.disks = append(c.disks, Disk{Path: p.Mountpoint, Device: p.Device, Total: u.Total, Used: u.Used, Percent: u.UsedPercent})
			}
		}
		c.diskAt = now
	}
	s.Disks = c.disks
	for k, v := range c.diskErrors {
		s.Errors[k] = v
	}
	extra := <-extraCh
	s.Processes, s.Nginx, s.PHPFPM, s.Database = extra.processes, extra.nginx, extra.php, extra.database
	for k, v := range extra.errors {
		s.Errors[k] = v
	}
	c.publish(s)
}
func cpuTotal(t cpu.TimesStat) float64 {
	return t.User + t.System + t.Idle + t.Nice + t.Iowait + t.Irq + t.Softirq + t.Steal
}
