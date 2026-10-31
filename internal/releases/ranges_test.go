package releases

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

type rangeRequest struct {
	name, scope string
	result      chan sourceResult
}
type rangeSource struct{ calls chan rangeRequest }

func (s *rangeSource) FetchRange(ctx context.Context, name, scope string) (Release, error) {
	req := rangeRequest{name, scope, make(chan sourceResult, 1)}
	select {
	case s.calls <- req:
	case <-ctx.Done():
		return Release{}, ctx.Err()
	}
	select {
	case r := <-req.result:
		return r.release, r.err
	case <-ctx.Done():
		return Release{}, ctx.Err()
	}
}
func nextRangeRequest(t *testing.T, source *rangeSource, scope string) rangeRequest {
	t.Helper()
	select {
	case req := <-source.calls:
		if req.name != "nginx" || req.scope != scope {
			t.Fatalf("wrong range request: %+v", req)
		}
		return req
	case <-time.After(time.Second):
		t.Fatal("range request not scheduled")
	}
	return rangeRequest{}
}
func rangeManager(t *testing.T) (*Manager, *rangeSource, string) {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	source := &rangeSource{calls: make(chan rangeRequest, 10)}
	m := NewManager(ctx, source)
	dir := t.TempDir()
	if err := os.Chmod(dir, 0700); err != nil {
		t.Fatal(err)
	}
	if err := m.ConfigureRanges(dir); err != nil {
		t.Fatal(err)
	}
	return m, source, dir
}

func TestRangesIsolateInflightResultsCacheAndCooldown(t *testing.T) {
	m, source, dir := rangeManager(t)
	m.Latest([]string{"nginx"}, false)
	original := nextRangeRequest(t, source, "")
	release, err := m.SaveRange("nginx", "1.28")
	if err != nil || release.Status != "loading" || release.Range != "1.28" || release.Version != "" {
		t.Fatalf("switch: %+v %v", release, err)
	}
	scoped := nextRangeRequest(t, source, "1.28")
	original.result <- sourceResult{release: Release{Version: "1.30.2"}}
	// Wait until the old query has committed; it must only affect its own key.
	deadline := time.Now().Add(time.Second)
	for {
		m.mu.Lock()
		done := m.entries["nginx"].Status == "ready"
		m.mu.Unlock()
		if done {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("old request did not complete")
		}
		time.Sleep(time.Millisecond)
	}
	if current := m.Latest([]string{"nginx"}, true)["nginx"]; current.Status != "loading" || current.Version != "" || current.Range != "1.28" {
		t.Fatalf("old range leaked: %+v", current)
	}
	scoped.result <- sourceResult{release: Release{Version: "1.28.3", Changelog: "fixes"}}
	if got := waitRelease(t, m, "ready"); got.Version != "1.28.3" || got.Range != "1.28" {
		t.Fatal(got)
	}
	// Going back to a cached range respects that range's manual cooldown.
	if got, err := m.SaveRange("nginx", ""); err != nil || got.Version != "1.30.2" || got.Status != "ready" {
		t.Fatalf("cached default: %+v %v", got, err)
	}
	if got, err := m.SaveRange("nginx", "1.28"); err != nil || got.Version != "1.28.3" || got.Status != "ready" {
		t.Fatalf("cached scope: %+v %v", got, err)
	}
	select {
	case call := <-source.calls:
		t.Fatalf("cooldown bypassed: %+v", call)
	default:
	}
	m.mu.Lock()
	entry := m.entries["nginx@1.28"]
	old := time.Now().Add(-7 * time.Hour)
	entry.AttemptedAt = &old
	m.entries["nginx@1.28"] = entry
	m.mu.Unlock()
	m.Latest([]string{"nginx"}, false)
	retry := nextRangeRequest(t, source, "1.28")
	retry.result <- sourceResult{err: errors.New("offline")}
	if got := waitRelease(t, m, "failed"); got.Version != "1.28.3" || got.Changelog != "fixes" || got.Range != "1.28" {
		t.Fatalf("lost same-range result: %+v", got)
	}
	store, err := openRangeStore(dir)
	if err != nil || store.values["nginx"] != "1.28" {
		t.Fatalf("restart lost setting: %+v %v", store, err)
	}
}

func TestRangeValidationAndAtomicSaveFailure(t *testing.T) {
	m, source, dir := rangeManager(t)
	for _, value := range []string{"8.3.1", "8.*", "08", "8.03", "8.", "-1", "8/3", " 8", "8\n", "9999999999", "1e2"} {
		if _, err := m.SaveRange("nginx", value); !errors.Is(err, ErrRangeInput) {
			t.Fatalf("accepted %q: %v", value, err)
		}
	}
	if _, err := m.SaveRange("redis", "8"); !errors.Is(err, ErrRangeInput) {
		t.Fatal(err)
	}
	for _, value := range []string{"", "0", "1", "8.3", "11.4"} {
		if !ValidRange(value) {
			t.Fatalf("rejected %q", value)
		}
	}
	if _, err := m.SaveRange("nginx", "1.28"); err != nil {
		t.Fatal(err)
	}
	nextRangeRequest(t, source, "1.28")
	path := filepath.Join(dir, "service-release-ranges.json")
	before, _ := os.ReadFile(path)
	if err := os.Chmod(path, 0644); err != nil {
		t.Fatal(err)
	}
	if _, err := m.SaveRange("nginx", "1.26"); !errors.Is(err, ErrRangeStorage) {
		t.Fatal("unsafe file accepted", err)
	}
	if got := m.Latest([]string{"nginx"}, false)["nginx"]; got.Range != "1.28" {
		t.Fatal("failed save applied", got)
	}
	after, _ := os.ReadFile(path)
	if string(before) != string(after) {
		t.Fatal("failed save changed persisted state")
	}
}

func TestDamagedRangeSettingsBlockOnlyScopedServices(t *testing.T) {
	for _, data := range []string{`null`, `{"nginx":null}`, `{"nginx":"1.2.3"}`, `{"redis":"8"}`, `{} {}`, `broken`, strings.Repeat(" ", 4097)} {
		t.Run(data[:min(len(data), 30)], func(t *testing.T) {
			dir := t.TempDir()
			if err := os.Chmod(dir, 0700); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(dir, "service-release-ranges.json"), []byte(data), 0600); err != nil {
				t.Fatal(err)
			}
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			source := &rangeSource{calls: make(chan rangeRequest, 1)}
			m := NewManager(ctx, source)
			if m.ConfigureRanges(dir) == nil {
				t.Fatal("damaged settings accepted")
			}
			got := m.Latest([]string{"nginx", "node", "python3", "redis"}, false)
			if got["nginx"].Status != "failed" || got["node"].Status != "failed" || got["python3"].Status != "failed" || got["redis"].Status != "loading" {
				t.Fatalf("bad failure isolation: %+v", got)
			}
			if _, err := m.SaveRange("nginx", ""); !errors.Is(err, ErrRangeStorage) {
				t.Fatal("load error silently cleared", err)
			}
		})
	}
}

func TestRangeStoreRejectsLinks(t *testing.T) {
	dir := t.TempDir()
	if err := os.Chmod(dir, 0700); err != nil {
		t.Fatal(err)
	}
	outside := filepath.Join(t.TempDir(), "ranges.json")
	if err := os.WriteFile(outside, []byte(`{"nginx":"1.28"}`), 0600); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "service-release-ranges.json")
	if err := os.Symlink(outside, path); err != nil {
		t.Fatal(err)
	}
	if _, err := openRangeStore(dir); err == nil {
		t.Fatal("symlink accepted")
	}
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	if err := os.Link(outside, path); err != nil {
		t.Fatal(err)
	}
	if _, err := openRangeStore(dir); err == nil {
		t.Fatal("hardlink accepted")
	}
}

func TestComponentRangesExtendExistingSettingsAndPersist(t *testing.T) {
	m, source, dir := rangeManager(t)
	if _, err := m.SaveRange("nginx", "1.28"); err != nil {
		t.Fatal(err)
	}
	nextRangeRequest(t, source, "1.28")
	// Reopening the old three-service file must preserve its values when components are added.
	if err := m.ConfigureRanges(dir); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct{ name, scope string }{{"node", "22.23"}, {"python3", "3.13"}} {
		got, err := m.SaveRange(tc.name, tc.scope)
		if err != nil || got.Range != tc.scope || got.Status != "loading" {
			t.Fatalf("component range not applied: %+v %v", got, err)
		}
		select {
		case req := <-source.calls:
			if req.name != tc.name || req.scope != tc.scope {
				t.Fatalf("wrong range request: %+v", req)
			}
			req.result <- sourceResult{release: Release{Version: tc.scope + ".10"}}
		case <-time.After(time.Second):
			t.Fatal("component range request not scheduled")
		}
	}
	store, err := openRangeStore(dir)
	if err != nil || store.values["nginx"] != "1.28" || store.values["node"] != "22.23" || store.values["python3"] != "3.13" {
		t.Fatalf("lost saved ranges: %+v %v", store, err)
	}
}
