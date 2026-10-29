import { describe, expect, it } from "vitest";
import { detectUserAgent } from "./user-agent";
import { parseNginxAccess } from "./nginx-access";

const desktop =
  "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko)";
const iphone =
  "Mozilla/5.0 (iPhone; CPU iPhone OS 18_0 like Mac OS X) AppleWebKit/605.1.15 (KHTML, like Gecko)";
const android =
  "Mozilla/5.0 (Linux; Android 14; Pixel 8) AppleWebKit/537.36 (KHTML, like Gecko)";

describe("User-Agent identification", () => {
  it.each([
    ["chrome", `${desktop} Chrome/130.0.0.0 Safari/537.36`],
    ["chrome", `${android} Chrome/130.0.0.0 Mobile Safari/537.36`],
    ["chrome", `${iphone} CriOS/130.0.0.0 Mobile/15E148 Safari/604.1`],
    ["edge", `${desktop} Chrome/130.0.0.0 Safari/537.36 Edg/130.0.0.0`],
    ["edge", `${android} Chrome/130.0.0.0 Mobile Safari/537.36 EdgA/130.0.0.0`],
    [
      "edge",
      `${iphone} Version/18.0 EdgiOS/130.0.0.0 Mobile/15E148 Safari/605.1.15`,
    ],
    ["edge", `${desktop} Chrome/42.0.2311.135 Safari/537.36 Edge/12.246`],
    [
      "firefox",
      "Mozilla/5.0 (Windows NT 10.0; Win64; x64; rv:132.0) Gecko/20100101 Firefox/132.0",
    ],
    [
      "firefox",
      "Mozilla/5.0 (Android 14; Mobile; rv:132.0) Gecko/132.0 Firefox/132.0",
    ],
    ["firefox", `${iphone} FxiOS/132.0 Mobile/15E148 Safari/605.1.15`],
    [
      "safari",
      "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/605.1.15 (KHTML, like Gecko) Version/18.0 Safari/605.1.15",
    ],
    ["safari", `${iphone} Version/18.0 Mobile/15E148 Safari/604.1`],
    ["opera", `${desktop} Chrome/130.0.0.0 Safari/537.36 OPR/115.0.0.0`],
    ["opera", `${android} Chrome/130.0.0.0 Mobile Safari/537.36 OPR/85.0.0.0`],
    ["opera", `${iphone} OPiOS/3.0 Mobile/15E148 Safari/9537.53`],
    [
      "opera",
      "Opera/9.80 (Windows NT 6.1; U; en) Presto/2.12.388 Version/12.18",
    ],
    ["curl", "curl/8.10.1"],
    ["wget", "Wget/1.21.4"],
    ["requests", "python-requests/2.32.3"],
    ["go", "Go-http-client/1.1"],
    ["go", "Go-http-client/2.0"],
    ["okhttp", "okhttp/4.12.0"],
    ["axios", "axios/1.7.7"],
  ])("identifies %s from %s", (id, ua) => {
    const brand = detectUserAgent(ua);
    expect(brand?.id).toBe(id);
    expect(brand?.name).toBeTruthy();
    expect(brand?.icon).toMatch(/^\/brands\/[\w-]+\.(svg|png)$/);
  });

  it("accepts casing and surrounding whitespace without changing the UA", () => {
    const ua = "  CURL/8.10.1  ";
    expect(detectUserAgent(ua)?.id).toBe("curl");
    expect(ua).toBe("  CURL/8.10.1  ");
  });

  it.each([
    "",
    " ",
    "-",
    "Mozilla/5.0",
    "Test browser",
    "Safari/605.1.15",
    "AppleWebKit/537.36 (KHTML, like Gecko)",
    "curl",
    "axios/latest",
    "NotChrome/130.0 NotFirefox/132.0 NotEdg/130.0 NotOPR/115.0",
    "not-curl/8.10.1",
    "my-python-requests/2.32.3",
    "<script>window.logInjected=true</script>",
    "a".repeat(65536),
  ])("leaves unsupported values unidentified (%#)", (ua) => {
    expect(detectUserAgent(ua)).toBeNull();
  });

  it("only inspects the parsed UA, not product names in requests or referers", () => {
    const entry = parseNginxAccess(
      '192.0.2.1 - - [27/Oct/2026:14:23:45 +0800] "GET /curl/8.10.1 HTTP/1.1" 200 123 "https://example.com/Chrome/130.0" "unknown"',
    );
    expect(entry).not.toBeNull();
    expect(detectUserAgent(entry!.userAgent)).toBeNull();
  });
});
