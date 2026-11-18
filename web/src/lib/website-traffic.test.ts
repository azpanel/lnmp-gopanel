import { afterEach, expect, it, vi } from "vitest";
import { readTrafficView, saveTrafficView } from "./website-traffic";

afterEach(() => vi.unstubAllGlobals());

it("restores valid preferences and falls back for missing or damaged storage", () => {
  for (const content of [
    null,
    "{",
    "null",
    "[]",
    '{"period":"month","chart":"bar"}',
  ]) {
    vi.stubGlobal("localStorage", { getItem: () => content });
    expect(readTrafficView()).toEqual({ period: "day", chart: "line" });
  }
  for (const period of ["day", "hour"] as const)
    for (const chart of ["line", "bar"] as const) {
      let saved = "";
      vi.stubGlobal("localStorage", {
        getItem: () => saved,
        setItem: (_key: string, value: string) => {
          saved = value;
        },
      });
      saveTrafficView({ period, chart });
      expect(readTrafficView()).toEqual({ period, chart });
    }
});

it("keeps the widget usable when storage access is denied", () => {
  vi.stubGlobal("localStorage", {
    getItem: () => {
      throw new Error("denied");
    },
    setItem: () => {
      throw new Error("denied");
    },
  });
  expect(readTrafficView()).toEqual({ period: "day", chart: "line" });
  expect(() => saveTrafficView({ period: "hour", chart: "bar" })).not.toThrow();
});
