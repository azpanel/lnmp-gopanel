import { describe, expect, it } from "vitest";
import {
  certificateCovers,
  generateDatabasePassword,
  splitDomains,
} from "./vhost-create";

describe("database password generation", () => {
  it("generates distinct 24-character passwords with all four character classes", () => {
    const passwords = Array.from({ length: 8 }, () =>
      generateDatabasePassword(),
    );
    expect(new Set(passwords).size).toBe(passwords.length);
    for (const password of passwords) {
      expect(password).toHaveLength(24);
      expect(password).toMatch(/[A-Z]/);
      expect(password).toMatch(/[a-z]/);
      expect(password).toMatch(/[0-9]/);
      expect(password).toMatch(/[!@#$%*_+\-=]/);
      expect(password).not.toMatch(/[\s\x00'"\\]/);
    }
  });
});

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
