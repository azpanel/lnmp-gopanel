package vhosts

import (
	"errors"
	"fmt"
	"path/filepath"
	"slices"
	"sort"
	"strings"
	"time"
)

type RenewalPlan struct {
	Name        string
	Original    string
	Base        string
	AuthFile    string
	Domains     []string
	Certificate Certificate
	CertPath    string
	KeyPath     string
}
type configEdit struct {
	start, end int
	text       string
}

func applyConfigEdits(content string, edits []configEdit) string {
	sort.Slice(edits, func(i, j int) bool {
		if edits[i].start == edits[j].start {
			return edits[i].end > edits[j].end
		}
		return edits[i].start > edits[j].start
	})
	for _, e := range edits {
		content = content[:e.start] + e.text + content[e.end:]
	}
	return content
}
func challengeLocation() string {
	return "\n    location ^~ /.well-known/acme-challenge/ { alias " + NginxQuote(ChallengeDirectory+"/.well-known/acme-challenge/") + "; default_type text/plain; auth_basic off; allow all; }\n"
}

// PlanRenewal changes only direct certificate directives and the challenge route.
// Includes are inspected, never edited, since they can be shared by other sites.
func (s Store) PlanRenewal(name string, trust func(string) error) (RenewalPlan, error) {
	p := RenewalPlan{Name: name}
	path, enabled, err := s.file(name)
	if err != nil {
		return p, err
	}
	if !enabled {
		return p, errors.New("站点已停用，请先启用")
	}
	if err = trust(path); err != nil {
		return p, err
	}
	view, err := s.Read(name)
	if err != nil {
		return p, err
	}
	p.Original = view.Content
	base, auth, err := s.StripBasicAuth(name, view.Content)
	if err != nil {
		return p, err
	}
	p.AuthFile = auth
	nodes, err := ParseAuthConfig(base)
	if err != nil {
		return p, err
	}
	var servers []AuthDirective
	for _, n := range nodes {
		if n.Name != "server" {
			return p, errors.New("配置包含非 server 顶层指令，无法安全续签")
		}
		servers = append(servers, n)
	}
	if len(servers) == 0 {
		return p, errors.New("未找到 server 配置")
	}
	allNames := []string{}
	httpNames := []string{}
	var edits []configEdit
	var includeBudget int
	var inspectIncludes func([]AuthDirective, int, map[string]bool, bool) error
	inspectIncludes = func(ns []AuthDirective, depth int, visited map[string]bool, serverLevel bool) error {
		if depth > 12 {
			return errors.New("include 嵌套过深")
		}
		for _, n := range ns {
			switch n.Name {
			case "ssl_certificate", "ssl_certificate_key", "server", "server_name", "listen":
				return errors.New("关键 SSL 或域名指令位于 include，无法安全续签")
			case "return", "rewrite", "if":
				if serverLevel {
					return errors.New("include 包含服务器级跳转或重写，无法安全提供验证入口")
				}
			case "location":
				if strings.Contains(strings.Join(n.Args, " "), "acme-challenge") {
					return errors.New("证书验证 location 位于共享 include")
				}
			case "include":
				if len(n.Args) != 1 || strings.Contains(n.Args[0], "$") {
					return errors.New("动态 include 无法安全续签")
				}
				matches, e := filepath.Glob(s.configPath(n.Args[0]))
				if e != nil || len(matches) == 0 {
					return errors.New("include 不存在或无效")
				}
				for _, path := range matches {
					if visited[path] {
						return errors.New("include 循环引用")
					}
					if e = trust(path); e != nil {
						return e
					}
					data, e := readBounded(path, MaxConfig)
					if e != nil {
						return e
					}
					includeBudget += len(data)
					if includeBudget > 4*MaxConfig {
						return errors.New("include 超出解析限制")
					}
					children, e := ParseAuthConfig(string(data))
					if e != nil {
						return e
					}
					visited[path] = true
					if e = inspectIncludes(children, depth+1, visited, serverLevel); e != nil {
						return e
					}
					delete(visited, path)
				}
			}
			if err := inspectIncludes(n.Children, depth, visited, false); err != nil {
				return err
			}
		}
		return nil
	}
	for _, server := range servers {
		names := []string{}
		isHTTP := false
		listens := 0
		cert, key := "", ""
		for _, n := range server.Children {
			switch n.Name {
			case "server_name":
				for _, raw := range n.Args {
					d, e := NormalizeDomain(raw)
					if e != nil {
						return p, fmt.Errorf("域名 %s 不支持 HTTP 验证", raw)
					}
					names = unique(names, d)
					allNames = unique(allNames, d)
				}
			case "listen":
				listens++
				if len(n.Args) == 0 {
					return p, errors.New("监听配置无效")
				}
				endpoint := n.Args[0]
				if endpoint == "80" || strings.HasSuffix(endpoint, ":80") {
					if slices.Contains(n.Args, "ssl") {
						return p, errors.New("80 端口启用了 SSL，无法 HTTP 验证")
					}
					isHTTP = true
				}
			case "ssl_certificate":
				if len(n.Args) != 1 || cert != "" {
					return p, errors.New("不支持多组证书")
				}
				cert = s.configPath(n.Args[0])
			case "ssl_certificate_key":
				if len(n.Args) != 1 || key != "" {
					return p, errors.New("不支持多组私钥")
				}
				key = s.configPath(n.Args[0])
			}
		}
		if listens == 0 {
			isHTTP = true
		}
		if len(names) == 0 {
			return p, errors.New("server 缺少明确的域名")
		}
		if (cert == "") != (key == "") {
			return p, errors.New("证书与私钥配置不完整")
		}
		if cert != "" {
			if p.CertPath != "" && (p.CertPath != cert || p.KeyPath != key) {
				return p, errors.New("一个配置中存在多组证书，暂不支持")
			}
			p.CertPath = cert
			p.KeyPath = key
		}
		// Check includes in every context, but apply server rewrite restrictions only
		// where HTTP challenges will actually be served.
		for _, n := range server.Children {
			if n.Name == "include" {
				if e := inspectIncludes([]AuthDirective{n}, 0, map[string]bool{}, isHTTP); e != nil {
					return p, e
				}
			}
			if len(n.Children) > 0 {
				if e := inspectIncludes(n.Children, 0, map[string]bool{}, false); e != nil {
					return p, e
				}
			}
		}
		if !isHTTP {
			continue
		}
		for _, name := range names {
			httpNames = unique(httpNames, name)
		}
		found := false
		hasRootLocation := false
		for _, n := range server.Children {
			if n.Name == "location" && len(n.Args) == 1 && n.Args[0] == "/" {
				hasRootLocation = true
			}
		}
		for _, n := range server.Children {
			if n.Name == "rewrite" || n.Name == "if" {
				return p, errors.New("HTTP server 含服务器级重写或条件，无法安全提供验证入口")
			}
			if n.Name == "return" {
				if len(n.Args) != 2 || (n.Args[0] != "301" && n.Args[0] != "302" && n.Args[0] != "308") || hasRootLocation {
					return p, errors.New("HTTP server 跳转无法安全调整")
				}
				edits = append(edits, configEdit{n.Start, n.End, "location / { " + base[n.Start:n.End] + " }"})
				hasRootLocation = true
			}
			if n.Name == "location" && strings.Contains(strings.Join(n.Args, " "), "acme-challenge") {
				if found || !(len(n.Args) == 1 && n.Args[0] == authChallenge || len(n.Args) == 2 && n.Args[0] == "^~" && n.Args[1] == authChallenge) {
					return p, errors.New("证书验证 location 无法安全调整")
				}
				for _, c := range n.Children {
					switch c.Name {
					case "root", "alias", "default_type", "try_files", "auth_basic", "allow", "deny", "access_log", "log_not_found":
					default:
						return p, errors.New("证书验证 location 含自定义行为，无法安全调整")
					}
				}
				found = true
				edits = append(edits, configEdit{n.Start, n.End, challengeLocation()})
			}
		}
		if !found {
			edits = append(edits, configEdit{server.Open + 1, server.Open + 1, challengeLocation()})
		}
	}
	if p.CertPath == "" {
		return p, errors.New("未配置 SSL 证书")
	}
	if !SafeAbsolutePath(p.CertPath) || !SafeAbsolutePath(p.KeyPath) || strings.Contains(p.CertPath, "$") || strings.Contains(p.KeyPath, "$") {
		return p, errors.New("动态或不安全的证书路径无法续签")
	}
	if err = trust(p.CertPath); err != nil {
		return p, errors.New("证书文件不可读取或权限不安全")
	}
	p.Certificate = certificate(p.CertPath, time.Now())
	if p.Certificate.Error != "" {
		return p, errors.New("无法确定原证书域名：" + p.Certificate.Error)
	}
	p.Domains = append([]string{}, p.Certificate.DNSNames...)
	if len(p.Domains) == 0 {
		p.Domains = append([]string{}, allNames...)
	}
	if len(p.Domains) > 50 {
		return p, errors.New("证书域名超过 50 个")
	}
	for i, name := range p.Domains {
		d, e := NormalizeDomain(name)
		if e != nil {
			return p, errors.New("原证书包含通配符或不支持的域名，需使用 DNS 验证")
		}
		p.Domains[i] = d
		if !slices.Contains(allNames, d) {
			return p, errors.New("原证书包含本配置未绑定的域名，无法保证完整验证")
		}
	}
	for _, name := range allNames {
		if !slices.Contains(p.Domains, name) {
			return p, errors.New("站点域名与原证书覆盖范围不同，请先调整配置")
		}
	}
	p.Base = applyConfigEdits(base, edits)
	missingHTTP := []string{}
	for _, name := range p.Domains {
		if !slices.Contains(httpNames, name) {
			missingHTTP = append(missingHTTP, name)
		}
	}
	if len(missingHTTP) > 0 {
		p.Base += "\n# LNMP GoPanel HTTP certificate validation\nserver { listen 80; listen [::]:80; server_name " + strings.Join(missingHTTP, " ") + ";" + challengeLocation() + " location / { return 404; }\n}\n"
	}
	return p, nil
}
func (s Store) RenewalContent(p RenewalPlan, cert, key string) (string, string, error) {
	base := p.Base
	if cert != "" {
		nodes, err := ParseAuthConfig(base)
		if err != nil {
			return "", "", err
		}
		var edits []configEdit
		for _, server := range nodes {
			for _, n := range server.Children {
				if n.Name == "ssl_certificate" {
					edits = append(edits, configEdit{n.Start, n.End, "ssl_certificate " + NginxQuote(cert) + ";"})
				}
				if n.Name == "ssl_certificate_key" {
					edits = append(edits, configEdit{n.Start, n.End, "ssl_certificate_key " + NginxQuote(key) + ";"})
				}
			}
		}
		base = applyConfigEdits(base, edits)
	}
	content := base
	if p.AuthFile != "" {
		var err error
		content, err = s.AttachBasicAuth(p.Name, base, p.AuthFile)
		if err != nil {
			return "", "", err
		}
	}
	return content, base, nil
}

// Only this exact, isolated challenge route is exempt from whole-site auth
// inspection. Arbitrary auth_basic off directives remain administrator-owned.
func isRenewalChallenge(n AuthDirective) bool {
	if n.Name != "location" || !slices.Equal(n.Args, []string{"^~", authChallenge}) || len(n.Children) != 4 {
		return false
	}
	expected := []AuthDirective{{Name: "alias", Args: []string{ChallengeDirectory + "/.well-known/acme-challenge/"}}, {Name: "default_type", Args: []string{"text/plain"}}, {Name: "auth_basic", Args: []string{"off"}}, {Name: "allow", Args: []string{"all"}}}
	for i, c := range n.Children {
		if c.Name != expected[i].Name || !slices.Equal(c.Args, expected[i].Args) || c.Open >= 0 {
			return false
		}
	}
	return true
}
