package vhosts

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode"

	"github.com/tufanbarisyildirim/gonginx/parser"
)

type BasicAuthInput struct {
	Name     string `json:"name"`
	Username string `json:"username"`
	Password string `json:"password"`
}
type BasicAuthState struct {
	Name        string `json:"name"`
	Status      string `json:"status"` // disabled, enabled, external, unknown
	Managed     bool   `json:"managed"`
	Username    string `json:"username,omitempty"`
	SiteEnabled bool   `json:"site_enabled"`
	Message     string `json:"message,omitempty"`
}

var basicUsername = regexp.MustCompile(`^[a-zA-Z0-9._-]{1,64}$`)

func (r BasicAuthInput) Validate() error {
	if !ValidName(r.Name) {
		return errors.New("配置文件名无效")
	}
	if !basicUsername.MatchString(r.Username) {
		return errors.New("用户名需为 1–64 位字母、数字、点、下划线或连字符")
	}
	if len(r.Password) < 8 || len(r.Password) > 256 || strings.IndexFunc(r.Password, unicode.IsControl) >= 0 {
		return errors.New("密码需为 8–256 字节，不能包含控制字符")
	}
	return nil
}

type BasicAuthBackend interface {
	ReadVhostBasicAuth(context.Context, string) (BasicAuthState, error)
	SetVhostBasicAuth(context.Context, BasicAuthInput) (BasicAuthState, error)
	DisableVhostBasicAuth(context.Context, string) (BasicAuthState, error)
}

func (m *Manager) BasicAuth(ctx context.Context, name string) (BasicAuthState, error) {
	if !ValidName(name) {
		return BasicAuthState{}, errors.New("配置文件名无效")
	}
	b, ok := m.backend.(BasicAuthBackend)
	if !ok {
		return BasicAuthState{}, errors.New("访问认证管理未配置，请升级辅助程序")
	}
	return b.ReadVhostBasicAuth(ctx, name)
}
func (m *Manager) ChangeBasicAuth(r BasicAuthInput, disable bool) (BasicAuthState, error) {
	if !ValidName(r.Name) {
		return BasicAuthState{}, errors.New("配置文件名无效")
	}
	if !disable {
		if err := r.Validate(); err != nil {
			return BasicAuthState{}, err
		}
	}
	b, ok := m.backend.(BasicAuthBackend)
	if !ok {
		return BasicAuthState{}, errors.New("访问认证管理未配置，请升级辅助程序")
	}
	if !m.actions.TryLock() {
		return BasicAuthState{}, errors.New("另一个站点操作正在执行，请稍后再试")
	}
	defer m.actions.Unlock()
	ctx, cancel := context.WithTimeout(m.ctx, 45*time.Second)
	defer cancel()
	var state BasicAuthState
	var err error
	if disable {
		state, err = b.DisableVhostBasicAuth(ctx, r.Name)
	} else {
		state, err = b.SetVhostBasicAuth(ctx, r)
	}
	if err == nil {
		m.Invalidate()
	}
	return state, err
}

const authBegin = "\n# BEGIN LNMP GOPANEL BASIC AUTH\n"
const authEnd = "# END LNMP GOPANEL BASIC AUTH\n"
const authChallenge = "/.well-known/acme-challenge/"

func (s Store) BasicAuthDirectory() string { return filepath.Join(s.Prefix, "conf", "gopanel-auth") }
func BasicAuthKey(name string) string {
	h := sha256.Sum256([]byte(name))
	return hex.EncodeToString(h[:])
}
func (s Store) ValidBasicAuthFile(name, path string) bool {
	prefix := filepath.Join(s.BasicAuthDirectory(), BasicAuthKey(name)+"-")
	if !strings.HasPrefix(path, prefix) || !strings.HasSuffix(path, ".htpasswd") {
		return false
	}
	suffix := strings.TrimSuffix(strings.TrimPrefix(path, prefix), ".htpasswd")
	_, err := hex.DecodeString(suffix)
	return len(suffix) == 32 && err == nil
}
func authBlock(body string) string { return authBegin + body + authEnd }
func authServerBody(path string) string {
	return "auth_basic \"Restricted\";\nauth_basic_user_file " + NginxQuote(path) + ";\n"
}
func authFallback() string { return "location ^~ " + authChallenge + " { auth_basic off; }\n" }

// Directive byte positions permit surgical insertions without reformatting the
// administrator's configuration. Lua and other opaque blocks fail closed.
type AuthDirective struct {
	Name     string
	Args     []string
	Start    int
	Open     int
	End      int
	Children []AuthDirective
}

func ParseAuthConfig(content string) ([]AuthDirective, error) {
	if len(content) > MaxConfig || strings.ContainsRune(content, 0) {
		return nil, errors.New("配置过大或包含空字符")
	}
	if _, err := parser.NewStringParser(content).Parse(); err != nil {
		return nil, errors.New("Nginx 配置解析失败")
	}
	i := 0
	var parse func(bool) ([]AuthDirective, error)
	skip := func() {
		for i < len(content) {
			if strings.ContainsRune(" \t\r\n", rune(content[i])) {
				i++
				continue
			}
			if content[i] == '#' {
				for i < len(content) && content[i] != '\n' {
					i++
				}
				continue
			}
			break
		}
	}
	word := func() (string, error) {
		var out strings.Builder
		quote := byte(0)
		for i < len(content) {
			c := content[i]
			if c == '\\' {
				if i+1 >= len(content) {
					return "", errors.New("配置转义不完整")
				}
				out.WriteByte(content[i+1])
				i += 2
				continue
			}
			if quote != 0 {
				i++
				if c == quote {
					quote = 0
				} else {
					out.WriteByte(c)
				}
				continue
			}
			if c == '\'' || c == '"' {
				quote = c
				i++
				continue
			}
			if c == '$' && i+1 < len(content) && content[i+1] == '{' {
				end := strings.IndexByte(content[i+2:], '}')
				if end < 0 {
					return "", errors.New("配置变量不完整")
				}
				out.WriteString(content[i : i+end+3])
				i += end + 3
				continue
			}
			if strings.ContainsRune(" \t\r\n;{}#", rune(c)) {
				break
			}
			out.WriteByte(c)
			i++
		}
		if quote != 0 {
			return "", errors.New("配置引号不完整")
		}
		return out.String(), nil
	}
	parse = func(nested bool) ([]AuthDirective, error) {
		var nodes []AuthDirective
		for {
			skip()
			if i == len(content) {
				if nested {
					return nil, errors.New("配置块不完整")
				}
				return nodes, nil
			}
			if content[i] == '}' {
				if !nested {
					return nil, errors.New("多余配置块结束符")
				}
				i++
				return nodes, nil
			}
			start := i
			name, err := word()
			if err != nil || i == start {
				return nil, errors.New("无法安全定位配置指令")
			}
			n := AuthDirective{Name: name, Start: start, Open: -1}
			for {
				skip()
				if i >= len(content) {
					return nil, errors.New("配置指令不完整")
				}
				c := content[i]
				if c == ';' {
					i++
					n.End = i
					break
				}
				if c == '{' {
					if strings.Contains(name, "lua") || strings.HasSuffix(name, "_by_js") {
						return nil, errors.New("含脚本配置块，无法安全修改认证")
					}
					n.Open = i
					i++
					n.Children, err = parse(true)
					if err != nil {
						return nil, err
					}
					n.End = i
					break
				}
				start := i
				arg, e := word()
				if e != nil || i == start {
					return nil, errors.New("无法安全定位配置参数")
				}
				n.Args = append(n.Args, arg)
			}
			nodes = append(nodes, n)
		}
	}
	return parse(false)
}

// StripBasicAuth accepts only the exact generated fragments and verifies their
// placement by regenerating them. A marker alone never proves ownership.
func (s Store) StripBasicAuth(name, content string) (string, string, error) {
	base := content
	credential := ""
	count := 0
	for {
		start := strings.Index(base, authBegin)
		if start < 0 {
			break
		}
		end := strings.Index(base[start+len(authBegin):], authEnd)
		if end < 0 {
			return "", "", errors.New("认证管理标记不完整")
		}
		end += start + len(authBegin)
		body := base[start+len(authBegin) : end]
		if body != "auth_basic off;\n" {
			nodes, err := ParseAuthConfig(body)
			if err != nil || len(nodes) < 2 || nodes[0].Name != "auth_basic" || nodes[1].Name != "auth_basic_user_file" || len(nodes[1].Args) != 1 {
				return "", "", errors.New("认证管理片段已被修改")
			}
			path := nodes[1].Args[0]
			if !s.ValidBasicAuthFile(name, path) || (credential != "" && credential != path) || (body != authServerBody(path) && body != authServerBody(path)+authFallback()) {
				return "", "", errors.New("认证管理片段无效或凭证路径不匹配")
			}
			credential = path
		}
		base = base[:start] + base[end+len(authEnd):]
		count++
	}
	if strings.Contains(base, "BEGIN LNMP GOPANEL BASIC AUTH") || strings.Contains(base, "END LNMP GOPANEL BASIC AUTH") {
		return "", "", errors.New("认证管理标记异常")
	}
	if count == 0 {
		return base, "", nil
	}
	if credential == "" {
		return "", "", errors.New("认证管理片段缺少站点凭证")
	}
	regenerated, err := s.AttachBasicAuth(name, base, credential)
	if err != nil || regenerated != content {
		return "", "", errors.New("认证管理片段位置或数量异常")
	}
	return base, credential, nil
}
func (s Store) AttachBasicAuth(name, base, credential string) (string, error) {
	if !s.ValidBasicAuthFile(name, credential) {
		return "", errors.New("凭证路径无效")
	}
	nodes, err := ParseAuthConfig(base)
	if err != nil {
		return "", err
	}
	type insertion struct {
		pos  int
		text string
	}
	var edits []insertion
	for _, n := range nodes {
		if n.Name != "server" {
			continue
		}
		if n.Open < 0 {
			return "", errors.New("server 配置块无效")
		}
		var challenge *AuthDirective
		for i := range n.Children {
			c := &n.Children[i]
			if c.Name == "location" && strings.Contains(strings.Join(c.Args, " "), "acme-challenge") {
				if challenge != nil || !(len(c.Args) == 1 && c.Args[0] == authChallenge || len(c.Args) == 2 && c.Args[0] == "^~" && c.Args[1] == authChallenge) {
					return "", errors.New("证书验证 location 无法安全设置免认证")
				}
				challenge = c
			}
		}
		body := authServerBody(credential)
		if challenge == nil {
			body += authFallback()
		} else {
			existingOff := false
			for _, c := range challenge.Children {
				if c.Name != "auth_basic" {
					continue
				}
				if existingOff || len(c.Args) != 1 || c.Args[0] != "off" {
					return "", errors.New("证书验证 location 的认证配置冲突")
				}
				existingOff = true
			}
			if !existingOff {
				edits = append(edits, insertion{challenge.Open + 1, authBlock("auth_basic off;\n")})
			}
		}
		edits = append(edits, insertion{n.Open + 1, authBlock(body)})
	}
	if len(edits) == 0 {
		return "", errors.New("配置中没有可管理的 server 块")
	}
	sort.Slice(edits, func(i, j int) bool { return edits[i].pos > edits[j].pos })
	for _, e := range edits {
		base = base[:e.pos] + e.text + base[e.pos:]
	}
	if len(base) > MaxConfig {
		return "", errors.New("认证配置超过 1 MiB 限制")
	}
	return base, nil
}

// InspectAuthDirectives expands static includes without modifying them. Global
// inspection skips server blocks, so another site's auth is not a conflict.
func (s Store) InspectAuthDirectives(ctx context.Context, content string, global bool, read func(string) (string, error)) (string, error) {
	budget := 0
	stack := map[string]bool{}
	var visit func(string, bool, bool, int) (string, error)
	visit = func(data string, global, included bool, depth int) (string, error) {
		if err := ctx.Err(); err != nil {
			return "", err
		}
		budget += len(data)
		if depth > 12 || budget > 4*MaxConfig {
			return "", errors.New("include 超过安全解析限制")
		}
		nodes, err := ParseAuthConfig(data)
		if err != nil {
			return "", err
		}
		var walk func([]AuthDirective) (string, error)
		walk = func(nodes []AuthDirective) (string, error) {
			for _, n := range nodes {
				if isRenewalChallenge(n) {
					continue
				}
				if global && n.Name == "server" {
					continue
				}
				if n.Name == "auth_basic" || n.Name == "auth_basic_user_file" {
					return "检测到已有 auth_basic 配置，请先由管理员处理", nil
				}
				if strings.Contains(n.Name, "_by_lua") {
					return "", errors.New("含 Lua 执行指令，无法保证整站认证")
				}
				if n.Name == "return" && len(n.Args) > 0 {
					code, e := strconv.Atoi(n.Args[0])
					if e == nil && code >= 200 && code < 300 {
						return "", errors.New("含认证阶段之前执行的 return 2xx，无法保证整站认证")
					}
				}
				if n.Name == "satisfy" && len(n.Args) == 1 && n.Args[0] == "any" {
					return "检测到 satisfy any，无法保证整站认证", nil
				}
				if included && !global && n.Name == "server" {
					return "", errors.New("server 位于共享 include，无法安全修改认证")
				}
				if included && !global && n.Name == "location" && strings.Contains(strings.Join(n.Args, " "), "acme-challenge") {
					return "", errors.New("证书验证 location 位于共享 include，无法安全修改")
				}
				if n.Name == "include" {
					if len(n.Args) != 1 || strings.Contains(n.Args[0], "$") {
						return "", errors.New("动态 include 无法确认认证状态")
					}
					path := n.Args[0]
					if !filepath.IsAbs(path) {
						path = filepath.Join(s.Prefix, "conf", path)
					}
					matches, e := filepath.Glob(path)
					if e != nil {
						return "", errors.New("include 路径无效")
					}
					if len(matches) == 0 && !strings.ContainsAny(path, "*?[") {
						return "", errors.New("include 文件不存在")
					}
					for _, p := range matches {
						if stack[p] {
							return "", errors.New("include 循环引用")
						}
						stack[p] = true
						text, e := read(p)
						if e != nil {
							return "", e
						}
						conflict, e := visit(text, global, true, depth+1)
						delete(stack, p)
						if conflict != "" || e != nil {
							return conflict, e
						}
					}
				}
				conflict, e := walk(n.Children)
				if conflict != "" || e != nil {
					return conflict, e
				}
			}
			return "", nil
		}
		return walk(nodes)
	}
	conflict, err := visit(content, global, false, 0)
	if err != nil {
		return "", fmt.Errorf("无法确认认证状态：%w", err)
	}
	return conflict, nil
}
