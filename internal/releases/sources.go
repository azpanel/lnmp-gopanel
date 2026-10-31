package releases

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"time"

	"golang.org/x/net/html"
)

const maxBody = 4 * 1024 * 1024

var stableVersion = regexp.MustCompile(`^[0-9]+\.[0-9]+\.[0-9]+$`)
var seriesVersion = regexp.MustCompile(`^[0-9]+\.[0-9]+$`)
var prereleaseName = regexp.MustCompile(`(?i)\b(alpha|beta|rc|preview)\b`)

type Fetcher struct{ client *http.Client }

func allowedURL(raw string) bool {
	req, err := http.NewRequest(http.MethodGet, raw, nil)
	if err != nil || req.URL.Scheme != "https" || req.URL.User != nil || req.URL.Port() != "" {
		return false
	}
	switch req.URL.Hostname() {
	case "www.python.org", "docs.python.org", "go.dev", "nodejs.org", "nginx.org", "www.php.net", "downloads.mariadb.org", "mariadb.com", "api.github.com", "raw.githubusercontent.com", "dev.mysql.com":
		return true
	}
	return false
}

func NewFetcher() *Fetcher {
	return &Fetcher{client: &http.Client{
		Timeout: 12 * time.Second,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if len(via) >= 5 || !allowedURL(req.URL.String()) {
				return fmt.Errorf("发布源重定向不受支持")
			}
			return nil
		},
	}}
}

func (f *Fetcher) get(ctx context.Context, url string) ([]byte, error) {
	if !allowedURL(url) {
		return nil, fmt.Errorf("不支持的官方发布源")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "LNMP-GoPanel/0.1 (release-checker)")
	resp, err := f.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("连接官方发布源失败或超时")
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("官方发布源返回 HTTP %d", resp.StatusCode)
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, maxBody+1))
	if err != nil || len(data) > maxBody {
		return nil, fmt.Errorf("官方发布源响应不完整或过大")
	}
	return data, nil
}

func (f *Fetcher) json(ctx context.Context, url string, target any) error {
	data, err := f.get(ctx, url)
	if err != nil {
		return err
	}
	if json.Unmarshal(data, target) != nil {
		return fmt.Errorf("官方发布源格式无法识别")
	}
	return nil
}

func newer(a, b string) bool {
	aa, bb := strings.Split(a, "."), strings.Split(b, ".")
	for i := 0; i < len(aa); i++ {
		x, _ := strconv.Atoi(aa[i])
		y := 0
		if i < len(bb) {
			y, _ = strconv.Atoi(bb[i])
		}
		if x != y {
			return x > y
		}
	}
	return false
}

func series(version string) string {
	parts := strings.Split(version, ".")
	return strings.Join(parts[:2], ".")
}

func excerpt(value string) string {
	value = strings.TrimSpace(value)
	runes := []rune(value)
	if len(runes) > 12000 {
		return string(runes[:12000]) + "\n\n…更多内容请查看官方原文。"
	}
	return value
}

func (f *Fetcher) Fetch(ctx context.Context, name string) (Release, error) {
	return f.FetchRange(ctx, name, "")
}

func (f *Fetcher) FetchRange(ctx context.Context, name, scope string) (Release, error) {
	if !ValidRange(scope) || (scope != "" && !RangeService(name)) {
		return Release{}, ErrRangeInput
	}
	var release Release
	var err error
	switch name {
	case "nginx":
		release, err = f.nginx(ctx, scope)
	case "php-fpm":
		release, err = f.php(ctx, scope)
	case "mariadb":
		release, err = f.mariadb(ctx, scope)
	case "mysql":
		release, err = f.mysql(ctx)
	case "redis":
		release, err = f.redis(ctx)
	case "python3":
		release, err = f.python(ctx, scope)
	case "go":
		release, err = f.golang(ctx)
	case "node":
		release, err = f.node(ctx, scope)
	case "codex":
		release, err = f.codex(ctx)
	case "phpmyadmin":
		release, err = f.phpMyAdmin(ctx)
	case "composer":
		release, err = f.composer(ctx)
	default:
		return Release{}, fmt.Errorf("不支持的服务")
	}
	if err != nil {
		return Release{}, err
	}
	if !stableVersion.MatchString(release.Version) && !(name == "go" && seriesVersion.MatchString(release.Version)) {
		return Release{}, fmt.Errorf("未找到可识别的稳定版本")
	}
	release.Changelog = excerpt(release.Changelog)
	return release, nil
}

func (f *Fetcher) notes(ctx context.Context, release *Release, url string, transform func(string) string) {
	data, err := f.get(ctx, url)
	if err != nil {
		release.ChangelogError = err.Error()
		return
	}
	text := transform(string(data))
	if strings.TrimSpace(text) == "" {
		release.ChangelogError = "更新日志格式无法识别，请查看官方原文"
		return
	}
	release.Changelog = text
}

func (f *Fetcher) nginx(ctx context.Context, scope string) (Release, error) {
	source := "https://nginx.org/en/download.html"
	explicitSeries := strings.Contains(scope, ".")
	if explicitSeries {
		source = "https://nginx.org/download/"
	}
	data, err := f.get(ctx, source)
	if err != nil {
		return Release{}, err
	}
	r := Release{SourceURL: source}
	if scope == "" {
		match := regexp.MustCompile(`(?s)Stable version</h4>.*?nginx-([0-9]+\.[0-9]+\.[0-9]+)\.tar\.gz`).FindSubmatch(data)
		if len(match) == 2 {
			r.Version = string(match[1])
		}
	} else {
		candidates := string(data)
		if !explicitSeries {
			// Only Stable and Legacy sections are eligible for a major-only range.
			start := strings.Index(candidates, "Stable version</h4>")
			if start < 0 {
				return Release{}, fmt.Errorf("未找到 Nginx 稳定分支")
			}
			candidates = candidates[start:]
			if end := strings.Index(candidates, "Source Code</h4>"); end >= 0 {
				candidates = candidates[:end]
			}
		}
		for _, match := range regexp.MustCompile(`nginx-([0-9]+\.[0-9]+\.[0-9]+)\.tar\.gz`).FindAllStringSubmatch(candidates, -1) {
			if matchesRange(match[1], scope) && newer(match[1], r.Version) {
				r.Version = match[1]
			}
		}
	}
	if r.Version == "" {
		return Release{}, fmt.Errorf("未找到范围 %q 内的 Nginx 正式版本", scope)
	}
	r.ChangelogURL = "https://nginx.org/en/CHANGES-" + series(r.Version)
	if explicitSeries {
		minor, _ := strconv.Atoi(strings.Split(r.Version, ".")[1])
		if minor%2 == 1 {
			r.ChangelogURL = "https://nginx.org/en/CHANGES"
		}
	}
	f.notes(ctx, &r, r.ChangelogURL, func(text string) string {
		match := regexp.MustCompile(`Changes with nginx ` + regexp.QuoteMeta(r.Version) + `(?:[\t \r\n]|$)`).FindStringIndex(text)
		if match == nil {
			return ""
		}
		text = text[match[0]:]
		if end := strings.Index(text[1:], "Changes with nginx "); end >= 0 {
			text = text[:end+1]
		}
		return text
	})
	return r, nil
}

func (f *Fetcher) php(ctx context.Context, scope string) (Release, error) {
	source := "https://www.php.net/releases/index.php?json"
	if scope != "" {
		source += "&version=" + scope + "&max=1"
	}
	var versions map[string]struct {
		Version string `json:"version"`
	}
	if err := f.json(ctx, source, &versions); err != nil {
		return Release{}, err
	}
	r := Release{SourceURL: "https://www.php.net/downloads.php"}
	for key, v := range versions {
		version := v.Version
		if scope != "" {
			// The filtered API uses full release versions as object keys.
			version = key
		}
		if stableVersion.MatchString(version) && matchesRange(version, scope) && newer(version, r.Version) {
			r.Version = version
		}
	}
	if r.Version == "" {
		return Release{}, fmt.Errorf("未找到范围 %q 内的 PHP 正式版本", scope)
	}
	r.ChangelogURL = "https://www.php.net/ChangeLog-" + strings.Split(r.Version, ".")[0] + ".php#" + r.Version
	f.notes(ctx, &r, "https://raw.githubusercontent.com/php/php-src/php-"+r.Version+"/NEWS", func(text string) string {
		headings := regexp.MustCompile(`(?m)^\d{1,2} [A-Za-z]+ \d{4}, PHP `).FindAllStringIndex(text, -1)
		if len(headings) == 0 {
			return ""
		}
		start, end := headings[0][0], len(text)
		if len(headings) > 1 {
			end = headings[1][0]
		}
		section := text[start:end]
		if !strings.Contains(strings.SplitN(section, "\n", 2)[0], "PHP "+r.Version) {
			return ""
		}
		return section
	})
	return r, nil
}

func (f *Fetcher) mariadb(ctx context.Context, scope string) (Release, error) {
	latestSeries := scope
	if !strings.Contains(scope, ".") {
		latestSeries = ""
		var majors struct {
			Releases []struct {
				ID     string `json:"release_id"`
				Status string `json:"release_status"`
			} `json:"major_releases"`
		}
		if err := f.json(ctx, "https://downloads.mariadb.org/rest-api/mariadb/", &majors); err != nil {
			return Release{}, err
		}
		for _, v := range majors.Releases {
			if v.Status == "Stable" && seriesVersion.MatchString(v.ID) && matchesRange(v.ID, scope) && newer(v.ID, latestSeries) {
				latestSeries = v.ID
			}
		}
		if latestSeries == "" {
			return Release{}, fmt.Errorf("未找到范围 %q 内的 MariaDB 稳定分支", scope)
		}
	}
	var versions struct {
		Releases map[string]struct {
			Name string `json:"release_name"`
		} `json:"releases"`
	}
	if err := f.json(ctx, "https://downloads.mariadb.org/rest-api/mariadb/"+latestSeries+"/", &versions); err != nil {
		return Release{}, err
	}
	r := Release{SourceURL: "https://mariadb.org/download/"}
	for version, metadata := range versions.Releases {
		prerelease := prereleaseName.MatchString(metadata.Name)
		if !prerelease && stableVersion.MatchString(version) && series(version) == latestSeries && newer(version, r.Version) {
			r.Version = version
		}
	}
	if r.Version == "" {
		return Release{}, fmt.Errorf("未找到范围 %q 内的 MariaDB 正式版本", scope)
	}
	r.ChangelogURL = "https://mariadb.com/docs/release-notes/community-server/changelogs/" + latestSeries + "/" + r.Version
	f.notes(ctx, &r, r.ChangelogURL+".md", func(text string) string {
		if start := strings.Index(text, "# MariaDB "+r.Version); start >= 0 {
			return text[start:]
		}
		return ""
	})
	return r, nil
}

func (f *Fetcher) mysql(ctx context.Context) (Release, error) {
	var tags []struct {
		Ref string `json:"ref"`
	}
	if err := f.json(ctx, "https://api.github.com/repos/mysql/mysql-server/git/matching-refs/tags/mysql-", &tags); err != nil {
		return Release{}, err
	}
	r := Release{SourceURL: "https://github.com/mysql/mysql-server/tags"}
	for _, tag := range tags {
		version := strings.TrimPrefix(tag.Ref, "refs/tags/mysql-")
		if stableVersion.MatchString(version) && newer(version, r.Version) {
			r.Version = version
		}
	}
	if r.Version == "" {
		return Release{}, fmt.Errorf("未找到 MySQL 正式版本")
	}
	branch := series(r.Version)
	r.ChangelogURL = "https://dev.mysql.com/doc/relnotes/mysql/" + branch + "/en/news-" + strings.ReplaceAll(r.Version, ".", "-") + ".html"
	f.notes(ctx, &r, r.ChangelogURL, mysqlNotes)
	return r, nil
}

func (f *Fetcher) redis(ctx context.Context) (Release, error) {
	var release struct {
		Tag        string `json:"tag_name"`
		Body       string `json:"body"`
		Prerelease bool   `json:"prerelease"`
		Draft      bool   `json:"draft"`
	}
	if err := f.json(ctx, "https://api.github.com/repos/redis/redis/releases/latest", &release); err != nil {
		return Release{}, err
	}
	version := strings.TrimPrefix(release.Tag, "v")
	if release.Prerelease || release.Draft || !stableVersion.MatchString(version) {
		return Release{}, fmt.Errorf("未找到 Redis 稳定版本")
	}
	r := Release{Version: version, SourceURL: "https://github.com/redis/redis/releases", ChangelogURL: "https://github.com/redis/redis/releases/tag/" + release.Tag, Changelog: release.Body}
	if strings.TrimSpace(r.Changelog) == "" {
		f.notes(ctx, &r, "https://raw.githubusercontent.com/redis/redis/"+release.Tag+"/00-RELEASENOTES", func(text string) string { return text })
	}
	return r, nil
}

// Extract documentation text, discarding scripts, navigation and all markup.
// Upstream HTML is never sent to the browser as executable content.
func mysqlNotes(text string) string {
	root, err := html.Parse(strings.NewReader(text))
	if err != nil {
		return ""
	}
	var find func(*html.Node) *html.Node
	find = func(node *html.Node) *html.Node {
		for _, attr := range node.Attr {
			if attr.Key == "id" && attr.Val == "docs-body" {
				return node
			}
		}
		for child := node.FirstChild; child != nil; child = child.NextSibling {
			if found := find(child); found != nil {
				return found
			}
		}
		return nil
	}
	content := find(root)
	if content == nil {
		return ""
	}
	var out strings.Builder
	var walk func(*html.Node)
	walk = func(node *html.Node) {
		if node.Type == html.ElementNode {
			switch node.Data {
			case "script", "style", "nav", "header", "footer":
				return
			case "p", "li", "h1", "h2", "h3", "div", "br":
				out.WriteByte('\n')
			}
		}
		if node.Type == html.TextNode {
			out.WriteString(strings.Join(strings.Fields(node.Data), " "))
			out.WriteByte(' ')
		}
		for child := node.FirstChild; child != nil; child = child.NextSibling {
			walk(child)
		}
	}
	walk(content)
	var lines []string
	for _, line := range strings.Split(out.String(), "\n") {
		if line = strings.TrimSpace(line); line != "" {
			lines = append(lines, line)
		}
	}
	return strings.Join(lines, "\n")
}
