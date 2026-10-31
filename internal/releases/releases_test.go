package releases

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (fn roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return fn(r) }

func fixtureFetcher(t *testing.T, fixtures map[string]string) *Fetcher {
	t.Helper()
	f := NewFetcher()
	f.client.Transport = roundTripFunc(func(req *http.Request) (*http.Response, error) {
		body, ok := fixtures[req.URL.String()]
		status := http.StatusOK
		if !ok {
			status = http.StatusServiceUnavailable
		}
		return &http.Response{StatusCode: status, Body: io.NopCloser(strings.NewReader(body)), Header: make(http.Header), Request: req}, nil
	})
	return f
}

func TestOfficialSourcesSelectStableReleasesAndMatchingChangelog(t *testing.T) {
	fixtures := map[string]string{
		"https://nginx.org/en/download.html":                                                  `<h4>Mainline version</h4><a>nginx-1.31.0.tar.gz</a><h4>Stable version</h4><a>nginx-1.30.2.tar.gz</a>`,
		"https://nginx.org/en/CHANGES-1.30":                                                   "Changes with nginx 1.30.2\nlatest fixes\nChanges with nginx 1.30.1\nold fixes",
		"https://www.php.net/releases/index.php?json":                                         `{"7":{"version":"7.4.33"},"8":{"version":"8.5.2"},"9":{"version":"9.0.0RC1"}}`,
		"https://raw.githubusercontent.com/php/php-src/php-8.5.2/NEWS":                        "PHP NEWS\n22 Oct 2026, PHP 8.5.2\nlatest fixes\n01 Oct 2026, PHP 8.5.1\nold fixes",
		"https://downloads.mariadb.org/rest-api/mariadb/":                                     `{"major_releases":[{"release_id":"14.0","release_status":"RC"},{"release_id":"12.9","release_status":"Stable"},{"release_id":"12.10","release_status":"Stable"}]}`,
		"https://downloads.mariadb.org/rest-api/mariadb/12.10/":                               `{"releases":{"12.10.2":{},"12.10.3":{},"12.10.4-rc":{},"99.0.0":{}}}`,
		"https://mariadb.com/docs/release-notes/community-server/changelogs/12.10/12.10.3.md": "preamble\n# MariaDB 12.10.3 Changelog\nlatest fixes",
		"https://api.github.com/repos/mysql/mysql-server/git/matching-refs/tags/mysql-":       `[{"ref":"refs/tags/mysql-cluster-99.0.0"},{"ref":"refs/tags/mysql-9.9.2"},{"ref":"refs/tags/mysql-9.10.0"},{"ref":"refs/tags/mysql-9.11.0-rc"}]`,
		"https://dev.mysql.com/doc/relnotes/mysql/9.10/en/news-9-10-0.html":                   `<nav>outside navigation</nav><div id="docs-body"><h2>9.10.0</h2><p>latest fixes</p><script>bad script</script></div>`,
		"https://api.github.com/repos/redis/redis/releases/latest":                            `{"tag_name":"8.2.1","body":"latest fixes","draft":false,"prerelease":false}`,
	}
	for name, want := range map[string]string{"nginx": "1.30.2", "php-fpm": "8.5.2", "mariadb": "12.10.3", "mysql": "9.10.0", "redis": "8.2.1"} {
		t.Run(name, func(t *testing.T) {
			r, err := fixtureFetcher(t, fixtures).Fetch(context.Background(), name)
			if err != nil || r.Version != want || r.ChangelogError != "" || !strings.Contains(r.Changelog, "latest fixes") {
				t.Fatalf("unexpected release: %+v, %v", r, err)
			}
			for _, unwanted := range []string{"old fixes", "outside navigation", "bad script", "preamble"} {
				if strings.Contains(r.Changelog, unwanted) {
					t.Fatalf("unexpected changelog content: %q", r.Changelog)
				}
			}
		})
	}
}

func TestChangelogFailureKeepsLatestVersionAndOfficialLink(t *testing.T) {
	f := fixtureFetcher(t, map[string]string{
		"https://nginx.org/en/download.html": `<h4>Stable version</h4><a>nginx-1.30.2.tar.gz</a>`,
	})
	r, err := f.Fetch(context.Background(), "nginx")
	if err != nil || r.Version != "1.30.2" || !strings.Contains(r.ChangelogError, "503") || r.ChangelogURL == "" {
		t.Fatalf("changelog failure lost version: %+v, %v", r, err)
	}
}

func TestRejectPrereleasesAndUntrustedSources(t *testing.T) {
	f := fixtureFetcher(t, map[string]string{"https://api.github.com/repos/redis/redis/releases/latest": `{"tag_name":"9.0.0","prerelease":true}`})
	if _, err := f.Fetch(context.Background(), "redis"); err == nil {
		t.Fatal("prerelease accepted")
	}
	for _, url := range []string{"http://nginx.org/en/download.html", "https://localhost/", "https://nginx.org.evil.test/", "https://user@nginx.org/", "https://nginx.org:443/"} {
		if _, err := f.get(context.Background(), url); err == nil {
			t.Fatalf("untrusted source accepted: %s", url)
		}
	}
	f.client.Transport = roundTripFunc(func(req *http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: 302, Body: io.NopCloser(strings.NewReader("")), Header: http.Header{"Location": []string{"http://127.0.0.1/"}}, Request: req}, nil
	})
	if _, err := f.get(context.Background(), "https://nginx.org/"); err == nil {
		t.Fatal("untrusted redirect accepted")
	}
}

func TestBoundedResponseAndCancelledRequest(t *testing.T) {
	f := fixtureFetcher(t, map[string]string{"https://nginx.org/": strings.Repeat("x", maxBody+1)})
	if _, err := f.get(context.Background(), "https://nginx.org/"); err == nil {
		t.Fatal("oversized upstream response accepted")
	}
	f.client.Transport = roundTripFunc(func(req *http.Request) (*http.Response, error) {
		<-req.Context().Done()
		return nil, req.Context().Err()
	})
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := f.get(ctx, "https://nginx.org/"); err == nil {
		t.Fatal("cancelled query succeeded")
	}
}

type sourceResult struct {
	release Release
	err     error
}
type blockingSource struct {
	calls   atomic.Int32
	results chan sourceResult
}

func (s *blockingSource) FetchRange(ctx context.Context, name, scope string) (Release, error) {
	s.calls.Add(1)
	select {
	case result := <-s.results:
		return result.release, result.err
	case <-ctx.Done():
		return Release{}, ctx.Err()
	}
}

func waitRelease(t *testing.T, m *Manager, status string) Release {
	t.Helper()
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		v := m.Latest([]string{"nginx"}, false)["nginx"]
		if v.Status == status {
			return v
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatalf("release did not reach %s", status)
	return Release{}
}

func TestAsyncQueriesDeduplicateCacheAndPreserveStaleResults(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	source := &blockingSource{results: make(chan sourceResult, 1)}
	m := NewManager(ctx, source)
	for i := 0; i < 20; i++ {
		if v := m.Latest([]string{"nginx", "sshd"}, true); len(v) != 1 || v["nginx"].Status != "loading" {
			t.Fatal("query blocked or unsupported service accepted")
		}
	}
	source.results <- sourceResult{release: Release{Version: "1.30.2", Changelog: "fixes"}}
	v := waitRelease(t, m, "ready")
	if source.calls.Load() != 1 || v.CheckedAt == nil {
		t.Fatal("queries were not shared")
	}
	for i := 0; i < 10; i++ {
		m.Latest([]string{"nginx"}, false)
		m.Latest([]string{"nginx"}, true)
	}
	if source.calls.Load() != 1 {
		t.Fatal("cache or manual refresh cooldown ignored")
	}
	m.mu.Lock()
	entry := m.entries["nginx"]
	old := time.Now().Add(-7 * time.Hour)
	entry.AttemptedAt = &old
	m.entries["nginx"] = entry
	m.mu.Unlock()
	if m.Latest([]string{"nginx"}, false)["nginx"].Status != "loading" {
		t.Fatal("expired cache did not refresh")
	}
	source.results <- sourceResult{err: errors.New("offline")}
	v = waitRelease(t, m, "failed")
	if v.Version != "1.30.2" || v.Changelog != "fixes" || v.Error != "offline" || v.CheckedAt == nil || source.calls.Load() != 2 {
		t.Fatalf("failure lost cached data: %+v", v)
	}
}
