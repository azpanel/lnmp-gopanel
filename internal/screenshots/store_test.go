package screenshots

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func testStore(t *testing.T) *store {
	t.Helper()
	dir := t.TempDir()
	if err := os.Chmod(dir, 0700); err != nil {
		t.Fatal(err)
	}
	s, _, err := openStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	return s
}

// Use individually valid records so an accepted boundary file must also reload.
func testStateOfSize(t *testing.T, size int) map[string]record {
	t.Helper()
	items := map[string]record{}
	count := size/MaxJavaScriptSize + 1
	for i := range count {
		name := fmt.Sprintf("site-%04d.conf", i)
		items[name] = record{View: View{Name: name, URL: "http://localhost/", Status: "ready",
			ViewportWidth: DefaultViewportWidth, ViewportHeight: DefaultViewportHeight,
			ZoomPercent: DefaultZoomPercent, Language: DefaultLanguage, LoadTimeoutSeconds: 30}}
	}
	remaining := size - len(testStateJSON(t, items))
	for i := range count {
		name := fmt.Sprintf("site-%04d.conf", i)
		r := items[name]
		r.JavaScript = strings.Repeat("x", min(remaining, MaxJavaScriptSize))
		remaining -= len(r.JavaScript)
		items[name] = r
	}
	if remaining != 0 || len(testStateJSON(t, items)) != size {
		t.Fatal("could not construct exact state size", remaining)
	}
	return items
}

func testStateJSON(t *testing.T, items map[string]record) []byte {
	t.Helper()
	data, err := json.Marshal(items)
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func testReadFile(t *testing.T, path string) []byte {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func TestStoreStateSizeBoundary(t *testing.T) {
	for _, delta := range []int{-1, 0, 1} {
		t.Run(fmt.Sprintf("limit%+d", delta), func(t *testing.T) {
			s := testStore(t)
			if err := s.save(map[string]record{}); err != nil {
				t.Fatal(err)
			}
			path := filepath.Join(s.dir, "screenshot-settings.json")
			before := testReadFile(t, path)
			items := testStateOfSize(t, MaxStateSize+delta)
			err := s.save(items)
			if delta > 0 {
				if !errors.Is(err, ErrStateTooLarge) {
					t.Fatal("oversized state accepted", err)
				}
				if !bytes.Equal(before, testReadFile(t, path)) {
					t.Fatal("rejected write changed state file")
				}
				temps, err := filepath.Glob(filepath.Join(s.dir, ".screenshot-*"))
				if err != nil || len(temps) != 0 {
					t.Fatal("rejected write left temporary files", temps, err)
				}
				return
			}
			if err != nil {
				t.Fatal("valid boundary write rejected", err)
			}
			if got := len(testReadFile(t, path)); got != MaxStateSize+delta {
				t.Fatal("unexpected stored size", got)
			}
			_, loaded, err := openStore(s.dir)
			if err != nil || len(loaded) != len(items) {
				t.Fatal("successfully saved state could not reload", err)
			}
		})
	}
}

func TestStoreLimitsEncodedSizeAndPreservesExistingOversizedState(t *testing.T) {
	s := testStore(t)
	if err := s.save(map[string]record{}); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(s.dir, "screenshot-settings.json")
	before := testReadFile(t, path)
	keyPath := filepath.Join(s.dir, "screenshots.key")
	key := testReadFile(t, keyPath)
	items := map[string]record{}
	script := "/*" + strings.Repeat("\x01", MaxJavaScriptSize-4) + "*/"
	for i := range 180 {
		name := fmt.Sprintf("site-%04d.conf", i)
		items[name] = record{View: View{Name: name, URL: "http://localhost/", JavaScript: script, Status: "failed"}}
	}
	data := testStateJSON(t, items)
	if len(data) <= MaxStateSize || len(script)*len(items) >= MaxStateSize {
		t.Fatal("fixture does not demonstrate JSON escape expansion")
	}
	if err := s.save(items); !errors.Is(err, ErrStateTooLarge) {
		t.Fatal("JSON escape expansion bypassed limit", err)
	}
	if !bytes.Equal(before, testReadFile(t, path)) {
		t.Fatal("rejected escaped state changed existing file")
	}
	// Simulate a file written by an older version without the write limit.
	if err := os.WriteFile(path, data, 0600); err != nil {
		t.Fatal(err)
	}
	if _, _, err := openStore(s.dir); !errors.Is(err, ErrStateTooLarge) {
		t.Fatal("oversized legacy file did not report the size error", err)
	}
	if !bytes.Equal(data, testReadFile(t, path)) || !bytes.Equal(key, testReadFile(t, keyPath)) {
		t.Fatal("opening oversized state modified file or key")
	}
}

func TestRejectedSavePreservesSettingsQueueAndRunningJob(t *testing.T) {
	s := testStore(t)
	items := testStateOfSize(t, MaxStateSize)
	if err := s.save(items); err != nil {
		t.Fatal(err)
	}
	name := "site-0000.conf"
	before := items[name]
	queued := job{name: name, revision: 7}
	cancelled := false
	m := &Manager{ctx: context.Background(), store: s, items: items, versions: map[string]uint64{name: 7},
		queue: []job{queued}, notify: make(chan struct{}, 1), runningName: name,
		runningCancel: func() { cancelled = true }}
	path := filepath.Join(s.dir, "screenshot-settings.json")
	diskBefore := testReadFile(t, path)
	_, err := m.Save(name, Input{URL: "http://localhost/" + strings.Repeat("a", 1024), JavaScript: before.JavaScript})
	if !errors.Is(err, ErrStateTooLarge) {
		t.Fatal("manager did not preserve size error", err)
	}
	if m.items[name] != before || len(m.queue) != 1 || m.queue[0] != queued || m.versions[name] != 7 || cancelled || m.runningName != name || len(m.notify) != 0 {
		t.Fatal("rejected save changed settings, queue or running job")
	}
	if !bytes.Equal(diskBefore, testReadFile(t, path)) {
		t.Fatal("rejected save changed persisted state")
	}
}

func TestFinishOverStateLimitPreservesPreviousImage(t *testing.T) {
	s := testStore(t)
	items := testStateOfSize(t, MaxStateSize)
	name := "site-0000.conf"
	r := items[name]
	when := time.Unix(0, 0).UTC()
	r.Status, r.ImageVersion, r.CapturedURL, r.CapturedAt = "running", strings.Repeat("a", 32), "http://localhost/previous", &when
	items[name] = r
	extra := len(testStateJSON(t, items)) - MaxStateSize
	r.JavaScript = r.JavaScript[:len(r.JavaScript)-extra]
	items[name] = r
	if err := s.save(items); err != nil {
		t.Fatal(err)
	}
	image := testImage()
	if err := s.atomic(imageName(name, r.ImageVersion), image); err != nil {
		t.Fatal(err)
	}
	statePath := filepath.Join(s.dir, "screenshot-settings.json")
	before := testReadFile(t, statePath)
	m := &Manager{store: s, items: items, versions: map[string]uint64{}, runningName: name, runningCancel: func() {}}
	m.finish(job{name: name}, "http://localhost/"+strings.Repeat("a", 1024), image, nil)
	got := m.items[name]
	if got.Status != "failed" || got.Error != ErrStateTooLarge.Error() || got.ImageVersion != r.ImageVersion || got.CapturedURL != r.CapturedURL || got.CapturedAt != r.CapturedAt || m.runningName != "" || m.runningCancel != nil {
		t.Fatal("state failure lost previous capture or remained running", got.Status, got.Error)
	}
	storedImage, err := m.Image(name)
	if err != nil || !bytes.Equal(storedImage, image) {
		t.Fatal("previous image is no longer readable", err)
	}
	images, err := filepath.Glob(filepath.Join(s.dir, "*.jpg"))
	if err != nil || len(images) != 1 || filepath.Base(images[0]) != imageName(name, r.ImageVersion) {
		t.Fatal("new image not removed after failed state save", images, err)
	}
	if !bytes.Equal(before, testReadFile(t, statePath)) {
		t.Fatal("failed finish overwrote persisted state")
	}
	_, loaded, err := openStore(s.dir)
	if err != nil || loaded[name].ImageVersion != r.ImageVersion || loaded[name].Status != "failed" {
		t.Fatal("previous capture could not reload", err)
	}
}
