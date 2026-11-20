package server

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"testing"
	"time"

	"github.com/azpanel/lnmp-gopanel/internal/auth"
	"github.com/azpanel/lnmp-gopanel/internal/buildinfo"
	"github.com/azpanel/lnmp-gopanel/internal/releases"
)

type panelReleaseSource func(context.Context, string) (releases.PanelRelease, error)

func (f panelReleaseSource) FetchPanel(ctx context.Context, current string) (releases.PanelRelease, error) {
	return f(ctx, current)
}

func TestPanelRelease(t *testing.T) {
	s, _ := testServer(t)
	calls := 0
	want := releases.PanelRelease{CurrentVersion: buildinfo.Version, LatestVersion: "v9.0.0", HasUpdate: true, Changelog: "更新日志", ReleaseURL: "https://github.com/azpanel/lnmp-gopanel/releases/tag/v9.0.0"}
	s.PanelReleases = panelReleaseSource(func(ctx context.Context, current string) (releases.PanelRelease, error) {
		calls++
		if deadline, ok := ctx.Deadline(); !ok || time.Until(deadline) > 12*time.Second || current != buildinfo.Version {
			t.Fatalf("unexpected check context or version: %q", current)
		}
		return want, nil
	})
	h := s.Handler()
	if w := request(h, "GET", "/api/v1/panel/release", "", nil, "", ""); w.Code != 401 || calls != 0 {
		t.Fatalf("anonymous check: status=%d calls=%d", w.Code, calls)
	}
	id, _ := s.Sessions.Create()
	cookie := &http.Cookie{Name: auth.CookieName, Value: id}
	for range 2 {
		w := request(h, "GET", "/api/v1/panel/release", "", cookie, "", "")
		var got releases.PanelRelease
		if json.Unmarshal(w.Body.Bytes(), &got) != nil || w.Code != 200 || w.Header().Get("Cache-Control") != "no-store" || got != want {
			t.Fatalf("release response: status=%d body=%s", w.Code, w.Body.String())
		}
	}
	if calls != 2 {
		t.Fatalf("login checks must not use a six-hour cache: calls=%d", calls)
	}
	s.PanelReleases = panelReleaseSource(func(context.Context, string) (releases.PanelRelease, error) {
		return releases.PanelRelease{}, errors.New("upstream timeout")
	})
	if w := request(h, "GET", "/api/v1/panel/release", "", cookie, "", ""); w.Code != 502 {
		t.Fatalf("failed check: status=%d", w.Code)
	}
}
