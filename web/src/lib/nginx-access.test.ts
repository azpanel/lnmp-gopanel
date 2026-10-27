import { describe, expect, it } from "vitest";
import {
  isNginxLog,
  matchesHTTPStatus,
  parseNginxAccess,
} from "./nginx-access";

const common =
  '2001:db8::1 - alice [27/Oct/2026:14:23:45 +0800] "GET /hello?q=404 HTTP/2.0" 200 123';
const combined = `${common} "https://example.com/" "Mozilla/5.0"`;

describe("Nginx access log parsing", () => {
  it("parses common logs, IPv6 and the original timezone", () => {
    expect(parseNginxAccess(common)).toEqual({
      client: "2001:db8::1",
      time: "27/Oct/2026:14:23:45 +0800",
      request: "GET /hello?q=404 HTTP/2.0",
      status: "200",
      bytes: "123",
      referer: "-",
      userAgent: "-",
    });
  });

  it("reads combined fields while accepting additional trailing fields", () => {
    expect(parseNginxAccess(`${combined} "192.0.2.1" 0.123`)).toMatchObject({
      referer: "https://example.com/",
      userAgent: "Mozilla/5.0",
    });
    expect(parseNginxAccess(`${common} 0.123`)).toMatchObject({
      status: "200",
    });
  });

  it("handles escaped quotes and backslashes without decoding control escapes", () => {
    const log = `${common} "-" ${String.raw`"agent \"quoted\" \\path\x0A<script>"`}`;
    expect(parseNginxAccess(log)?.userAgent).toBe(
      String.raw`agent "quoted" \path\x0A<script>`,
    );
  });

  it("preserves missing values, unusual status codes and large byte counts", () => {
    expect(
      parseNginxAccess(
        '192.0.2.1 - - [27/Oct/2026:06:23:45 -0800] "-" 499 - "-" "-"',
      ),
    ).toMatchObject({ request: "-", status: "499", bytes: "-" });
    expect(
      parseNginxAccess(common.replace("200 123", "000 9007199254740993")),
    ).toMatchObject({ status: "000", bytes: "9007199254740993" });
  });

  it.each([
    "",
    "2026/10/27 [error] 404: request failed",
    '{"status":200,"request":"GET /"}',
    common.replace("200 123", "2000 123"),
    common.replace("200 123", "200 123oops"),
    common.replace('HTTP/2.0"', "HTTP/2.0"),
    `${common} "-" "unfinished`,
    `${common} "-""agent"`,
    `${combined}oops`,
  ])("leaves malformed and unsupported lines as raw text: %s", (text) => {
    expect(parseNginxAccess(text)).toBeNull();
  });

  it("handles bounded long fields and incomplete lines without throwing", () => {
    const long = combined.replace("Mozilla/5.0", "a".repeat(65536));
    expect(parseNginxAccess(long)?.userAgent).toHaveLength(65536);
    expect(parseNginxAccess(long.slice(0, 65536))).toBeNull();
    expect(parseNginxAccess(common)?.status).toBe("200");
  });
});

describe("HTTP status filters", () => {
  const entry = (status: string) =>
    parseNginxAccess(common.replace("200 123", `${status} 123`));

  it("unions exact codes and classes without matching codes inside requests", () => {
    expect(matchesHTTPStatus(entry("200"), ["404"])).toBe(false);
    expect(matchesHTTPStatus(entry("404"), ["404", "5xx"])).toBe(true);
    expect(matchesHTTPStatus(entry("502"), ["404", "5xx"])).toBe(true);
    expect(matchesHTTPStatus(entry("403"), ["404", "5xx"])).toBe(false);
    expect(matchesHTTPStatus(entry("499"), ["4xx", "499"])).toBe(true);
    expect(matchesHTTPStatus(entry("000"), ["000"])).toBe(true);
  });

  it("includes unparsed lines only when no filters are selected", () => {
    expect(matchesHTTPStatus(null, [])).toBe(true);
    expect(matchesHTTPStatus(null, ["2xx"])).toBe(false);
    expect(matchesHTTPStatus(undefined, ["200"])).toBe(false);
    expect(matchesHTTPStatus(entry("200"), [])).toBe(true);
  });

  it("only offers formatting for files directly inside Nginx log directories", () => {
    expect(isNginxLog("/home/wwwlogs/site.log")).toBe(true);
    expect(isNginxLog("/usr/local/nginx/logs/gopanel/error.log")).toBe(true);
    expect(isNginxLog("/home/wwwlogs/nested/site.log")).toBe(false);
    expect(isNginxLog("/var/log/mysql/mariadb-slow.log")).toBe(false);
    expect(isNginxLog("/usr/local/php/var/log/php_errors.log")).toBe(false);
  });
});
