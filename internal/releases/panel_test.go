package releases

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"
)

func TestNewerPanelVersion(t *testing.T) {
	for _, tc := range []struct {
		current, latest string
		want            bool
	}{
		{"v0.1.9", "v0.1.10", true},
		{"0.9.99", "v0.10.0", true},
		{"v1.0.0", "v0.99.99", false},
		{"v1.2.3", "1.2.3", false},
		{"v1.2.3+build.2", "v1.2.3", false},
		{"v1.2.3-rc.1", "v1.2.3", true},
		{"v1.2.4-rc.1", "v1.2.3", false},
		{"dev", "v1.2.3", false},
		{"dev-a1234", "v1.2.3", false},
		{"v01.2.3", "v1.2.3", false},
		{"v1.2.3", "v1.3.0-beta.1", false},
		{"v1.2.3", "invalid", false},
	} {
		if got := newerPanelVersion(tc.current, tc.latest); got != tc.want {
			t.Errorf("newerPanelVersion(%q, %q)=%v, want %v", tc.current, tc.latest, got, tc.want)
		}
	}
}

func TestFetchPanel(t *testing.T) {
	f := fixtureFetcher(t, map[string]string{panelReleaseAPI: `{"tag_name":"v0.1.30","body":"## 更新\n修复问题","html_url":"javascript:alert(1)"}`})
	r, err := f.FetchPanel(context.Background(), "v0.1.29")
	if err != nil || !r.HasUpdate || r.CurrentVersion != "v0.1.29" || r.LatestVersion != "v0.1.30" || r.Changelog != "## 更新\n修复问题" || r.ReleaseURL != "https://github.com/azpanel/lnmp-gopanel/releases/tag/v0.1.30" {
		t.Fatalf("release=%+v error=%v", r, err)
	}
	for _, body := range []string{
		`{"tag_name":"v1.0.0","draft":true}`,
		`{"tag_name":"v1.0.0","prerelease":true}`,
		`{"tag_name":"v1.0.0-rc.1"}`,
		`{"tag_name":"not-a-version"}`,
		`{broken`,
	} {
		if _, err := fixtureFetcher(t, map[string]string{panelReleaseAPI: body}).FetchPanel(context.Background(), "v0.1.0"); err == nil {
			t.Errorf("accepted invalid release: %s", body)
		}
	}
	if r, err := fixtureFetcher(t, map[string]string{panelReleaseAPI: `{"tag_name":"v1.0.0"}`}).FetchPanel(context.Background(), "dev"); err != nil || r.HasUpdate || r.Changelog != "" {
		t.Fatalf("development release=%+v error=%v", r, err)
	}
}

func TestFetchPanelUpstreamErrors(t *testing.T) {
	for _, status := range []int{403, 404, 429, 503} {
		f := NewFetcher()
		f.client.Transport = roundTripFunc(func(req *http.Request) (*http.Response, error) {
			return &http.Response{StatusCode: status, Body: io.NopCloser(strings.NewReader("")), Header: make(http.Header)}, nil
		})
		if _, err := f.FetchPanel(context.Background(), "v0.1.0"); err == nil {
			t.Errorf("accepted HTTP %d", status)
		}
	}
	f := NewFetcher()
	f.client.Transport = roundTripFunc(func(req *http.Request) (*http.Response, error) {
		if req.Context().Err() != nil {
			return nil, req.Context().Err()
		}
		return nil, errors.New("network timeout")
	})
	if _, err := f.FetchPanel(context.Background(), "v0.1.0"); err == nil {
		t.Fatal("accepted network failure")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := f.FetchPanel(ctx, "v0.1.0"); err == nil {
		t.Fatal("accepted cancelled check")
	}
}
