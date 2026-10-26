//go:build linux

package storage

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/shirou/gopsutil/v4/disk"
)

func TestScanFilesPreservesNamesAndDoesNotFollowSymlinks(t *testing.T) {
	root := t.TempDir()
	names := []string{"file with spaces", "file\nwith newline", "normal"}
	for i, name := range names {
		if err := os.WriteFile(filepath.Join(root, name), make([]byte, (i+1)*10), 0600); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.Symlink(filepath.Join(root, "normal"), filepath.Join(root, "symlink")); err != nil {
		t.Fatal(err)
	}
	var result Progress
	err := ScanFiles(context.Background(), root, func(p Progress) { result = p })
	if err != nil || !result.Complete || result.Partial || result.Scanned != 3 || result.TotalBytes != 60 {
		t.Fatalf("%+v %v", result, err)
	}
	for i, f := range result.Files {
		if f.Size != int64((3-i)*10) || f.Path != filepath.Join(root, names[2-i]) {
			t.Fatalf("rank %d: %+v", i, f)
		}
	}
}
func TestSparseFileUsesApparentSize(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "sparse")
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := f.Truncate(1000000000); err != nil {
		f.Close()
		t.Fatal(err)
	}
	f.Close()
	var result Progress
	if err := ScanFiles(context.Background(), root, func(p Progress) { result = p }); err != nil {
		t.Fatal(err)
	}
	if len(result.Files) != 1 || result.Files[0].Size != 1000000000 {
		t.Fatal(result)
	}
}
func TestFindArgsExcludesOtherMountsAndEscapesPatterns(t *testing.T) {
	args := findArgs("/", []disk.PartitionStat{{Mountpoint: "/"}, {Mountpoint: "/proc"}, {Mountpoint: "/data[*]"}, {Mountpoint: "/proc"}})
	s := strings.Join(args, "\n")
	if !strings.Contains(s, "-xdev") || strings.Count(s, "/proc") != 1 || !strings.Contains(s, `/data\[\*\]`) {
		t.Fatal(args)
	}
	args = findArgs("/fixture", []disk.PartitionStat{{Mountpoint: "/elsewhere"}})
	for _, s := range args {
		if s == "-prune" {
			t.Fatal("outside mount excluded")
		}
	}
}
func TestParseRecord(t *testing.T) {
	for _, path := range []string{"/a b", "/line\nbreak", "/tab\tname"} {
		f, err := parseRecord("123 " + path)
		if err != nil || f.Path != path || f.Size != 123 {
			t.Fatal(f, err)
		}
	}
	for _, record := range []string{"not-size /file", "-1 /file", "12", "12 "} {
		if _, err := parseRecord(record); err == nil {
			t.Fatal("invalid record accepted")
		}
	}
}
