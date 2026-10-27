import { describe, expect, it } from "vitest";
import { certificateCovers, splitDomains } from "./vhost-create";

describe("site certificate matching", () => {
  it("requires every hostname and matches only one wildcard label", () => {
    const names = ["example.com", "*.example.com"];
    expect(certificateCovers(names, ["EXAMPLE.COM.", "www.example.com"])).toBe(
      true,
    );
    expect(certificateCovers(names, ["a.b.example.com"])).toBe(false);
    expect(certificateCovers(names, ["www.example.com", "other.com"])).toBe(
      false,
    );
    expect(certificateCovers(["*.example.com"], ["example.com"])).toBe(false);
    expect(certificateCovers(names, [])).toBe(false);
  });
  it("normalizes international hostnames and parses alias separators", () => {
    expect(
      certificateCovers(["*.xn--fsqu00a.xn--0zwm56d"], ["www.例子.测试"]),
    ).toBe(true);
    expect(
      splitDomains(
        "www.example.com，api.example.com\nwww.example.com api.example.com",
      ),
    ).toEqual(["www.example.com", "api.example.com"]);
  });
});
