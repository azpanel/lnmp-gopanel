// Package logs provides bounded, read-only windows over the panel's log allowlist.
package logs

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"
)

const (
	Directory        = "/home/wwwlogs"
	MySQL            = "/var/log/mysql/mariadb-slow.log"
	PHP              = "/usr/local/php/var/log/php_errors.log"
	BatchBytes int64 = 256 * 1024
	LineBytes  int64 = 64 * 1024
	MaxFiles         = 4096
	Timeout          = 8 * time.Second
)

var ErrInvalid = errors.New("日志请求无效")
var ErrChanged = errors.New("日志已轮转或截断，请回到最新日志")

type File struct {
	Path      string    `json:"path"`
	Size      int64     `json:"size"`
	Modified  time.Time `json:"modified"`
	Available bool      `json:"available"`
	Error     string    `json:"error,omitempty"`
}
type Catalog struct {
	Files    []File   `json:"files"`
	Warnings []string `json:"warnings"`
}
type Rule struct {
	Kind       string `json:"kind"`
	Action     string `json:"action"`
	Pattern    string `json:"pattern"`
	IgnoreCase bool   `json:"ignore_case"`
	Enabled    bool   `json:"enabled"`
}
type Request struct {
	Path  string `json:"path"`
	Mode  string `json:"mode"`
	Start string `json:"start,omitempty"`
	End   string `json:"end,omitempty"`
	Rules []Rule `json:"rules"`
}
type Line struct {
	Offset    string `json:"offset"`
	Text      string `json:"text"`
	Truncated bool   `json:"truncated,omitempty"`
	Partial   bool   `json:"partial,omitempty"`
}
type Window struct {
	ReplaceFrom string    `json:"replace_from"`
	Lines       []Line    `json:"lines"`
	Start       string    `json:"start"`
	End         string    `json:"end"`
	From        string    `json:"from"`
	To          string    `json:"to"`
	Size        int64     `json:"size"`
	Modified    time.Time `json:"modified"`
	HasBefore   bool      `json:"has_before"`
	HasAfter    bool      `json:"has_after"`
	Limited     bool      `json:"limited"`
}
type Reader interface {
	List(context.Context) (Catalog, error)
	Read(context.Context, Request) (Window, error)
}

func ValidPath(path string) bool {
	if path == MySQL || path == PHP {
		return true
	}
	if filepath.Dir(path) != Directory || filepath.Clean(path) != path {
		return false
	}
	name := filepath.Base(path)
	return strings.HasSuffix(name, ".log") && !strings.ContainsAny(name, "\x00/\\") && utf8.ValidString(path)
}

type matcher struct {
	rule Rule
	re   *regexp.Regexp
}

func compile(rules []Rule) ([]matcher, error) {
	if len(rules) > 20 {
		return nil, fmt.Errorf("%w：最多 20 条规则", ErrInvalid)
	}
	total := 0
	out := []matcher{}
	for i, r := range rules {
		total += len(r.Pattern)
		if r.Kind != "keyword" && r.Kind != "regex" || r.Action != "show" && r.Action != "hide" || len(r.Pattern) > 256 || total > 2048 || !utf8.ValidString(r.Pattern) {
			return nil, fmt.Errorf("%w：规则参数或长度无效", ErrInvalid)
		}
		if !r.Enabled {
			continue
		}
		if r.Pattern == "" {
			return nil, fmt.Errorf("%w：第 %d 条规则不能为空", ErrInvalid, i+1)
		}
		m := matcher{rule: r}
		if r.Kind == "regex" {
			pattern := r.Pattern
			if r.IgnoreCase {
				pattern = "(?i)" + pattern
			}
			re, e := regexp.Compile(pattern)
			if e != nil {
				return nil, fmt.Errorf("%w：第 %d 条正则无效：%s", ErrInvalid, i+1, e)
			}
			m.re = re
		}
		out = append(out, m)
	}
	return out, nil
}
func Validate(r Request) error {
	if !ValidPath(r.Path) {
		return fmt.Errorf("%w：不允许读取此路径", ErrInvalid)
	}
	if r.Mode != "tail" && r.Mode != "before" && r.Mode != "after" && r.Mode != "range" {
		return ErrInvalid
	}
	if len(r.Start) > 1024 || len(r.End) > 1024 {
		return ErrInvalid
	}
	if r.Mode == "before" || r.Mode == "range" {
		if _, e := decodeCursor(r.Start); e != nil {
			return e
		}
	}
	if r.Mode == "after" || r.Mode == "range" {
		if _, e := decodeCursor(r.End); e != nil {
			return e
		}
	}
	_, err := compile(r.Rules)
	return err
}
func visible(text string, rules []matcher) bool {
	show, hasShow := false, false
	for _, m := range rules {
		if m.rule.Action == "show" {
			hasShow = true
		}
		hit := false
		if m.re != nil {
			hit = m.re.MatchString(text)
		} else if m.rule.IgnoreCase {
			hit = strings.Contains(strings.ToLower(text), strings.ToLower(m.rule.Pattern))
		} else {
			hit = strings.Contains(text, m.rule.Pattern)
		}
		if hit && m.rule.Action == "hide" {
			return false
		}
		if hit {
			show = true
		}
	}
	return !hasShow || show
}

type cursor struct {
	Identity string `json:"i"`
	Offset   int64  `json:"o"`
	Anchor   string `json:"a"`
}

func decodeCursor(raw string) (cursor, error) {
	var c cursor
	data, e := base64.RawURLEncoding.DecodeString(raw)
	if e != nil || json.Unmarshal(data, &c) != nil || c.Identity == "" || c.Offset < 0 || len(c.Anchor) != 64 {
		return c, ErrInvalid
	}
	return c, nil
}
func anchor(f *os.File, offset int64) (string, error) {
	start := max(int64(0), offset-32)
	b := make([]byte, offset-start)
	if _, e := f.ReadAt(b, start); e != nil && e != io.EOF {
		return "", e
	}
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:]), nil
}
func makeCursor(f *os.File, identity string, offset int64) (string, error) {
	a, err := anchor(f, offset)
	if err != nil {
		return "", err
	}
	b, _ := json.Marshal(cursor{Identity: identity, Offset: offset, Anchor: a})
	return base64.RawURLEncoding.EncodeToString(b), nil
}
func checkCursor(f *os.File, raw, identity string, size int64) (int64, error) {
	c, err := decodeCursor(raw)
	if err != nil {
		return 0, err
	}
	if c.Identity != identity || c.Offset > size {
		return 0, ErrChanged
	}
	a, err := anchor(f, c.Offset)
	if err != nil {
		return 0, err
	}
	if a != c.Anchor {
		return 0, ErrChanged
	}
	return c.Offset, nil
}

// ReadFile bounds the data read by BatchBytes + 2*LineBytes + 2 bytes. Lookaround repairs
// line boundaries across pages; offsets let clients replace a growing EOF line.
func ReadFile(ctx context.Context, f *os.File, req Request) (Window, error) {
	out := Window{Lines: []Line{}}
	if err := Validate(req); err != nil {
		return out, err
	}
	rules, _ := compile(req.Rules)
	info, err := f.Stat()
	if err != nil {
		return out, err
	}
	if !info.Mode().IsRegular() {
		return out, errors.New("仅支持普通日志文件")
	}
	identity := fileIdentity(info)
	size := info.Size()
	start, end := max(int64(0), size-BatchBytes), size
	switch req.Mode {
	case "before":
		end, err = checkCursor(f, req.Start, identity, size)
		start = max(int64(0), end-BatchBytes)
	case "after":
		start, err = checkCursor(f, req.End, identity, size)
		end = min(size, start+BatchBytes)
	case "range":
		start, err = checkCursor(f, req.Start, identity, size)
		if err == nil {
			end, err = checkCursor(f, req.End, identity, size)
		}
	}
	if err != nil {
		return out, err
	}
	if start > end || end-start > BatchBytes {
		return out, ErrInvalid
	}
	if err = ctx.Err(); err != nil {
		return out, err
	}
	lo, hi := max(int64(0), start-LineBytes-1), min(size, end+LineBytes+1)
	if start == end {
		lo, hi = start, end
	}
	data := make([]byte, hi-lo)
	n, err := f.ReadAt(data, lo)
	if err != nil && err != io.EOF {
		return out, err
	}
	if n != len(data) {
		return out, ErrChanged
	}
	// Drop the unknown prefix if lookbehind starts inside a physical line.
	pos := 0
	ring := 0
	replaceFrom := start
	if lo > 0 && start < end {
		p := bytes.IndexByte(data, '\n')
		if p < 0 {
			pos = len(data)
			out.Limited = true
		} else {
			pos = p + 1
			if lo+int64(p) >= start {
				out.Limited = true
			}
		}
	}
	for pos < len(data) && start < end {
		if err := ctx.Err(); err != nil {
			return out, err
		}
		relative := bytes.IndexByte(data[pos:], '\n')
		finish := len(data)
		complete := relative >= 0
		if complete {
			finish = pos + relative
		}
		lineStart, lineEnd := lo+int64(pos), lo+int64(finish)
		if complete {
			lineEnd++
		}
		if lineStart >= end {
			break
		}
		if lineEnd > start {
			if lineStart < replaceFrom {
				replaceFrom = lineStart
			}
			length := finish - pos
			truncated := int64(length) > LineBytes || !complete && hi < size
			if truncated {
				out.Limited = true
			}
			cut := pos + min(length, int(LineBytes))
			if truncated {
				// Do not split a valid UTF-8 rune at the display limit. Invalid input
				// elsewhere is still replaced, rather than interpreted as markup.
				for dropped := 0; cut < len(data) && cut > pos && dropped < 3 && !utf8.RuneStart(data[cut]); dropped++ {
					cut--
				}
			}
			text := strings.ToValidUTF8(string(data[pos:cut]), "�")
			text = strings.TrimSuffix(text, "\r")
			if visible(text, rules) {
				row := Line{Offset: strconv.FormatInt(lineStart, 10), Text: text, Truncated: truncated, Partial: !complete && !truncated}
				if len(out.Lines) < 4000 {
					out.Lines = append(out.Lines, row)
				} else if req.Mode == "tail" || req.Mode == "before" {
					out.Lines[ring] = row
					ring = (ring + 1) % 4000
					first, _ := strconv.ParseInt(out.Lines[ring].Offset, 10, 64)
					start = max(start, first)
					replaceFrom = start
				} else {
					end = lineStart
					break
				}
			}
		}
		pos = finish + 1
	}
	// A long line with no discoverable start is deliberately skipped, never
	// rescanned without a bound. Its first 64 KiB was returned by its first page.

	if ring > 0 {
		out.Lines = append(out.Lines[ring:], out.Lines[:ring]...)
	}
	out.ReplaceFrom = strconv.FormatInt(replaceFrom, 10)
	out.Start, err = makeCursor(f, identity, start)
	if err != nil {
		return out, err
	}
	out.End, err = makeCursor(f, identity, end)
	if err != nil {
		return out, err
	}
	latest, err := f.Stat()
	if err != nil {
		return out, err
	}
	if latest.Size() < size {
		return out, ErrChanged
	}
	out.From, out.To = strconv.FormatInt(start, 10), strconv.FormatInt(end, 10)
	out.Size, out.Modified = size, info.ModTime()
	out.HasBefore, out.HasAfter = start > 0, end < size
	return out, nil
}
