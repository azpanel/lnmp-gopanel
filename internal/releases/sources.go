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

type Fetcher struct{ client *http.Client }

func allowedURL(raw string) bool {
	req, err := http.NewRequest(http.MethodGet, raw, nil)
	if err != nil || req.URL.Scheme != "https" || req.URL.User != nil || req.URL.Port() != "" {
		return false
	}
	switch req.URL.Hostname() {
	case "nginx.org", "www.php.net", "downloads.mariadb.org", "mariadb.com", "api.github.com", "raw.githubusercontent.com", "dev.mysql.com":
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
	var release Release
	var err error
	switch name {
	case "nginx":
		release, err = f.nginx(ctx)
	case "php-fpm":
		release, err = f.php(ctx)
	case "mariadb":
		release, err = f.mariadb(ctx)
	case "mysql":
		release, err = f.mysql(ctx)
	case "redis":
		release, err = f.redis(ctx)
	default:
		return Release{}, fmt.Errorf("不支持的服务")
	}
	if err != nil {
		return Release{}, err
	}
	if !stableVersion.MatchString(release.Version) {
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

func (f *Fetcher) nginx(ctx context.Context) (Release, error) {
	const source = "https://nginx.org/en/download.html"
	data, err := f.get(ctx, source)
	if err != nil {
		return Release{}, err
	}
	match := regexp.MustCompile(`(?s)Stable version</h4>.*?nginx-([0-9]+\.[0-9]+\.[0-9]+)\.tar\.gz`).FindSubmatch(data)
	if len(match) != 2 {
		return Release{}, fmt.Errorf("未找到 Nginx 稳定版本")
	}
	r := Release{Version: string(match[1]), SourceURL: source}
	r.ChangelogURL = "https://nginx.org/en/CHANGES-" + series(r.Version)
	f.notes(ctx, &r, r.ChangelogURL, func(text string) string {
		start := strings.Index(text, "Changes with nginx "+r.Version)
		if start < 0 {
			return ""
		}
		text = text[start:]
		if end := strings.Index(text[1:], "Changes with nginx "); end >= 0 {
			text = text[:end+1]
		}
		return text
	})
	return r, nil
}

func (f *Fetcher) php(ctx context.Context) (Release, error) {
	var versions map[string]struct {
		Version string `json:"version"`
	}
	if err := f.json(ctx, "https://www.php.net/releases/index.php?json", &versions); err != nil {
		return Release{}, err
	}
	r := Release{SourceURL: "https://www.php.net/downloads.php"}
	for _, v := range versions {
		if stableVersion.MatchString(v.Version) && newer(v.Version, r.Version) {
			r.Version = v.Version
		}
	}
	if r.Version == "" {
		return Release{}, fmt.Errorf("未找到 PHP 稳定版本")
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

func (f *Fetcher) mariadb(ctx context.Context) (Release, error) {
	var majors struct {
		Releases []struct {
			ID     string `json:"release_id"`
			Status string `json:"release_status"`
		} `json:"major_releases"`
	}
	if err := f.json(ctx, "https://downloads.mariadb.org/rest-api/mariadb/", &majors); err != nil {
		return Release{}, err
	}
	latestSeries := ""
	for _, v := range majors.Releases {
		if v.Status == "Stable" && seriesVersion.MatchString(v.ID) && newer(v.ID, latestSeries) {
			latestSeries = v.ID
		}
	}
	if latestSeries == "" {
		return Release{}, fmt.Errorf("未找到 MariaDB 稳定分支")
	}
	var versions struct {
		Releases map[string]json.RawMessage `json:"releases"`
	}
	if err := f.json(ctx, "https://downloads.mariadb.org/rest-api/mariadb/"+latestSeries+"/", &versions); err != nil {
		return Release{}, err
	}
	r := Release{SourceURL: "https://mariadb.org/download/"}
	for version := range versions.Releases {
		if stableVersion.MatchString(version) && series(version) == latestSeries && newer(version, r.Version) {
			r.Version = version
		}
	}
	if r.Version == "" {
		return Release{}, fmt.Errorf("未找到 MariaDB 稳定版本")
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
