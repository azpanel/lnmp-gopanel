import { fetchSession, refreshStreamSession } from "./api";
import { captureSession, onSessionChange } from "./session-scope";

type Options = {
  url: () => string;
  events: Record<string, (event: MessageEvent) => void>;
  onState: (state: "connecting" | "live" | "offline") => void;
  beforeReconnect?: () => Promise<boolean | void>;
};

// Leave transient transport errors to EventSource (including Last-Event-ID).
// Explicitly closed streams need a separate, cancellable recovery lifecycle.
export function sessionStream(options: Options) {
  const scope = captureSession();
  const controller = new AbortController();
  let source: EventSource | null = null;
  let stopped = false;
  let timer: ReturnType<typeof setTimeout> | undefined;
  let recovering = false;
  let failures = 0;
  let lastCheck = -Infinity;
  const active = () => !stopped && scope.current();
  const closeSource = () => {
    source?.close();
    source = null;
  };
  const stop = () => {
    if (stopped) return;
    stopped = true;
    clearTimeout(timer);
    controller.abort();
    closeSource();
    unsubscribe();
  };
  const unsubscribe = onSessionChange(stop);
  const schedule = () => {
    if (!active() || timer !== undefined) return;
    timer = setTimeout(
      () => {
        timer = undefined;
        void recover();
      },
      Math.min(30000, 1000 * 2 ** Math.min(failures++, 5)),
    );
  };
  const recover = async () => {
    if (!active() || recovering) return;
    recovering = true;
    try {
      const session = await refreshStreamSession(controller.signal);
      if (!active()) return;
      if (!session) {
        stop();
        return;
      }
      const reconnect = await options.beforeReconnect?.();
      if (!active()) return;
      if (reconnect === false) {
        stop();
        return;
      }
      connect();
    } catch {
      if (active()) schedule();
    } finally {
      recovering = false;
    }
  };
  const connect = () => {
    if (!active()) return;
    options.onState("connecting");
    const current = new EventSource(options.url());
    source = current;
    const valid = () => active() && source === current;
    current.onopen = () => {
      if (!valid()) return;
      failures = 0;
      options.onState("live");
    };
    for (const [name, callback] of Object.entries(options.events)) {
      current.addEventListener(name, (event) => {
        if (valid()) callback(event as MessageEvent);
      });
    }
    current.addEventListener("auth_expired", () => {
      if (!valid()) return;
      closeSource();
      options.onState("offline");
      void recover();
    });
    current.onerror = () => {
      if (!valid()) return;
      options.onState("offline");
      if (current.readyState === EventSource.CLOSED) {
        closeSource();
        schedule();
      } else if (Date.now() - lastCheck >= 30000) {
        lastCheck = Date.now();
        void fetchSession(true, controller.signal).catch(() => {});
      }
    };
  };
  connect();
  return { stop };
}
