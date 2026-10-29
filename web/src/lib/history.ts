import type { Snapshot } from "./types";
export const MAX_POINTS = 150;
export const WINDOW_MS = 5 * 60 * 1000;
export function appendSample(
  previous: Snapshot[],
  sample: Snapshot,
): Snapshot[] {
  const time = Date.parse(sample.timestamp);
  if (
    !Number.isFinite(time) ||
    (previous.length > 0 &&
      time <= Date.parse(previous[previous.length - 1].timestamp))
  )
    return previous;
  return [
    ...previous.filter((p) => Date.parse(p.timestamp) > time - WINDOW_MS),
    sample,
  ].slice(-MAX_POINTS);
}
export function chartPoints(samples: Snapshot[]) {
  return samples.flatMap((s, i) => {
    const time = Date.parse(s.timestamp);
    const point = {
      time,
      cpu: s.cpu,
      memory: s.memory?.percent ?? null,
      download: s.network?.read ?? null,
      upload: s.network?.write ?? null,
      diskRead: s.disk_io?.read ?? null,
      diskWrite: s.disk_io?.write ?? null,
      loadOne: s.load?.one ?? null,
      loadFive: s.load?.five ?? null,
      loadFifteen: s.load?.fifteen ?? null,
      swapUsed: s.swap?.used ?? null,
      nginxActive: s.nginx?.active ?? null,
      phpActive: s.php_fpm?.active ?? null,
      phpIdle: s.php_fpm?.idle ?? null,
      phpQueue: s.php_fpm?.queue ?? null,
      dbConnections: s.database?.connections ?? null,
    };
    if (i > 0 && time - Date.parse(samples[i - 1].timestamp) > 6500) {
      const gap = { ...point, time: time - 1 };
      for (const key of Object.keys(point) as (keyof typeof point)[]) {
        if (key !== "time") gap[key] = null;
      }
      return [gap, point];
    }
    return [point];
  });
}
