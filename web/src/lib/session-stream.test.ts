import { afterEach, beforeEach, expect, it, vi } from "vitest";
import { beginSession, expireSession } from "./session-scope";
import { sessionStream } from "./session-stream";

const { refresh, fetchSession } = vi.hoisted(() => ({
  refresh: vi.fn(),
  fetchSession: vi.fn(),
}));
vi.mock("./api", () => ({ refreshStreamSession: refresh, fetchSession }));
class Source extends EventTarget {
  static OPEN = 1;
  static CLOSED = 2;
  static instances: Source[] = [];
  readyState = 0;
  onopen?: () => void;
  onerror?: () => void;
  constructor(public url: string) {
    super();
    Source.instances.push(this);
  }
  close() {
    this.readyState = 2;
  }
  open() {
    this.readyState = 1;
    this.onopen?.();
  }
  emit(name: string, value = {}) {
    this.dispatchEvent(new MessageEvent(name, { data: JSON.stringify(value) }));
  }
}
let streams: ReturnType<typeof sessionStream>[];
function connect(options: Partial<Parameters<typeof sessionStream>[0]> = {}) {
  const stream = sessionStream({
    url: () => "/metrics",
    events: {},
    onState: vi.fn(),
    ...options,
  });
  streams.push(stream);
  return stream;
}
beforeEach(() => {
  vi.useFakeTimers();
  vi.stubGlobal("window", new EventTarget());
  vi.stubGlobal("EventSource", Source);
  beginSession("test");
  Source.instances = [];
  streams = [];
  refresh.mockReset().mockResolvedValue({ csrf_token: "test" });
  fetchSession.mockReset().mockResolvedValue({ csrf_token: "test" });
});
afterEach(() => {
  streams.forEach((s) => s.stop());
  vi.useRealTimers();
  vi.unstubAllGlobals();
});

it("retries explicitly closed streams with backoff after transient validation failures", async () => {
  refresh
    .mockRejectedValueOnce(new Error("503"))
    .mockRejectedValueOnce(new Error("network"));
  const onState = vi.fn();
  connect({ onState });
  const first = Source.instances[0];
  first.emit("auth_expired");
  first.emit("auth_expired");
  await vi.advanceTimersByTimeAsync(0);
  expect(refresh).toHaveBeenCalledTimes(1);
  expect(first.readyState).toBe(Source.CLOSED);
  await vi.advanceTimersByTimeAsync(999);
  expect(refresh).toHaveBeenCalledTimes(1);
  await vi.advanceTimersByTimeAsync(1);
  expect(refresh).toHaveBeenCalledTimes(2);
  await vi.advanceTimersByTimeAsync(2000);
  expect(Source.instances).toHaveLength(2);
  Source.instances[1].open();
  expect(onState).toHaveBeenLastCalledWith("live");
});

it("caps recovery at 30 seconds and cancels timers on stop", async () => {
  refresh.mockRejectedValue(new Error("offline"));
  const stream = connect();
  Source.instances[0].emit("auth_expired");
  await vi.advanceTimersByTimeAsync(0);
  for (const delay of [1000, 2000, 4000, 8000, 16000, 30000, 30000]) {
    const before = refresh.mock.calls.length;
    await vi.advanceTimersByTimeAsync(delay);
    expect(refresh.mock.calls.length).toBe(before + 1);
  }
  stream.stop();
  expect(vi.getTimerCount()).toBe(0);
});

it("ignores events from a replaced connection and refreshes the job snapshot before reconnecting", async () => {
  const snapshot = vi.fn(async () => true);
  const log = vi.fn();
  connect({ beforeReconnect: snapshot, events: { log } });
  const old = Source.instances[0];
  old.emit("auth_expired");
  await vi.advanceTimersByTimeAsync(0);
  expect(snapshot).toHaveBeenCalledOnce();
  old.emit("log");
  Source.instances[1].emit("log");
  expect(log).toHaveBeenCalledOnce();
});

it("leaves native reconnects intact and throttles transport failure session checks", () => {
  connect();
  const source = Source.instances[0];
  source.onerror?.();
  source.onerror?.();
  expect(source.readyState).toBe(0);
  expect(fetchSession).toHaveBeenCalledOnce();
  expect(refresh).not.toHaveBeenCalled();
});

it("never reopens a finished task or a stopped/expired session", async () => {
  connect({ beforeReconnect: async () => false });
  Source.instances[0].emit("auth_expired");
  await vi.advanceTimersByTimeAsync(0);
  expect(Source.instances).toHaveLength(1);
  const stream = connect();
  let release!: (value: unknown) => void;
  refresh.mockImplementationOnce(
    () =>
      new Promise((resolve) => {
        release = resolve;
      }),
  );
  Source.instances[1].emit("auth_expired");
  stream.stop();
  release({ csrf_token: "test" });
  await vi.advanceTimersByTimeAsync(60000);
  expect(Source.instances).toHaveLength(2);
  connect();
  expireSession();
  expect(Source.instances[2].readyState).toBe(Source.CLOSED);
  expect(vi.getTimerCount()).toBe(0);
});

it("stops instead of retrying a confirmed expired session", async () => {
  refresh.mockResolvedValueOnce(null);
  connect();
  Source.instances[0].emit("auth_expired");
  await vi.advanceTimersByTimeAsync(60000);
  expect(refresh).toHaveBeenCalledOnce();
  expect(Source.instances).toHaveLength(1);
  expect(vi.getTimerCount()).toBe(0);
});
