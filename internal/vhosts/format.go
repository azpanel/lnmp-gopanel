package vhosts

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"strings"
	"time"
)

type FormatInput struct {
	Name     string `json:"name"`
	Revision string `json:"revision"`
}

func (r FormatInput) Validate() error {
	if !ValidName(r.Name) {
		return errors.New("配置文件名无效")
	}
	if revision, err := hex.DecodeString(r.Revision); err != nil || len(revision) != sha256.Size {
		return errors.New("配置版本无效，请重新读取配置")
	}
	return nil
}

type FormatResult struct {
	ConfigView
	Changed bool   `json:"changed"`
	Message string `json:"message"`
}

func ConfigRevision(view ConfigView) string {
	sum := sha256.Sum256([]byte(view.Filename + "\x00" + view.Content))
	return hex.EncodeToString(sum[:])
}

type FormatBackend interface {
	FormatVhost(context.Context, FormatInput) (FormatResult, error)
}

func (m *Manager) Format(r FormatInput) (FormatResult, error) {
	if err := r.Validate(); err != nil {
		return FormatResult{}, err
	}
	backend, ok := m.backend.(FormatBackend)
	if !ok {
		return FormatResult{}, errors.New("配置格式化未配置，请升级辅助程序")
	}
	if !m.actions.TryLock() {
		return FormatResult{}, errors.New("另一个站点操作正在执行，请稍后再试")
	}
	defer m.actions.Unlock()
	ctx, cancel := context.WithTimeout(m.ctx, 40*time.Second)
	defer cancel()
	result, err := backend.FormatVhost(ctx, r)
	if err == nil && result.Changed {
		m.Invalidate()
	}
	return result, err
}

// Formatting may also update a disabled file; it never renames or enables it.
// No reload is necessary because FormatNginx verifies that directives are equal.
func (s Store) SaveFormatted(ctx context.Context, name string, content, expected []byte, check func(context.Context) error) error {
	return s.install(ctx, name, content, expected, check, func(context.Context) error { return nil }, true)
}

// FormatNginx uses parser positions for structural punctuation, so braces and
// semicolons in regexes, variables and quoted strings are never interpreted as
// block boundaries. All token bytes and comment text are retained.
func FormatNginx(content string) (string, error) {
	nodes, err := ParseAuthConfig(content)
	if err != nil {
		return "", errors.New("此配置包含无法安全格式化的语法，未修改文件：" + err.Error())
	}
	marks := map[int]byte{}
	var collect func([]AuthDirective, int) error
	collect = func(nodes []AuthDirective, depth int) error {
		if depth > 128 {
			return errors.New("配置嵌套过深，未修改文件")
		}
		for _, n := range nodes {
			marks[n.End-1] = content[n.End-1]
			if n.Open >= 0 {
				marks[n.Open] = '{'
				if err := collect(n.Children, depth+1); err != nil {
					return err
				}
			}
		}
		return nil
	}
	if err := collect(nodes, 0); err != nil {
		return "", err
	}
	newline := "\n"
	if strings.Contains(content, "\r\n") {
		newline = "\r\n"
	}
	var out, line strings.Builder
	depth, lineDepth := 0, 0
	space, ended, blank := false, false, false
	flush := func() {
		if line.Len() > 0 {
			out.WriteString(strings.Repeat("    ", lineDepth))
			out.WriteString(line.String())
			out.WriteString(newline)
			line.Reset()
			blank = false
		}
		space, ended = false, false
	}
	write := func(value string) {
		if ended {
			flush()
		}
		if line.Len() == 0 {
			lineDepth = depth
		} else if space {
			line.WriteByte(' ')
		}
		space = false
		line.WriteString(value)
	}
	for i := 0; i < len(content); {
		if out.Len()+line.Len() > MaxConfig {
			return "", errors.New("格式化后配置超出 1 MiB 限制")
		}
		c := content[i]
		switch {
		case c == ' ' || c == '\t' || c == '\r':
			space = true
			i++
		case c == '\n':
			if line.Len() > 0 {
				flush()
			} else if out.Len() > 0 && !blank {
				out.WriteString(newline)
				blank = true
			}
			i++
		case c == '#':
			end := strings.IndexByte(content[i:], '\n')
			if end < 0 {
				end = len(content) - i
			}
			// Inline comments stay with the preceding directive or brace.
			ended = false
			space = line.Len() > 0
			write(strings.TrimSuffix(content[i:i+end], "\r"))
			flush()
			i += end
			if i < len(content) {
				i++
			}
		case c == '\'' || c == '"':
			end := i + 1
			for end < len(content) {
				if content[end] == '\\' {
					end += 2
					continue
				}
				if content[end] == c {
					end++
					break
				}
				end++
			}
			if end > len(content) {
				return "", errors.New("配置字符串转义无效")
			}
			write(content[i:end])
			i = end
		case c == '\\' && i+1 < len(content):
			write(content[i : i+2])
			i += 2
		case marks[i] == '{':
			space = line.Len() > 0
			write("{")
			ended = true
			depth++
			i++
		case marks[i] == '}':
			flush()
			depth--
			write("}")
			ended = true
			i++
		case marks[i] == ';':
			space = false
			write(";")
			ended = true
			i++
		default:
			write(content[i : i+1])
			i++
		}
	}
	flush()
	formatted := strings.TrimRight(out.String(), "\r\n") + newline
	if len(formatted) > MaxConfig {
		return "", errors.New("格式化后配置超出 1 MiB 限制")
	}
	again, err := ParseAuthConfig(formatted)
	if err != nil || !sameNginxDirectives(nodes, again) {
		return "", errors.New("无法保证格式化前后指令一致，未修改文件")
	}
	return formatted, nil
}

func sameNginxDirectives(a, b []AuthDirective) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i].Name != b[i].Name || (a[i].Open >= 0) != (b[i].Open >= 0) || len(a[i].Args) != len(b[i].Args) || !sameNginxDirectives(a[i].Children, b[i].Children) {
			return false
		}
		for j := range a[i].Args {
			if a[i].Args[j] != b[i].Args[j] {
				return false
			}
		}
	}
	return true
}
