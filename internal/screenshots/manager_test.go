package screenshots

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"image"
	"image/jpeg"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

type fakeRenderer struct {
	capture func(context.Context, Target) ([]byte, error)
	calls   atomic.Int32
}
type unavailableRenderer struct{}

func (unavailableRenderer) Available() error { return errors.New("未找到 Chromium") }
func (r unavailableRenderer) Capture(context.Context, Target) ([]byte, error) {
	return nil, r.Available()
}
func TestMissingBrowserKeepsSettings(t *testing.T) {
	m := newTestManager(t, t.TempDir(), unavailableRenderer{})
	if m.Latest().BrowserAvailable {
		t.Fatal("missing browser reported available")
	}
	if _, err := m.Save("example.conf", Input{URL: "http://localhost/"}); err != nil {
		t.Fatal(err)
	}
	v := waitStatus(t, m, "example.conf", "failed")
	if v.URL != "http://localhost/" || !strings.Contains(v.Error, "Chromium") {
		t.Fatal("settings lost or no explanation")
	}
}

func (f *fakeRenderer) Available() error { return nil }
func (f *fakeRenderer) Capture(ctx context.Context, target Target) ([]byte, error) {
	f.calls.Add(1)
	return f.capture(ctx, target)
}
func testImage() []byte {
	var b bytes.Buffer
	_ = jpeg.Encode(&b, image.NewRGBA(image.Rect(0, 0, 1440, 900)), nil)
	return b.Bytes()
}
func newTestManager(t *testing.T, dir string, f Renderer) *Manager {
	t.Helper()
	if err := os.Chmod(dir, 0700); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	m, err := NewManager(ctx, dir, f)
	if err != nil {
		t.Fatal(err)
	}
	return m
}
func waitStatus(t *testing.T, m *Manager, name, status string) View {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		v := m.Latest().Items[name]
		if v.Status == status {
			return v
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("status: %+v, want %s", m.Latest().Items[name], status)
	return View{}
}
func TestPersistentEncryptedCredentialsAndImage(t *testing.T) {
	dir := t.TempDir()
	password := "private-basic-password"
	var mu sync.Mutex
	var received Target
	f := &fakeRenderer{capture: func(_ context.Context, target Target) ([]byte, error) {
		mu.Lock()
		received = target
		mu.Unlock()
		return testImage(), nil
	}}
	m := newTestManager(t, dir, f)
	if _, err := m.Save("example.conf", Input{URL: "http://localhost:8080/", AuthEnabled: true, Username: "admin", Password: &password}); err != nil {
		t.Fatal(err)
	}
	v := waitStatus(t, m, "example.conf", "ready")
	mu.Lock()
	if received.Password != password {
		t.Error("missing credentials")
	}
	mu.Unlock()
	jsonView, _ := json.Marshal(m.Latest())
	if bytes.Contains(jsonView, []byte(password)) || bytes.Contains(jsonView, []byte("password_cipher")) {
		t.Fatal("API leaked password")
	}
	data, err := os.ReadFile(filepath.Join(dir, "screenshot-settings.json"))
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(data, []byte(password)) {
		t.Fatal("plaintext password on disk")
	}
	for _, file := range []string{"screenshots.key", "screenshot-settings.json", imageName("example.conf", v.ImageVersion)} {
		info, err := os.Stat(filepath.Join(dir, file))
		if err != nil || info.Mode().Perm() != 0600 {
			t.Fatalf("permissions %s: %v", file, err)
		}
	}
	m2 := newTestManager(t, dir, f)
	if m2.Latest().Items["example.conf"].ImageVersion != v.ImageVersion {
		t.Fatal("image lost after restart")
	}
	if data, err := m2.Image("example.conf"); err != nil || len(data) == 0 {
		t.Fatal("missing restored image", err)
	}
	if _, err := m2.Save("example.conf", Input{URL: "http://localhost:8080/new", AuthEnabled: true, Username: "admin"}); err != nil {
		t.Fatal("password should be retained", err)
	}
	waitStatus(t, m2, "example.conf", "ready")
	for _, input := range []Input{{URL: "http://localhost:8081/", AuthEnabled: true, Username: "admin"}, {URL: "http://localhost:8080/", AuthEnabled: true, Username: "new-user"}} {
		if _, err := m2.Save("example.conf", input); err == nil {
			t.Fatal("reused credentials with another origin/user")
		}
	}
	if _, err := m2.Save("example.conf", Input{URL: "http://localhost:8080/"}); err != nil {
		t.Fatal(err)
	}
	v = waitStatus(t, m2, "example.conf", "ready")
	if v.AuthEnabled || v.Username != "" || v.PasswordSaved {
		t.Fatal("credentials not cleared")
	}
	if m2.items["example.conf"].PasswordCipher != "" {
		t.Fatal("cipher retained after disabling auth")
	}
}
func TestFailuresPreserveImageAndClearRemovesFiles(t *testing.T) {
	var fail atomic.Bool
	f := &fakeRenderer{capture: func(context.Context, Target) ([]byte, error) {
		if fail.Load() {
			return nil, errors.New("认证失败")
		}
		return testImage(), nil
	}}
	dir := t.TempDir()
	m := newTestManager(t, dir, f)
	_, _ = m.Save("example.conf", Input{URL: "https://example.com/"})
	old := waitStatus(t, m, "example.conf", "ready")
	fail.Store(true)
	_, _ = m.Refresh("example.conf")
	v := waitStatus(t, m, "example.conf", "failed")
	if v.ImageVersion != old.ImageVersion || v.CapturedAt == nil || v.Error != "认证失败" {
		t.Fatal("failure replaced previous image")
	}
	if err := m.Delete("example.conf"); err != nil {
		t.Fatal(err)
	}
	if _, ok := m.Latest().Items["example.conf"]; ok {
		t.Fatal("settings not removed")
	}
	if _, err := m.Image("example.conf"); !os.IsNotExist(err) {
		t.Fatal("image still available", err)
	}
	if _, err := os.Stat(filepath.Join(dir, imageName("example.conf", old.ImageVersion))); !os.IsNotExist(err) {
		t.Fatal("file not removed")
	}
}
func TestChangedSettingsCancelAndDiscardOldCapture(t *testing.T) {
	started := make(chan Target, 4)
	release := make(chan struct{}, 4)
	f := &fakeRenderer{capture: func(ctx context.Context, target Target) ([]byte, error) {
		started <- target
		<-release
		return testImage(), nil
	}}
	m := newTestManager(t, t.TempDir(), f)
	_, _ = m.Save("example.conf", Input{URL: "https://old.example/"})
	<-started
	_, _ = m.Save("example.conf", Input{URL: "https://new.example/"})
	release <- struct{}{}
	second := <-started
	if second.URL != "https://new.example/" {
		t.Fatal("new settings lost")
	}
	if v := m.Latest().Items["example.conf"]; v.ImageVersion != "" {
		t.Fatal("old task overwrote new settings")
	}
	if err := m.Delete("example.conf"); err != nil {
		t.Fatal(err)
	}
	release <- struct{}{}
	// Finish must discard a capture even when a renderer ignores cancellation.
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		m.mu.Lock()
		running := m.runningName
		m.mu.Unlock()
		if running == "" {
			break
		}
		time.Sleep(time.Millisecond)
	}
	if _, ok := m.Latest().Items["example.conf"]; ok {
		t.Fatal("deleted task restored settings")
	}
}
func TestBoundedQueueDeduplicates(t *testing.T) {
	started := make(chan struct{}, 1)
	f := &fakeRenderer{capture: func(ctx context.Context, _ Target) ([]byte, error) {
		select {
		case started <- struct{}{}:
		default:
		}
		<-ctx.Done()
		return nil, ctx.Err()
	}}
	m := newTestManager(t, t.TempDir(), f)
	_, _ = m.Save("first.conf", Input{URL: "http://127.0.0.1/"})
	<-started
	for i := 0; i < QueueLimit; i++ {
		name := strings.Repeat("a", i+1) + ".conf"
		if _, err := m.Save(name, Input{URL: "http://127.0.0.1/"}); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := m.Save("overflow.conf", Input{URL: "http://127.0.0.1/"}); !errors.Is(err, ErrBusy) {
		t.Fatal("unbounded queue", err)
	}
	if _, err := m.Save("a.conf", Input{URL: "http://localhost/"}); err != nil {
		t.Fatal("queued replacement rejected", err)
	}
	if _, err := m.Refresh("first.conf"); err != nil {
		t.Fatal("running duplicate rejected", err)
	}
	if f.calls.Load() != 1 {
		t.Fatal("duplicate renderer execution")
	}
}
func TestURLValidationAndMissingKey(t *testing.T) {
	f := &fakeRenderer{capture: func(context.Context, Target) ([]byte, error) { return testImage(), nil }}
	dir := t.TempDir()
	m := newTestManager(t, dir, f)
	for _, u := range []string{"file:///etc/passwd", "data:text/html,test", "https://user:secret@example.com/", "http://", "javascript:alert(1)"} {
		if _, err := m.Save("example.conf", Input{URL: u}); err == nil {
			t.Fatal("accepted", u)
		}
	}
	if _, err := m.Save("../example.conf", Input{URL: "http://localhost/"}); err == nil {
		t.Fatal("accepted unsafe name")
	}
	_, _ = m.Save("example.conf", Input{URL: "http://localhost/"})
	waitStatus(t, m, "example.conf", "ready")
	if err := os.Remove(filepath.Join(dir, "screenshots.key")); err != nil {
		t.Fatal(err)
	}
	if _, _, err := openStore(dir); err == nil {
		t.Fatal("silently replaced missing key")
	}
}
func TestInterruptedTaskAndPrivateFiles(t *testing.T) {
	dir := t.TempDir()
	if err := os.Chmod(dir, 0700); err != nil {
		t.Fatal(err)
	}
	s, _, err := openStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.save(map[string]record{"example.conf": {View: View{Name: "example.conf", URL: "http://localhost/", Status: "running"}}}); err != nil {
		t.Fatal(err)
	}
	_, items, err := openStore(dir)
	if err != nil || items["example.conf"].Status != "failed" {
		t.Fatal("interrupted task not recovered", err)
	}
	if err := os.Chmod(filepath.Join(dir, "screenshot-settings.json"), 0644); err != nil {
		t.Fatal(err)
	}
	if _, _, err := openStore(dir); err == nil {
		t.Fatal("accepted public settings")
	}
}

func TestStorageFailureDoesNotLeaveTaskRunning(t *testing.T) {
	started, release := make(chan struct{}), make(chan struct{})
	var once sync.Once
	f := &fakeRenderer{capture: func(context.Context, Target) ([]byte, error) {
		once.Do(func() { close(started); <-release })
		return testImage(), nil
	}}
	dir := t.TempDir()
	m := newTestManager(t, dir, f)
	if _, err := m.Save("example.conf", Input{URL: "http://localhost/"}); err != nil {
		t.Fatal(err)
	}
	<-started
	m.mu.Lock()
	m.store.dir = filepath.Join(dir, "missing-state-directory")
	m.mu.Unlock()
	close(release)
	v := waitStatus(t, m, "example.conf", "failed")
	if !strings.Contains(v.Error, "磁盘空间") {
		t.Fatal("storage error not reported")
	}
	m.mu.Lock()
	m.store.dir = dir
	m.mu.Unlock()
	if _, err := m.Refresh("example.conf"); err != nil {
		t.Fatal(err)
	}
	waitStatus(t, m, "example.conf", "ready")
}
