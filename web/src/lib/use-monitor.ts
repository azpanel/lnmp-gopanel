import { useEffect, useState } from "react";
import { fetchSession, refreshStreamSession } from "./api";
import { appendSample } from "./history";
import type { Snapshot } from "./types";

export function useMonitor(sessionToken?: string) {
  const [reconnect, setReconnect] = useState(0);
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
    const source = new EventSource("/api/v1/metrics/stream");
    let alive = true;
    setConnection("connecting");
    source.addEventListener("metrics", (event) => {
      if (!alive) return;
      try {
        const sample = JSON.parse((event as MessageEvent).data) as Snapshot;
        setSamples((prev) => appendSample(prev, sample));
        setLastReceived(Date.now());
        setConnection("live");
      } catch {
        setConnection("offline");
      }
    });
    source.addEventListener("auth_expired", () => {
      source.close();
      // An old stream can expire after this browser has rotated its session.
      void refreshStreamSession()
        .then((session) => {
          if (alive && session) setReconnect((value) => value + 1);
        })
        .catch(() => {});
    });
    source.onerror = () => {
      if (!alive) return;
      setConnection("offline");
      void fetchSession().catch(() => {});
    };
    return () => {
      alive = false;
      source.close();
    };
  }, [paused, sessionToken, reconnect]);
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
