package vhosts

import (
	"context"
	"crypto/sha256"
	"crypto/x509"
	"encoding/hex"
	"encoding/pem"
	"errors"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	nginx "github.com/tufanbarisyildirim/gonginx/config"
	"github.com/tufanbarisyildirim/gonginx/parser"
)

const Directory = "/usr/local/nginx/conf/vhost"
const Prefix = "/usr/local/nginx"
const MaxConfig = 1024 * 1024

type Certificate struct {
	Path          string     `json:"path"`
	Issuer        string     `json:"issuer"`
	Subject       string     `json:"subject"`
	DNSNames      []string   `json:"dns_names"`
	NotBefore     *time.Time `json:"not_before"`
	NotAfter      *time.Time `json:"not_after"`
	DaysRemaining int        `json:"days_remaining"`
	Serial        string     `json:"serial"`
	Fingerprint   string     `json:"fingerprint"`
	Status        string     `json:"status"`
	Error         string     `json:"error,omitempty"`
}
type Root struct {
	Configured   string `json:"configured"`
	Path         string `json:"path"`
	PublicParent bool   `json:"public_parent"`
	Error        string `json:"error,omitempty"`
}
type Site struct {
	Name         string        `json:"name"`
	Filename     string        `json:"filename"`
	Enabled      bool          `json:"enabled"`
	Domains      []string      `json:"domains"`
	Listens      []string      `json:"listens"`
	Roots        []Root        `json:"roots"`
	Proxies      []string      `json:"proxies"`
	Certificates []Certificate `json:"certificates"`
	Modified     time.Time     `json:"modified"`
	Warnings     []string      `json:"warnings"`
	Error        string        `json:"error,omitempty"`
}
type ConfigView struct {
	Filename string `json:"filename"`
	Content  string `json:"content"`
}
type Store struct {
	Directory string
	Prefix    string
}

func DefaultStore() Store { return Store{Directory: Directory, Prefix: Prefix} }
func ValidName(name string) bool {
	return len(name) > 5 && len(name) <= 240 && filepath.Base(name) == name && !strings.ContainsAny(name, "/\\\x00\r\n") && !strings.HasPrefix(name, ".") && strings.HasSuffix(name, ".conf")
}
func readBounded(path string, limit int64) ([]byte, error) {
	f, e := os.Open(path)
	if e != nil {
		return nil, e
	}
	defer f.Close()
	data, e := io.ReadAll(io.LimitReader(f, limit+1))
	if e != nil {
		return nil, e
	}
	if int64(len(data)) > limit {
		return nil, errors.New("文件过大，无法解析")
	}
	return data, nil
}
func (s Store) file(name string) (string, bool, error) {
	if !ValidName(name) {
		return "", false, errors.New("配置文件名无效")
	}
	active, disabled := filepath.Join(s.Directory, name), filepath.Join(s.Directory, name+".disabled")
	a, ae := os.Lstat(active)
	d, de := os.Lstat(disabled)
	if ae == nil && de == nil {
		return "", false, errors.New("同名 .conf 和 .conf.disabled 同时存在，无法自动操作")
	}
	if ae == nil {
		if !a.Mode().IsRegular() {
			return "", false, errors.New("仅支持普通配置文件，不操作符号链接")
		}
		return active, true, nil
	}
	if de == nil {
		if !d.Mode().IsRegular() {
			return "", false, errors.New("仅支持普通配置文件，不操作符号链接")
		}
		return disabled, false, nil
	}
	return "", false, errors.New("站点配置不存在或不可读取")
}
func (s Store) Read(name string) (ConfigView, error) {
	path, _, e := s.file(name)
	if e != nil {
		return ConfigView{}, e
	}
	b, e := readBounded(path, MaxConfig)
	if e != nil {
		return ConfigView{}, errors.New("配置读取失败或超出 1 MiB 限制")
	}
	return ConfigView{Filename: filepath.Base(path), Content: string(b)}, nil
}
func (s Store) List(ctx context.Context) ([]Site, error) {
	entries, e := os.ReadDir(s.Directory)
	if e != nil {
		return nil, errors.New("无法读取 Nginx vhost 目录")
	}
	names := map[string]bool{}
	for _, entry := range entries {
		name := strings.TrimSuffix(entry.Name(), ".disabled")
		if ValidName(name) {
			names[name] = true
		}
	}
	sorted := []string{}
	for name := range names {
		sorted = append(sorted, name)
	}
	sort.Strings(sorted)
	if len(sorted) > 1000 {
		return nil, errors.New("站点数量超过 1000，请缩小配置目录")
	}
	sites := []Site{}
	for _, name := range sorted {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		site := Site{Name: name, Domains: []string{}, Listens: []string{}, Roots: []Root{}, Proxies: []string{}, Certificates: []Certificate{}, Warnings: []string{}}
		path, enabled, e := s.file(name)
		if e != nil {
			site.Error = e.Error()
			site.Filename = name
			sites = append(sites, site)
			continue
		}
		site.Enabled = enabled
		site.Filename = filepath.Base(path)
		if info, e := os.Stat(path); e == nil {
			site.Modified = info.ModTime()
		}
		data, e := readBounded(path, MaxConfig)
		if e != nil {
			site.Error = "配置不可读取或超过 1 MiB"
			sites = append(sites, site)
			continue
		}
		config, e := parser.NewStringParser(string(data)).Parse()
		if e != nil {
			site.Error = "配置解析失败，请查看配置或运行 nginx -t"
			sites = append(sites, site)
			continue
		}
		budget := 0
		visited := map[string]bool{path: true}
		s.collect(ctx, config.GetDirectives(), &site, visited, 0, &budget)
		sites = append(sites, site)
	}
	return sites, nil
}
func values(d nginx.IDirective) []string {
	out := []string{}
	for _, p := range d.GetParameters() {
		out = append(out, nginxValue(p.GetValue()))
	}
	return out
}
func unique(items []string, value string) []string {
	for _, v := range items {
		if v == value {
			return items
		}
	}
	return append(items, value)
}
func (s Store) collect(ctx context.Context, directives []nginx.IDirective, site *Site, visited map[string]bool, depth int, budget *int) {
	if depth > 12 || ctx.Err() != nil {
		site.Warnings = unique(site.Warnings, "包含文件过深或读取超时，部分信息未解析")
		return
	}
	for _, d := range directives {
		args := values(d)
		value := strings.Join(args, " ")
		switch d.GetName() {
		case "server_name":
			for _, v := range args {
				site.Domains = unique(site.Domains, v)
			}
		case "listen":
			site.Listens = unique(site.Listens, value)
		case "proxy_pass":
			site.Proxies = unique(site.Proxies, value)
		case "root":
			if len(args) == 1 {
				root := s.root(args[0])
				exists := false
				for _, r := range site.Roots {
					if r.Configured == root.Configured {
						exists = true
					}
				}
				if !exists {
					site.Roots = append(site.Roots, root)
				}
			}
		case "ssl_certificate":
			if len(args) == 1 {
				path := s.configPath(args[0])
				exists := false
				for _, c := range site.Certificates {
					if c.Path == path {
						exists = true
					}
				}
				if !exists {
					site.Certificates = append(site.Certificates, certificate(path, time.Now()))
				}
			}
		case "include":
			if len(args) != 1 || strings.Contains(args[0], "$") {
				site.Warnings = unique(site.Warnings, "动态 include 未展开")
				break
			}
			matches, _ := filepath.Glob(s.configPath(args[0]))
			for _, path := range matches {
				if ctx.Err() != nil || *budget > 4*MaxConfig {
					site.Warnings = unique(site.Warnings, "部分 include 超过解析限制")
					break
				}
				if visited[path] {
					continue
				}
				visited[path] = true
				data, e := readBounded(path, MaxConfig)
				*budget += len(data)
				if e != nil || *budget > 4*MaxConfig {
					site.Warnings = unique(site.Warnings, "部分 include 不可读取或超过解析限制")
					continue
				}
				parsed, e := parser.NewStringParser(string(data)).Parse()
				if e != nil {
					site.Warnings = unique(site.Warnings, "部分 include 解析失败")
					continue
				}
				s.collect(ctx, parsed.GetDirectives(), site, visited, depth+1, budget)
			}
		}
		if block := d.GetBlock(); block != nil {
			s.collect(ctx, block.GetDirectives(), site, visited, depth+1, budget)
		}
	}
}
func (s Store) configPath(path string) string {
	if filepath.IsAbs(path) || strings.Contains(path, "$") {
		return path
	}
	return filepath.Join(s.Prefix, "conf", path)
}
func (s Store) root(path string) Root {
	r := Root{Configured: path}
	if strings.Contains(path, "$") {
		r.Error = "动态 root 无法静态计算空间占用"
		return r
	}
	if !filepath.IsAbs(path) {
		path = filepath.Join(s.Prefix, path)
	}
	path = filepath.Clean(path)
	if filepath.Base(path) == "public" {
		r.PublicParent = true
		path = filepath.Dir(path)
	}
	r.Path = path
	return r
}
func certificate(path string, now time.Time) Certificate {
	c := Certificate{Path: path, Status: "unavailable", DNSNames: []string{}}
	if strings.Contains(path, "$") || strings.HasPrefix(path, "data:") {
		c.Error = "动态证书无法从文件读取"
		return c
	}
	data, e := readBounded(path, 4*MaxConfig)
	if e != nil {
		c.Error = "证书文件不可读取"
		return c
	}
	var cert *x509.Certificate
	for len(data) > 0 {
		block, rest := pem.Decode(data)
		if block == nil {
			break
		}
		data = rest
		if block.Type != "CERTIFICATE" {
			continue
		}
		cert, e = x509.ParseCertificate(block.Bytes)
		if e == nil {
			break
		}
	}
	if cert == nil {
		c.Error = "未找到有效的 PEM 证书"
		return c
	}
	c.Issuer = cert.Issuer.String()
	c.Subject = cert.Subject.String()
	c.DNSNames = cert.DNSNames
	if c.DNSNames == nil {
		c.DNSNames = []string{}
	}
	c.NotBefore = &cert.NotBefore
	c.NotAfter = &cert.NotAfter
	c.Serial = cert.SerialNumber.Text(16)
	hash := sha256.Sum256(cert.Raw)
	c.Fingerprint = hex.EncodeToString(hash[:])
	c.DaysRemaining = int(cert.NotAfter.Sub(now).Hours() / 24)
	switch {
	case now.Before(cert.NotBefore):
		c.Status = "not_yet_valid"
	case !now.Before(cert.NotAfter):
		c.Status = "expired"
	case cert.NotAfter.Sub(now) <= 30*24*time.Hour:
		c.Status = "expiring"
	default:
		c.Status = "valid"
	}
	return c
}

// The parser preserves literal quote marks; nginx paths use the decoded token.
func nginxValue(value string) string {
	if len(value) >= 2 && ((value[0] == '"' && value[len(value)-1] == '"') || (value[0] == 39 && value[len(value)-1] == 39)) {
		value = value[1 : len(value)-1]
	}
	var out strings.Builder
	for i := 0; i < len(value); i++ {
		if value[i] == '\\' && i+1 < len(value) {
			switch value[i+1] {
			case 'n':
				out.WriteByte('\n')
				i++
				continue
			case 'r':
				out.WriteByte('\r')
				i++
				continue
			case 't':
				out.WriteByte('\t')
				i++
				continue
			case '\\', '"', 39, ' ', ';', '#', '{', '}':
				out.WriteByte(value[i+1])
				i++
				continue
			}
		}
		out.WriteByte(value[i])
	}
	return out.String()
}
