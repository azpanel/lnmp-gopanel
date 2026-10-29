import { describe, expect, it } from "vitest";
import { formatBuildTime } from "./build-info";

describe("build time", () => {
  it("always uses Beijing time, including date rollover and midnight", () => {
    expect(formatBuildTime("2026-10-29T01:45:00Z")).toBe(
      "2026-10-29 09:45:00（UTC+8）",
    );
    expect(formatBuildTime("2026-12-31T16:00:00Z")).toBe(
      "2027-01-01 00:00:00（UTC+8）",
    );
    expect(formatBuildTime("2026-10-29T09:45:00+08:00")).toBe(
      "2026-10-29 09:45:00（UTC+8）",
    );
  });

  it("shows unknown for development builds and invalid timestamps", () => {
    expect(formatBuildTime()).toBe("未知");
    expect(formatBuildTime("")).toBe("未知");
    expect(formatBuildTime("invalid")).toBe("未知");
  });
});
