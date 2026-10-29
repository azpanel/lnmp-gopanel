import { api } from "./api";
import type { LogRequest, LogWindow } from "./logs";

type ReadTiming = {
  mode: LogRequest["mode"];
  started: number;
  headers: number;
  decoded: number;
  requestId: string;
  serverTiming: string;
  reported: boolean;
};
const timings = new WeakMap<LogWindow, ReadTiming>();
const measureName = "log-read";
const recentResources: PerformanceResourceTiming[] = [];
let observer: PerformanceObserver | undefined;

function observeLogResources() {
  if (observer || typeof PerformanceObserver === "undefined") return;
  // The browser's shared resource timeline can fill up during long polling.
  // Keep a separate bounded list so later slow requests remain diagnosable.
  observer = new PerformanceObserver((list) => {
    for (const entry of list.getEntries()) {
      if (entry.name !== `${location.origin}/api/v1/logs/read`) continue;
      recentResources.push(entry as PerformanceResourceTiming);
      if (recentResources.length > 64) recentResources.shift();
    }
  });
  observer.observe({ type: "resource", buffered: true });
}

export async function readLogWindow(
  mode: LogRequest["mode"],
  options: RequestInit,
): Promise<LogWindow> {
  observeLogResources();
  const timing: ReadTiming = {
    mode,
    started: performance.now(),
    headers: 0,
    decoded: 0,
    requestId: "",
    serverTiming: "",
    reported: false,
  };
  const window = await api<LogWindow>("/logs/read", options, (response) => {
    timing.headers = performance.now();
    timing.requestId = response.headers.get("X-Log-Request-ID") ?? "";
    timing.serverTiming = response.headers.get("Server-Timing") ?? "";
  });
  timing.decoded = performance.now();
  timings.set(window, timing);
  return window;
}

// Called after React commits the new pages. Two frames allow layout and a paint
// before recording visibility; a hidden tab naturally waits until it can paint.
export function recordLogRender(pages: LogWindow[]) {
  const pending = pages
    .map((page) => timings.get(page))
    .filter((timing): timing is ReadTiming => !!timing && !timing.reported);
  if (!pending.length) return;
  let frame = requestAnimationFrame(() => {
    frame = requestAnimationFrame(() => {
      const visible = performance.now();
      const resources = [
        ...(performance.getEntriesByType(
          "resource",
        ) as PerformanceResourceTiming[]),
        ...recentResources,
      ];
      for (const timing of pending) {
        if (timing.reported) continue;
        timing.reported = true;
        const resource = resources
          .slice()
          .reverse()
          .find(
            (entry) =>
              entry.name === `${location.origin}/api/v1/logs/read` &&
              entry.startTime >= timing.started &&
              entry.responseEnd <= timing.decoded,
          );
        const detail = {
          requestId: timing.requestId,
          mode: timing.mode,
          headersMs: timing.headers - timing.started,
          downloadMs: resource
            ? resource.responseEnd - resource.responseStart
            : null,
          decodeMs: resource ? timing.decoded - resource.responseEnd : null,
          renderMs: visible - timing.decoded,
          totalMs: visible - timing.started,
          encodedBytes: resource?.encodedBodySize ?? null,
          decodedBytes: resource?.decodedBodySize ?? null,
          serverTiming: timing.serverTiming,
        };
        // Polling must not grow the performance timeline without a bound.
        if (performance.getEntriesByName(measureName, "measure").length >= 64)
          performance.clearMeasures(measureName);
        performance.measure(measureName, {
          start: timing.started,
          end: visible,
          detail,
        });
        if (timing.mode === "tail" || detail.totalMs >= 500)
          console.debug("日志读取耗时", detail);
      }
    });
  });
  return () => cancelAnimationFrame(frame);
}
