package releases

import (
	"context"
	"strings"
	"testing"
)

func TestOfficialSourcesSelectVersionsWithinRange(t *testing.T) {
	fixtures := map[string]string{
		"https://nginx.org/en/download.html":                                                 `<h4>Mainline version</h4><a>nginx-1.31.8.tar.gz</a><h4>Stable version</h4><a>nginx-1.30.6.tar.gz</a><h4>Legacy versions</h4><a>nginx-1.28.3.tar.gz</a><a>nginx-0.8.55.tar.gz</a>`,
		"https://nginx.org/download/":                                                        `<a>nginx-1.29.9.tar.gz</a><a>nginx-1.29.10.tar.gz</a><a>nginx-1.30.6.tar.gz</a><a>nginx-1.29.11-rc.tar.gz</a><a>nginx-1.28.3.tar.gz</a>`,
		"https://nginx.org/en/CHANGES-1.30":                                                  "Changes with nginx 1.30.6\nlatest fixes",
		"https://nginx.org/en/CHANGES-1.28":                                                  "Changes with nginx 1.28.3\nlatest fixes",
		"https://nginx.org/en/CHANGES-0.8":                                                   "Changes with nginx 0.8.55\nlatest fixes",
		"https://nginx.org/en/CHANGES":                                                       "Changes with nginx 1.31.8\nwrong fixes\nChanges with nginx 1.29.10\nlatest fixes\nChanges with nginx 1.29.9\nold fixes",
		"https://www.php.net/releases/index.php?json&version=8&max=1":                        `{"8.5.12":{},"8.5.9":{},"9.0.0":{},"8.6.0RC1":{}}`,
		"https://www.php.net/releases/index.php?json&version=8.3&max=1":                      `{"8.3.36":{},"8.3.9":{},"8.30.1":{},"8.3.37-rc":{}}`,
		"https://www.php.net/releases/index.php?json&version=7.4&max=1":                      `{"7.4.33":{}}`,
		"https://raw.githubusercontent.com/php/php-src/php-8.5.12/NEWS":                      "21 Oct 2026, PHP 8.5.12\nlatest fixes\n01 Oct 2026, PHP 8.5.11\nold fixes",
		"https://raw.githubusercontent.com/php/php-src/php-8.3.36/NEWS":                      "21 Oct 2026, PHP 8.3.36\nlatest fixes",
		"https://raw.githubusercontent.com/php/php-src/php-7.4.33/NEWS":                      "03 Nov 2022, PHP 7.4.33\nlatest fixes",
		"https://downloads.mariadb.org/rest-api/mariadb/":                                    `{"major_releases":[{"release_id":"12.3","release_status":"Stable"},{"release_id":"11.9","release_status":"RC"},{"release_id":"11.8","release_status":"Stable"},{"release_id":"11.4","release_status":"Stable"}]}`,
		"https://downloads.mariadb.org/rest-api/mariadb/11.8/":                               `{"releases":{"11.8.9":{},"11.8.10":{},"11.8.11-rc":{},"11.8.12":{"release_name":"MariaDB Server 11.8.12 RC"},"99.0.0":{}}}`,
		"https://downloads.mariadb.org/rest-api/mariadb/11.4/":                               `{"releases":{"11.4.13":{},"11.4.9":{},"11.40.1":{}}}`,
		"https://downloads.mariadb.org/rest-api/mariadb/10.5/":                               `{"releases":{"10.5.29":{}}}`,
		"https://mariadb.com/docs/release-notes/community-server/changelogs/11.8/11.8.10.md": "# MariaDB 11.8.10 Changelog\nlatest fixes",
		"https://mariadb.com/docs/release-notes/community-server/changelogs/11.4/11.4.13.md": "# MariaDB 11.4.13 Changelog\nlatest fixes",
		"https://mariadb.com/docs/release-notes/community-server/changelogs/10.5/10.5.29.md": "# MariaDB 10.5.29 Changelog\nlatest fixes",
	}
	for _, tc := range []struct{ name, scope, want string }{
		{"nginx", "1", "1.30.6"}, {"nginx", "0", "0.8.55"}, {"nginx", "1.28", "1.28.3"}, {"nginx", "1.29", "1.29.10"},
		{"php-fpm", "8", "8.5.12"}, {"php-fpm", "8.3", "8.3.36"}, {"php-fpm", "7.4", "7.4.33"},
		{"mariadb", "11", "11.8.10"}, {"mariadb", "11.4", "11.4.13"}, {"mariadb", "10.5", "10.5.29"},
	} {
		t.Run(tc.name+"/"+tc.scope, func(t *testing.T) {
			got, err := fixtureFetcher(t, fixtures).FetchRange(context.Background(), tc.name, tc.scope)
			if err != nil || got.Version != tc.want || got.ChangelogError != "" || !strings.Contains(got.Changelog, "latest fixes") || strings.Contains(got.Changelog, "old fixes") || strings.Contains(got.Changelog, "wrong fixes") {
				t.Fatalf("wrong scoped release: %+v %v", got, err)
			}
		})
	}
}

func TestRangesNeverFallBackToUnrestrictedOrPrereleases(t *testing.T) {
	for _, tc := range []struct{ name, scope, url, body string }{
		{"nginx", "9", "https://nginx.org/en/download.html", `<h4>Stable version</h4><a>nginx-1.30.6.tar.gz</a>`},
		{"nginx", "1.29", "https://nginx.org/download/", `<a>nginx-1.30.6.tar.gz</a><a>nginx-1.29.2-rc.tar.gz</a>`},
		{"php-fpm", "8.3", "https://www.php.net/releases/index.php?json&version=8.3&max=1", `{"8.5.12":{},"8.3.1RC1":{}}`},
		{"mariadb", "9", "https://downloads.mariadb.org/rest-api/mariadb/", `{"major_releases":[{"release_id":"11.4","release_status":"Stable"}]}`},
		{"mariadb", "13.1", "https://downloads.mariadb.org/rest-api/mariadb/13.1/", `{"releases":{"13.1.0":{"release_name":"MariaDB Server 13.1.0 Alpha"},"13.1.1":{"release_name":"MariaDB Server 13.1.1 RC"}}}`},
	} {
		t.Run(tc.name+"/"+tc.scope, func(t *testing.T) {
			_, err := fixtureFetcher(t, map[string]string{tc.url: tc.body}).FetchRange(context.Background(), tc.name, tc.scope)
			if err == nil || !strings.Contains(err.Error(), "未找到") {
				t.Fatalf("unexpected fallback: %v", err)
			}
		})
	}
}

func TestScopedChangelogFailureStillKeepsVersion(t *testing.T) {
	got, err := fixtureFetcher(t, map[string]string{
		"https://nginx.org/download/":  `<a>nginx-1.29.1.tar.gz</a>`,
		"https://nginx.org/en/CHANGES": "Changes with nginx 1.29.10\nwrong release",
	}).FetchRange(context.Background(), "nginx", "1.29")
	if err != nil || got.Version != "1.29.1" || got.Changelog != "" || got.ChangelogError == "" || got.ChangelogURL == "" {
		t.Fatalf("bad changelog failure: %+v %v", got, err)
	}
}

func TestComponentRangesSelectMatchingStableVersionAndChangelog(t *testing.T) {
	fixtures := map[string]string{
		"https://nodejs.org/dist/index.json":                                                      `[{"version":"v26.13.0"},{"version":"v22.23.10"},{"version":"v22.23.9"},{"version":"v22.24.1"},{"version":"v22.25.0-rc.1"},{"version":"v23.0.0"}]`,
		"https://raw.githubusercontent.com/nodejs/node/v22.24.1/doc/changelogs/CHANGELOG_V22.md":  "## Version 22.24.1 (LTS)\nlatest fixes\n## Version 22.24.0 (LTS)\nold fixes",
		"https://raw.githubusercontent.com/nodejs/node/v22.23.10/doc/changelogs/CHANGELOG_V22.md": "## Version 22.24.1 (LTS)\nwrong fixes\n## Version 22.23.10 (LTS)\nlatest fixes\n## Version 22.23.9 (LTS)\nold fixes",
		"https://www.python.org/downloads/":                                                       `<a>Python 3.15.2</a><a>Python 3.13.9</a><a>Python 3.13.18</a><a>Python 3.130.1</a><a>Python 3.13.19rc1</a><a>Python 3.13.20a1</a><a>Python 3.13.21b1</a><a>Python 2.7.18</a>`,
		"https://raw.githubusercontent.com/python/cpython/v3.130.1/Misc/NEWS.d/3.130.1.rst":       "latest fixes",
		"https://raw.githubusercontent.com/python/cpython/v3.13.18/Misc/NEWS.d/3.13.18.rst":       "latest fixes",
	}
	for _, tc := range []struct{ name, scope, want string }{
		{"node", "22", "22.24.1"}, {"node", "22.23", "22.23.10"},
		{"python3", "3", "3.130.1"}, {"python3", "3.13", "3.13.18"},
	} {
		t.Run(tc.name+"/"+tc.scope, func(t *testing.T) {
			got, err := fixtureFetcher(t, fixtures).FetchRange(context.Background(), tc.name, tc.scope)
			if err != nil || got.Version != tc.want || !strings.Contains(got.Changelog, "latest fixes") || got.ChangelogError != "" || strings.Contains(got.Changelog, "wrong fixes") || strings.Contains(got.Changelog, "old fixes") {
				t.Fatalf("wrong scoped component: %+v %v", got, err)
			}
			if !strings.Contains(got.ChangelogURL, tc.want) {
				t.Fatal("wrong changelog link", got.ChangelogURL)
			}
		})
	}
	for _, tc := range []struct{ name, scope string }{{"node", "2"}, {"node", "22.2"}, {"node", "22.25"}, {"python3", "2"}, {"python3", "3.1"}, {"python3", "3.16"}} {
		t.Run(tc.name+"/missing-"+tc.scope, func(t *testing.T) {
			if _, err := fixtureFetcher(t, fixtures).FetchRange(context.Background(), tc.name, tc.scope); err == nil || !strings.Contains(err.Error(), "未找到") {
				t.Fatalf("scope unexpectedly fell back: %v", err)
			}
		})
	}
}
