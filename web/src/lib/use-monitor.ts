import { useEffect, useState } from "react";
import { sessionStream } from "./session-stream";
import { appendSample } from "./history";
import type { Snapshot } from "./types";

export function useMonitor(sessionToken?: string) {
  const [samples, setSamples] = useState<Snapshot[]>([]);
  const [connection, setConnection] = useState<
    "connecting" | "live" | "offline"
  >("connecting");
  const [paused, setPaused] = useState(false);
  const [now, setNow] = useState(Date.now());
  const [lastReceived, setLastReceived] = useState<number | null>(null);
  useEffect(() => {
    const id = setInterval(() => setNow(Date.now()), 1000);
    return () => clearInterval(id);
  }, []);
  useEffect(() => {
    if (paused) return;
    setConnection("connecting");
    const stream = sessionStream({
      url: () => "/api/v1/metrics/stream",
      onState: (state) =>
        setConnection(state === "live" ? "connecting" : state),
      events: {
        metrics: (event) => {
          try {
            const sample = JSON.parse((event as MessageEvent).data) as Snapshot;
            setSamples((prev) => appendSample(prev, sample));
            setLastReceived(Date.now());
            setConnection("live");
          } catch {
            setConnection("offline");
          }
        },
      },
    });
    return stream.stop;
  }, [paused, sessionToken]);
  const latest = samples.at(-1);
  // Connection liveness uses local receipt time, independent of server clock skew.
  const stale = lastReceived === null || now - lastReceived > 7000;
  return {
    samples,
    latest,
    connection: paused
      ? ("paused" as const)
      : stale && connection === "live"
        ? ("offline" as const)
        : connection,
    stale,
    paused,
    setPaused,
  };
}
export type Monitor = ReturnType<typeof useMonitor>;
