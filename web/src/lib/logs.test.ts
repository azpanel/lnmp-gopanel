import { describe, expect, it, vi, afterEach } from "vitest";
import {
  loadLogRules,
  saveLogRules,
  mergeLogWindow,
  logLines,
  validateLogRules,
} from "./logs";
import type { LogWindow, LogRule } from "./logs";
const rule: LogRule = {
  kind: "keyword",
  action: "hide",
  pattern: "secret",
  ignore_case: false,
  enabled: true,
};
function window(from: number, to: number, offsets: number[] = []): LogWindow {
  return {
    from: String(from),
    to: String(to),
    replace_from: String(from),
    start: `c${from}`,
    end: `c${to}`,
    size: to,
    modified: "",
    has_before: from > 0,
    has_after: false,
    limited: false,
    lines: offsets.map((offset) => ({
      offset: String(offset),
      text: `line ${offset}`,
    })),
  };
}
afterEach(() => vi.unstubAllGlobals());
describe("bounded log windows", () => {
  it("replaces an unfinished line, including when the update is hidden", () => {
    const first = window(0, 10, [0, 5]);
    const update = { ...window(10, 20, [15]), replace_from: "5" };
    expect(
      logLines(mergeLogWindow([first], update).pages).map((r) => r.offset),
    ).toEqual(["0", "15"]);
  });
  it("deduplicates overlap and keeps byte offsets precise above 2^53", () => {
    const a = window(0, 10, [0, 5]);
    const b = {
      ...window(5, 20, [5, 15]),
      lines: [
        { offset: "5", text: "updated" },
        { offset: "9007199254740993", text: "last" },
      ],
    };
    const lines = logLines(mergeLogWindow([a], b).pages);
    expect(lines.map((r) => r.offset)).toEqual(["0", "5", "9007199254740993"]);
    expect(lines[1].text).toBe("updated");
  });
  it("evicts the opposite end in both browsing directions", () => {
    const pages = Array.from({ length: 8 }, (_, i) =>
      window(i * 262144, (i + 1) * 262144),
    );
    const appended = mergeLogWindow(pages, window(2097152, 2359296));
    expect(appended.evicted).toBe(true);
    expect(appended.pages[0].from).toBe("262144");
    const prepended = mergeLogWindow(pages.slice(1), window(0, 262144), true);
    expect(prepended.evicted).toBe(false);
    const bounded = mergeLogWindow(pages, window(0, 262144), true);
    expect(bounded.evicted).toBe(true);
    expect(bounded.pages.at(-1)?.to).toBe("1835008");
  });
  it("bounds dense rows and tiny polling pages", () => {
    let pages: LogWindow[] = [];
    for (let i = 0; i < 100; i++)
      pages = mergeLogWindow(pages, window(i, i + 1, [i])).pages;
    expect(pages.length).toBe(64);
    const dense = Array.from({ length: 6 }, (_, i) =>
      window(
        i * 4000,
        (i + 1) * 4000,
        Array.from({ length: 4000 }, (_, j) => i * 4000 + j),
      ),
    );
    expect(
      mergeLogWindow(dense.slice(0, 5), dense[5]).pages.reduce(
        (n, p) => n + p.lines.length,
        0,
      ),
    ).toBe(20000);
  });
});
describe("per-file filter preferences", () => {
  it("persists independently and ignores corrupt storage", () => {
    const data = new Map<string, string>();
    vi.stubGlobal("localStorage", {
      getItem: (key: string) => data.get(key) ?? null,
      setItem: (key: string, value: string) => data.set(key, value),
    });
    saveLogRules("/a.log", [rule]);
    expect(loadLogRules("/a.log")).toEqual([rule]);
    expect(loadLogRules("/b.log")).toEqual([]);
    data.set("log-rules:/b.log", "bad JSON");
    expect(loadLogRules("/b.log")).toEqual([]);
    data.set("log-rules:/b.log", JSON.stringify([{ enabled: true }]));
    expect(loadLogRules("/b.log")).toEqual([]);
  });
  it("validates byte lengths and enabled empty rules", () => {
    expect(validateLogRules([{ ...rule, pattern: "字".repeat(86) }])).toContain(
      "256",
    );
    expect(validateLogRules([{ ...rule, pattern: "" }])).toContain("不能为空");
    expect(
      validateLogRules([{ ...rule, pattern: "", enabled: false }]),
    ).toBeUndefined();
    expect(validateLogRules(Array(21).fill(rule))).toContain("20");
  });
});
