package releases

import (
	"context"
	"strings"
	"testing"
)

func TestComponentOfficialReleases(t *testing.T) {
	fixtures := map[string]string{
		"https://www.python.org/downloads/":                                                      `<a>Python 3.14.9</a><a>Python 3.15.2</a><a>Python 3.16.0rc1</a>`,
		"https://raw.githubusercontent.com/python/cpython/v3.15.2/Misc/NEWS.d/3.15.2.rst":        "latest fixes",
		"https://go.dev/dl/?mode=json":                                                           `[{"version":"go1.26.10","stable":true},{"version":"go1.27.3","stable":true},{"version":"go1.28rc1","stable":false}]`,
		"https://go.dev/doc/devel/release":                                                       `<h2 id="go1.27.0">go1.27.0 (release)</h2><p>major</p><p>go1.27.3 (released today) latest fixes<script>bad script</script></p><p>go1.27.2 old fixes</p>`,
		"https://nodejs.org/dist/index.json":                                                     `[{"version":"v24.22.1","lts":"Krypton"},{"version":"v26.12.1","lts":false},{"version":"v27.0.0-rc1"}]`,
		"https://raw.githubusercontent.com/nodejs/node/v26.12.1/doc/changelogs/CHANGELOG_V26.md": "# preamble\n<a>26.12.1</a>\n## 2026-10-21, Version 26.12.1 (Current)\nlatest fixes\n## Version 26.12.0 (Current)\nold fixes",
		"https://api.github.com/repos/openai/codex/releases/latest":                              `{"tag_name":"rust-v0.164.0","body":"latest fixes","prerelease":false,"draft":false}`,
	}
	for name, want := range map[string]string{"python3": "3.15.2", "go": "1.27.3", "node": "26.12.1", "codex": "0.164.0"} {
		t.Run(name, func(t *testing.T) {
			r, err := fixtureFetcher(t, fixtures).Fetch(context.Background(), name)
			if err != nil || r.Version != want || r.ChangelogError != "" || !strings.Contains(r.Changelog, "latest fixes") || r.ChangelogURL == "" {
				t.Fatal(r, err)
			}
			if strings.Contains(r.Changelog, "old fixes") || strings.Contains(r.Changelog, "bad script") {
				t.Fatal(r.Changelog)
			}
		})
	}
}
func TestComponentReleaseFailures(t *testing.T) {
	for _, body := range []string{`{"tag_name":"rust-v0.164.0","draft":true}`, `{"tag_name":"rust-v0.165.0-alpha.1","prerelease":true}`, `{"tag_name":"v0.164.0"}`} {
		if _, err := fixtureFetcher(t, map[string]string{"https://api.github.com/repos/openai/codex/releases/latest": body}).Fetch(context.Background(), "codex"); err == nil {
			t.Fatal("invalid release accepted", body)
		}
	}
	r, err := fixtureFetcher(t, map[string]string{"https://nodejs.org/dist/index.json": `[{"version":"v26.12.1"}]`}).Fetch(context.Background(), "node")
	if err != nil || r.Version != "26.12.1" || r.ChangelogError == "" || r.ChangelogURL == "" {
		t.Fatal(r, err)
	}
	r, err = fixtureFetcher(t, map[string]string{"https://go.dev/dl/?mode=json": `[{"version":"go1.28","stable":true}]`, "https://go.dev/doc/devel/release": `<h2>go1.28</h2><p>major fixes</p><h3>Minor revisions</h3><p>old fixes</p>`}).Fetch(context.Background(), "go")
	if err != nil || r.Version != "1.28" || !strings.Contains(r.Changelog, "major fixes") || strings.Contains(r.Changelog, "old fixes") {
		t.Fatal(r, err)
	}
}
