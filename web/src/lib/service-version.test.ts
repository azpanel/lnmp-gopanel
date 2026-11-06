import { describe, expect, it } from "vitest";
import { hasNewerStableVersion } from "./service-version";

describe("hasNewerStableVersion", () => {
  it.each([
    ["1.9.9", "1.10.0", true],
    ["8.3.9", "8.3.10", true],
    ["22.23.0", "24.0.0", true],
    ["1.26", "1.26.1", true],
    ["1.26", "1.26.0", false],
    ["8.3.10", "8.3.10", false],
    ["8.4.1", "8.3.20", false],
    ["3.14.0rc1", "3.14.0", true],
    ["1.26rc2", "1.26.0", true],
    ["0.100.0-beta.1", "0.100.0", true],
    ["3.15.0a1", "3.14.0", false],
    ["22.1.0+build.1", "22.1.0", false],
    ["", "1.2.3", false],
    [undefined, "1.2.3", false],
    ["1.2.3", undefined, false],
    ["unknown", "1.2.3", false],
    ["1.2.3", "1.3.0-rc1", false],
  ])("compares %s with %s", (installed, latest, expected) => {
    expect(hasNewerStableVersion(installed, latest)).toBe(expected);
  });
});
