package releases

import (
	"context"
	"fmt"
	"regexp"
	"strings"

	"golang.org/x/net/html"
)

func (f *Fetcher) python(ctx context.Context) (Release, error) {
	const source = "https://www.python.org/downloads/"
	data, err := f.get(ctx, source)
	if err != nil {
		return Release{}, err
	}
	r := Release{SourceURL: source}
	// Match the complete release label so a beta/RC cannot be truncated to a stable version.
	for _, match := range regexp.MustCompile(`>Python (3\.[0-9]+\.[0-9]+)</a>`).FindAllSubmatch(data, -1) {
		version := string(match[1])
		if newer(version, r.Version) {
			r.Version = version
		}
	}
	if r.Version == "" {
		return Release{}, fmt.Errorf("未找到 Python 3 正式版本")
	}
	r.ChangelogURL = "https://docs.python.org/release/" + r.Version + "/whatsnew/changelog.html#python-" + strings.ReplaceAll(r.Version, ".", "-") + "-final"
	// The full HTML contains many historical versions and exceeds the response limit.
	f.notes(ctx, &r, "https://raw.githubusercontent.com/python/cpython/v"+r.Version+"/Misc/NEWS.d/"+r.Version+".rst", func(text string) string { return text })
	return r, nil
}
func (f *Fetcher) golang(ctx context.Context) (Release, error) {
	var versions []struct {
		Version string `json:"version"`
		Stable  bool   `json:"stable"`
	}
	if err := f.json(ctx, "https://go.dev/dl/?mode=json", &versions); err != nil {
		return Release{}, err
	}
	r := Release{SourceURL: "https://go.dev/dl/"}
	for _, item := range versions {
		v := strings.TrimPrefix(item.Version, "go")
		if item.Stable && (stableVersion.MatchString(v) || seriesVersion.MatchString(v)) && newer(v, r.Version) {
			r.Version = v
		}
	}
	if r.Version == "" {
		return Release{}, fmt.Errorf("未找到 Go 正式版本")
	}
	r.ChangelogURL = "https://go.dev/doc/devel/release#go" + r.Version
	f.notes(ctx, &r, "https://go.dev/doc/devel/release", func(text string) string {
		return goReleaseNotes(text, r.Version)
	})
	return r, nil
}
func goReleaseNotes(text, version string) string {
	root, err := html.Parse(strings.NewReader(text))
	if err != nil {
		return ""
	}
	// Patch releases live in a paragraph, while .0 releases use a heading.
	// Match text at the start, not incidental references in other releases.
	var found string
	var walk func(*html.Node)
	walk = func(n *html.Node) {
		if found != "" {
			return
		}
		if n.Type == html.ElementNode && (n.Data == "p" || n.Data == "h2") {
			value := plainText(n)
			if strings.HasPrefix(value, "go"+version+" ") || value == "go"+version {
				found = value
				if n.Data == "h2" {
					for next := n.NextSibling; next != nil; next = next.NextSibling {
						if next.Type == html.ElementNode && (next.Data == "h2" || next.Data == "h3") {
							break
						}
						found += "\n" + plainText(next)
					}
				}
				return
			}
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			walk(c)
		}
	}
	walk(root)
	return strings.TrimSpace(found)
}
func plainText(n *html.Node) string {
	var out strings.Builder
	var walk func(*html.Node)
	walk = func(n *html.Node) {
		if n.Type == html.ElementNode {
			switch n.Data {
			case "script", "style", "nav", "header", "footer":
				return
			}
		}
		if n.Type == html.TextNode {
			out.WriteString(n.Data)
			out.WriteByte(' ')
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			walk(c)
		}
	}
	walk(n)
	return strings.Join(strings.Fields(out.String()), " ")
}
func (f *Fetcher) node(ctx context.Context) (Release, error) {
	var versions []struct {
		Version string `json:"version"`
	}
	if err := f.json(ctx, "https://nodejs.org/dist/index.json", &versions); err != nil {
		return Release{}, err
	}
	r := Release{SourceURL: "https://nodejs.org/en/download/current"}
	for _, item := range versions {
		v := strings.TrimPrefix(item.Version, "v")
		if stableVersion.MatchString(v) && newer(v, r.Version) {
			r.Version = v
		}
	}
	if r.Version == "" {
		return Release{}, fmt.Errorf("未找到 Node.js 正式版本")
	}
	r.ChangelogURL = "https://nodejs.org/en/blog/release/v" + r.Version
	major := strings.Split(r.Version, ".")[0]
	f.notes(ctx, &r, "https://raw.githubusercontent.com/nodejs/node/v"+r.Version+"/doc/changelogs/CHANGELOG_V"+major+".md", func(text string) string {
		headings := regexp.MustCompile(`(?m)^## .*`).FindAllStringIndex(text, -1)
		for i, h := range headings {
			if !strings.Contains(text[h[0]:h[1]], "Version "+r.Version+" ") {
				continue
			}
			end := len(text)
			if i+1 < len(headings) {
				end = headings[i+1][0]
			}
			return text[h[0]:end]
		}
		return ""
	})
	return r, nil
}
func (f *Fetcher) codex(ctx context.Context) (Release, error) {
	var release struct {
		Tag        string `json:"tag_name"`
		Body       string `json:"body"`
		Prerelease bool   `json:"prerelease"`
		Draft      bool   `json:"draft"`
	}
	if err := f.json(ctx, "https://api.github.com/repos/openai/codex/releases/latest", &release); err != nil {
		return Release{}, err
	}
	v := strings.TrimPrefix(release.Tag, "rust-v")
	if !strings.HasPrefix(release.Tag, "rust-v") || release.Draft || release.Prerelease || !stableVersion.MatchString(v) {
		return Release{}, fmt.Errorf("未找到 Codex CLI 正式版本")
	}
	r := Release{Version: v, SourceURL: "https://github.com/openai/codex/releases", ChangelogURL: "https://github.com/openai/codex/releases/tag/" + release.Tag, Changelog: release.Body}
	if strings.TrimSpace(r.Changelog) == "" {
		r.ChangelogError = "官方发布未提供更新日志，请查看官方原文"
	}
	return r, nil
}

func (f *Fetcher) composer(ctx context.Context) (Release, error) {
	var release struct {
		Tag        string `json:"tag_name"`
		Body       string `json:"body"`
		Prerelease bool   `json:"prerelease"`
		Draft      bool   `json:"draft"`
	}
	if err := f.json(ctx, "https://api.github.com/repos/composer/composer/releases/latest", &release); err != nil {
		return Release{}, err
	}
	if release.Draft || release.Prerelease || !stableVersion.MatchString(release.Tag) {
		return Release{}, fmt.Errorf("未找到 Composer 正式版本")
	}
	r := Release{Version: release.Tag, SourceURL: "https://getcomposer.org/download/", ChangelogURL: "https://github.com/composer/composer/releases/tag/" + release.Tag, Changelog: release.Body}
	if strings.TrimSpace(r.Changelog) == "" {
		r.ChangelogError = "官方发布未提供更新日志，请查看官方原文"
	}
	return r, nil
}

func (f *Fetcher) phpMyAdmin(ctx context.Context) (Release, error) {
	var release struct {
		Tag        string `json:"tag_name"`
		Body       string `json:"body"`
		Prerelease bool   `json:"prerelease"`
		Draft      bool   `json:"draft"`
	}
	if err := f.json(ctx, "https://api.github.com/repos/phpmyadmin/phpmyadmin/releases/latest", &release); err != nil {
		return Release{}, err
	}
	version := strings.TrimPrefix(release.Tag, "RELEASE_")
	version = strings.ReplaceAll(version, "_", ".")
	if !strings.HasPrefix(release.Tag, "RELEASE_") || release.Draft || release.Prerelease || !stableVersion.MatchString(version) {
		return Release{}, fmt.Errorf("未找到 phpMyAdmin 正式版本")
	}
	r := Release{Version: version, SourceURL: "https://github.com/phpmyadmin/phpmyadmin/releases", ChangelogURL: "https://github.com/phpmyadmin/phpmyadmin/releases/tag/" + release.Tag, Changelog: release.Body}
	if strings.TrimSpace(r.Changelog) == "" {
		r.ChangelogError = "官方发布未提供更新日志，请查看官方原文"
	}
	return r, nil
}
