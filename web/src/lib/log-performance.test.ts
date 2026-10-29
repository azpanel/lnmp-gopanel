import { afterEach, beforeEach, expect, it, vi } from "vitest";
import { readLogWindow, recordLogRender } from "./log-performance";

const source = {
  lines: [{ offset: "0", text: "private log content" }],
  start: "start",
  end: "end",
  from: "0",
  to: "20",
  replace_from: "0",
  size: 20,
  modified: "2026-10-30T00:00:00Z",
  has_before: false,
  has_after: false,
  limited: false,
};
let frames: Map<number, FrameRequestCallback>;
let nextFrame: number;
function paint() {
  for (let i = 0; i < 2; i++) {
    const callbacks = [...frames.values()];
    frames.clear();
    for (const callback of callbacks) callback(performance.now());
  }
}

beforeEach(() => {
  performance.clearMeasures("log-read");
  frames = new Map();
  nextFrame = 0;
  vi.stubGlobal("location", { origin: "https://panel.test" });
  vi.stubGlobal("requestAnimationFrame", (callback: FrameRequestCallback) => {
    frames.set(++nextFrame, callback);
    return nextFrame;
  });
  vi.stubGlobal("cancelAnimationFrame", (id: number) => frames.delete(id));
  vi.spyOn(console, "debug").mockImplementation(() => {});
  vi.stubGlobal(
    "fetch",
    vi.fn(async () => {
      const now = performance.now();
      vi.spyOn(performance, "getEntriesByType").mockReturnValue([
        {
          name: "https://panel.test/api/v1/logs/read",
          startTime: now,
          responseStart: now,
          responseEnd: now,
          encodedBodySize: 100,
          decodedBodySize: 1000,
        } as PerformanceResourceTiming,
      ]);
      return new Response(JSON.stringify(source), {
        headers: {
          "X-Log-Request-ID": "test-request-id",
          "Server-Timing": "read;dur=2.000, geo;dur=1.000",
        },
      });
    }),
  );
});
afterEach(() => {
  vi.restoreAllMocks();
  vi.unstubAllGlobals();
  performance.clearMeasures("log-read");
});

it("correlates network and server timings with the first paint without log contents", async () => {
  const window = await readLogWindow("tail", { method: "POST" });
  expect(window).toEqual(source);
  recordLogRender([window]);
  expect(performance.getEntriesByName("log-read")).toHaveLength(0);
  paint();
  const entries = performance.getEntriesByName(
    "log-read",
  ) as PerformanceMeasure[];
  expect(entries).toHaveLength(1);
  expect(entries[0].detail).toMatchObject({
    requestId: "test-request-id",
    mode: "tail",
    encodedBytes: 100,
    decodedBytes: 1000,
    serverTiming: "read;dur=2.000, geo;dur=1.000",
  });
  for (const field of [
    "headersMs",
    "downloadMs",
    "decodeMs",
    "renderMs",
    "totalMs",
  ])
    expect(entries[0].detail[field]).toBeGreaterThanOrEqual(0);
  expect(JSON.stringify(entries[0].detail)).not.toContain(
    "private log content",
  );
  recordLogRender([window]);
  paint();
  expect(performance.getEntriesByName("log-read")).toHaveLength(1);
  expect(console.debug).toHaveBeenCalledTimes(1);
});

it("cancels stale render callbacks when switching files", async () => {
  const window = await readLogWindow("tail", { method: "POST" });
  const cancel = recordLogRender([window]);
  cancel?.();
  paint();
  expect(performance.getEntriesByName("log-read")).toHaveLength(0);
  recordLogRender([window]);
  paint();
  expect(performance.getEntriesByName("log-read")).toHaveLength(1);
});

it("bounds performance history and keeps fast polls out of the console", async () => {
  for (let i = 0; i < 66; i++) {
    const window = await readLogWindow("after", { method: "POST" });
    recordLogRender([window]);
    paint();
    expect(performance.getEntriesByName("log-read").length).toBeLessThanOrEqual(
      64,
    );
  }
  expect(console.debug).not.toHaveBeenCalled();
});
