package releases

import (
	"context"
	"fmt"
	"regexp"
	"strings"
)

const panelReleaseAPI = "https://api.github.com/repos/azpanel/lnmp-gopanel/releases/latest"

type PanelRelease struct {
	CurrentVersion string `json:"current_version"`
	LatestVersion  string `json:"latest_version"`
	HasUpdate      bool   `json:"has_update"`
	Changelog      string `json:"changelog"`
	ReleaseURL     string `json:"release_url"`
}

var panelVersion = regexp.MustCompile(`^v?((?:0|[1-9][0-9]*)\.(?:0|[1-9][0-9]*)\.(?:0|[1-9][0-9]*))(-[0-9A-Za-z-]+(?:\.[0-9A-Za-z-]+)*)?(?:\+[0-9A-Za-z-]+(?:\.[0-9A-Za-z-]+)*)?$`)

func newerPanelVersion(current, latest string) bool {
	installed := panelVersion.FindStringSubmatch(current)
	target := panelVersion.FindStringSubmatch(latest)
	if installed == nil || target == nil || target[2] != "" {
		return false
	}
	// Compare decimal components without integer overflow.
	a, b := strings.Split(target[1], "."), strings.Split(installed[1], ".")
	for i := range a {
		if len(a[i]) != len(b[i]) {
			return len(a[i]) > len(b[i])
		}
		if a[i] != b[i] {
			return a[i] > b[i]
		}
	}
	return installed[2] != ""
}

func (f *Fetcher) FetchPanel(ctx context.Context, current string) (PanelRelease, error) {
	var release struct {
		Tag        string `json:"tag_name"`
		Body       string `json:"body"`
		Draft      bool   `json:"draft"`
		Prerelease bool   `json:"prerelease"`
	}
	if err := f.json(ctx, panelReleaseAPI, &release); err != nil {
		return PanelRelease{}, err
	}
	version := panelVersion.FindStringSubmatch(release.Tag)
	if release.Draft || release.Prerelease || version == nil || version[2] != "" {
		return PanelRelease{}, fmt.Errorf("未找到可识别的面板稳定版本")
	}
	return PanelRelease{
		CurrentVersion: current,
		LatestVersion:  release.Tag,
		HasUpdate:      newerPanelVersion(current, release.Tag),
		Changelog:      excerpt(release.Body),
		ReleaseURL:     "https://github.com/azpanel/lnmp-gopanel/releases/tag/" + release.Tag,
	}, nil
}
