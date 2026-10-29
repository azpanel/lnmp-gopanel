package logs

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"unicode/utf8"
)

func fixture(t *testing.T, content string) *os.File {
	t.Helper()
	f, err := os.CreateTemp(t.TempDir(), "reader-*.log")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { f.Close() })
	if _, err = f.WriteString(content); err != nil {
		t.Fatal(err)
	}
	return f
}
func read(t *testing.T, f *os.File, req Request) Window {
	t.Helper()
	req.Path = Directory + "/test.log"
	out, err := ReadFile(context.Background(), f, req)
	if err != nil {
		t.Fatal(err)
	}
	return out
}
func TestWindowsAndPartialReplacement(t *testing.T) {
	content := strings.Repeat("中文请求 content\n", 40000) + "pending"
	f := fixture(t, content)
	tail := read(t, f, Request{Mode: "tail"})
	if !tail.HasBefore || tail.HasAfter || len(tail.Lines) > 4000 {
		t.Fatalf("bad window: %+v", tail)
	}
	if !tail.Lines[len(tail.Lines)-1].Partial {
		t.Fatal("expected partial EOF")
	}
	old := read(t, f, Request{Mode: "before", Start: tail.Start})
	if old.To != tail.From {
		t.Fatal("history has a gap")
	}
	for _, row := range append(old.Lines, tail.Lines...) {
		if !utf8.ValidString(row.Text) || strings.ContainsRune(row.Text, '�') {
			t.Fatal("broken UTF-8 boundary")
		}
	}
	if _, err := f.WriteString(" 中文\nnew\n"); err != nil {
		t.Fatal(err)
	}
	next := read(t, f, Request{Mode: "after", End: tail.End})
	if len(next.Lines) != 2 || next.Lines[0].Text != "pending 中文" || next.Lines[0].Partial || next.Lines[1].Text != "new" {
		t.Fatalf("bad append: %+v", next.Lines)
	}
	if next.ReplaceFrom != tail.Lines[len(tail.Lines)-1].Offset {
		t.Fatal("partial replacement anchor missing")
	}
	again := read(t, f, Request{Mode: "after", End: next.End})
	if len(again.Lines) != 0 || again.From != again.To {
		t.Fatal("duplicated EOF")
	}
}
func TestFilteredCursorAndRules(t *testing.T) {
	f := fixture(t, "INFO hello\nERROR one\nwarn two\nERROR secret\n")
	rules := []Rule{{Kind: "regex", Action: "show", Pattern: "error|warn", IgnoreCase: true, Enabled: true}, {Kind: "keyword", Action: "hide", Pattern: "secret", Enabled: true}}
	out := read(t, f, Request{Mode: "tail", Rules: rules})
	if len(out.Lines) != 2 || out.Lines[0].Text != "ERROR one" || out.Lines[1].Text != "warn two" {
		t.Fatalf("bad rules: %+v", out.Lines)
	}
	hidden := read(t, f, Request{Mode: "range", Start: out.Start, End: out.End, Rules: []Rule{{Kind: "keyword", Action: "show", Pattern: "absent", Enabled: true}}})
	if len(hidden.Lines) != 0 || hidden.End != out.End {
		t.Fatal("filtered window must advance raw cursor")
	}
	restored := read(t, f, Request{Mode: "range", Start: out.Start, End: out.End})
	if len(restored.Lines) != 4 {
		t.Fatal("refilter failed")
	}
	hideOnly := read(t, f, Request{Mode: "tail", Rules: rules[1:]})
	if len(hideOnly.Lines) != 3 {
		t.Fatal("hide-only rules")
	}
	invalid := Request{Path: Directory + "/test.log", Mode: "tail", Rules: []Rule{{Kind: "regex", Action: "show", Pattern: "(?=bad)", Enabled: true}}}
	if !errors.Is(Validate(invalid), ErrInvalid) {
		t.Fatal("unsupported regex accepted")
	}
}
func TestTinyLinesAndPagination(t *testing.T) {
	f := fixture(t, strings.Repeat("\n", int(BatchBytes)*2))
	tail := read(t, f, Request{Mode: "tail"})
	if len(tail.Lines) != 4000 || tail.To != strconv.Itoa(int(BatchBytes)*2) {
		t.Fatal("tail should keep most recent lines")
	}
	before := read(t, f, Request{Mode: "before", Start: tail.Start})
	if len(before.Lines) != 4000 || before.To != tail.From {
		t.Fatal("backward pagination gap")
	}
	// Walk forward from the beginning, proving every line is reachable even
	// when the row limit is hit much sooner than the byte limit.
	first, err := makeCursor(f, fileIdentity(mustStat(t, f)), 0)
	if err != nil {
		t.Fatal(err)
	}
	seen := 0
	for seen < int(BatchBytes)*2 {
		page := read(t, f, Request{Mode: "after", End: first})
		if page.Start == page.End {
			t.Fatal("cursor stuck")
		}
		seen += len(page.Lines)
		first = page.End
	}
	if seen != int(BatchBytes)*2 {
		t.Fatal("lines skipped", seen)
	}
}
func mustStat(t *testing.T, f *os.File) os.FileInfo {
	t.Helper()
	info, err := f.Stat()
	if err != nil {
		t.Fatal(err)
	}
	return info
}
func TestLongLineAndEmptyFile(t *testing.T) {
	empty := fixture(t, "")
	out := read(t, empty, Request{Mode: "tail"})
	if out.From != "0" || out.To != "0" || len(out.Lines) != 0 {
		t.Fatal("empty file")
	}
	f := fixture(t, strings.Repeat("x", int(LineBytes)+500)+"\nok\n")
	out = read(t, f, Request{Mode: "tail"})
	if len(out.Lines) != 2 || !out.Lines[0].Truncated || len(out.Lines[0].Text) != int(LineBytes) || !out.Limited {
		t.Fatal("long line not bounded")
	}
}
func TestSparseFileBoundedRead(t *testing.T) {
	f := fixture(t, "")
	const size = int64(32) * 1024 * 1024 * 1024
	if err := f.Truncate(size); err != nil {
		t.Fatal(err)
	}
	if _, err := f.WriteAt([]byte("\nlast\n"), size-6); err != nil {
		t.Fatal(err)
	}
	out := read(t, f, Request{Mode: "tail"})
	if out.Size != size || len(out.Lines) != 1 || out.Lines[0].Text != "last" {
		t.Fatal("failed sparse tail")
	}
	from, _ := strconv.ParseInt(out.From, 10, 64)
	if size-from > BatchBytes {
		t.Fatal("unbounded window")
	}
}
func TestRotationTruncationAndInvalidCursors(t *testing.T) {
	f := fixture(t, "before\n")
	old := read(t, f, Request{Mode: "tail"})
	replacement := fixture(t, "after\n")
	for _, file := range []*os.File{replacement, f} {
		if file == f {
			if err := f.Truncate(0); err != nil {
				t.Fatal(err)
			}
			if _, err := f.WriteAt([]byte("other!\n"), 0); err != nil {
				t.Fatal(err)
			}
		}
		_, err := ReadFile(context.Background(), file, Request{Path: Directory + "/test.log", Mode: "after", End: old.End})
		if !errors.Is(err, ErrChanged) {
			t.Fatal("change not detected", err)
		}
	}
	for _, raw := range []string{"", "not-base64", "e30"} {
		if _, err := decodeCursor(raw); !errors.Is(err, ErrInvalid) {
			t.Fatal("bad cursor accepted")
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := ReadFile(ctx, f, Request{Path: Directory + "/test.log", Mode: "tail"}); !errors.Is(err, context.Canceled) {
		t.Fatal("cancellation ignored")
	}
}
func TestAllowlist(t *testing.T) {
	for _, path := range []string{Directory + "/a.log", MySQL, PHP} {
		if !BuiltinPath(path) {
			t.Fatal(path)
		}
	}
	for _, path := range []string{"/etc/shadow", Directory + "/../shadow.log", Directory + "/nested/a.log", Directory + "/a.log.1", Directory + "//a.log", Directory + "/a\x00.log"} {
		if BuiltinPath(path) {
			t.Fatal(path)
		}
	}
	for _, mode := range []string{"shell", "", "download"} {
		if Validate(Request{Path: PHP, Mode: mode}) == nil {
			t.Fatal(mode)
		}
	}
}
func TestAllPhysicalLinesAcrossPages(t *testing.T) {
	var source strings.Builder
	for i := 0; i < 10000; i++ {
		fmt.Fprintf(&source, "%05d %s\n", i, strings.Repeat("字", i%60))
	}
	f := fixture(t, source.String())
	page := read(t, f, Request{Mode: "tail"})
	seen := map[string]string{}
	for {
		for _, line := range page.Lines {
			seen[line.Offset] = line.Text
		}
		if !page.HasBefore {
			break
		}
		page = read(t, f, Request{Mode: "before", Start: page.Start})
	}
	if len(seen) != 10000 {
		t.Fatalf("history lost lines: %d", len(seen))
	}
	for offset, text := range seen {
		n, _ := strconv.Atoi(offset)
		if !strings.HasPrefix(source.String()[n:], text+"\n") {
			t.Fatalf("wrong line at %s", offset)
		}
	}
}
func TestPathIsNotDerivedFromTempFile(t *testing.T) {
	// Even an existing regular text file cannot bypass the sensitive-name policy.
	path := filepath.Join(t.TempDir(), ".env")
	if err := os.WriteFile(path, []byte("TOKEN=private"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := (Store{}).Read(context.Background(), Request{Path: path, Mode: "tail"}); !errors.Is(err, ErrInvalid) {
		t.Fatal(err)
	}
}

func TestLongLineSkippedPrefixHasNotice(t *testing.T) {
	f := fixture(t, strings.Repeat("x", int(BatchBytes)*3)+"\nlast\n")
	out := read(t, f, Request{Mode: "tail"})
	if !out.Limited || len(out.Lines) != 1 || out.Lines[0].Text != "last" {
		t.Fatal("missing skipped-line notice")
	}
	f = fixture(t, strings.Repeat("字", int(LineBytes/3)+20)+"\n")
	out = read(t, f, Request{Mode: "tail"})
	if len(out.Lines) != 1 || strings.ContainsRune(out.Lines[0].Text, '�') {
		t.Fatal("display limit split UTF-8")
	}
}
