package vhosts

import (
	"context"
	"errors"
	"io"
	"net"
	"net/http"
	"slices"
	"strings"
	"sync"
	"time"
)

type DNSRecord struct {
	Domain    string   `json:"domain"`
	Addresses []string `json:"addresses"`
	Matched   []string `json:"matched"`
	Status    string   `json:"status"`
	Error     string   `json:"error,omitempty"`
}
type DNSResult struct {
	ServerIPs []string    `json:"server_ips"`
	Warnings  []string    `json:"warnings"`
	Records   []DNSRecord `json:"records"`
}
type DNSChecker struct {
	PublicIPs []string
	Lookup    func(context.Context, string) ([]net.IPAddr, error)
	Discover  func(context.Context) ([]string, []string)
	mu        sync.Mutex
	updated   time.Time
	ips       []string
	warnings  []string
}

func DiscoverIPs(ctx context.Context) ([]string, []string) {
	ips, warnings := []string{}, []string{}
	if addrs, err := net.InterfaceAddrs(); err == nil {
		for _, addr := range addrs {
			ip, _, err := net.ParseCIDR(addr.String())
			if err == nil && ip.IsGlobalUnicast() {
				ips = append(ips, ip.String())
			}
		}
	}
	type result struct {
		ip  string
		err error
	}
	results := make(chan result, 2)
	for _, endpoint := range []string{"https://api.ipify.org", "https://api6.ipify.org"} {
		go func() {
			req, _ := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
			client := &http.Client{Timeout: 4 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
			resp, err := client.Do(req)
			if err != nil {
				results <- result{err: err}
				return
			}
			defer resp.Body.Close()
			data, err := io.ReadAll(io.LimitReader(resp.Body, 128))
			ip := net.ParseIP(strings.TrimSpace(string(data)))
			if err != nil || resp.StatusCode != 200 || ip == nil || !ip.IsGlobalUnicast() {
				results <- result{err: errors.New("公网 IP 探测失败")}
				return
			}
			results <- result{ip: ip.String()}
		}()
	}
	for range 2 {
		r := <-results
		if r.err != nil {
			warnings = append(warnings, "部分公网 IP 无法探测，未匹配的解析可能无法确认；NAT 可在配置中补充公网 IP")
		} else {
			ips = append(ips, r.ip)
		}
	}
	slices.Sort(ips)
	ips = slices.Compact(ips)
	slices.Sort(warnings)
	warnings = slices.Compact(warnings)
	return ips, warnings
}
func (d *DNSChecker) Check(parent context.Context, domains []string) (DNSResult, error) {
	out := DNSResult{ServerIPs: []string{}, Warnings: []string{}, Records: []DNSRecord{}}
	if len(domains) == 0 || len(domains) > 50 {
		return out, errors.New("请提供 1–50 个域名")
	}
	normalized := []string{}
	for _, domain := range domains {
		name, err := NormalizeDomain(domain)
		if err != nil {
			return out, err
		}
		if !slices.Contains(normalized, name) {
			normalized = append(normalized, name)
		}
	}
	ctx, cancel := context.WithTimeout(parent, 8*time.Second)
	defer cancel()
	d.mu.Lock()
	if time.Since(d.updated) > 5*time.Minute {
		discover := d.Discover
		if discover == nil {
			discover = DiscoverIPs
		}
		d.ips, d.warnings = discover(ctx)
		d.updated = time.Now()
	}
	out.ServerIPs = append(out.ServerIPs, d.ips...)
	out.Warnings = append(out.Warnings, d.warnings...)
	d.mu.Unlock()
	for _, value := range d.PublicIPs {
		if ip := net.ParseIP(value); ip != nil && !slices.Contains(out.ServerIPs, ip.String()) {
			out.ServerIPs = append(out.ServerIPs, ip.String())
		}
	}
	lookup := d.Lookup
	if lookup == nil {
		lookup = net.DefaultResolver.LookupIPAddr
	}
	records := make([]DNSRecord, len(normalized))
	var wg sync.WaitGroup
	// Limit each request to eight concurrent DNS queries.
	slots := make(chan struct{}, 8)
	for i, domain := range normalized {
		wg.Add(1)
		go func() {
			defer wg.Done()
			slots <- struct{}{}
			defer func() { <-slots }()
			r := DNSRecord{Domain: domain, Addresses: []string{}, Matched: []string{}}
			addresses, err := lookup(ctx, domain)
			if err != nil {
				r.Status = "unknown"
				r.Error = "解析查询失败或超时"
				var dns *net.DNSError
				if errors.As(err, &dns) && dns.IsNotFound {
					r.Status = "no_records"
					r.Error = "未找到 A／AAAA 记录"
				}
			} else {
				for _, addr := range addresses {
					ip := addr.IP.String()
					if !slices.Contains(r.Addresses, ip) {
						r.Addresses = append(r.Addresses, ip)
					}
					if slices.Contains(out.ServerIPs, ip) && !slices.Contains(r.Matched, ip) {
						r.Matched = append(r.Matched, ip)
					}
				}
				switch {
				case len(r.Addresses) == 0:
					r.Status = "no_records"
				case len(r.Matched) == len(r.Addresses):
					r.Status = "matched"
				case len(r.Matched) > 0:
					r.Status = "partial"
				case len(out.ServerIPs) == 0 || len(out.Warnings) > 0:
					r.Status = "unknown"
				default:
					r.Status = "mismatch"
				}
			}
			records[i] = r
		}()
	}
	wg.Wait()
	out.Records = records
	return out, nil
}
