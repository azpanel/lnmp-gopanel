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
    };
    if (i > 0 && time - Date.parse(samples[i - 1].timestamp) > 6500)
      return [
        {
          time: time - 1,
          cpu: null,
          memory: null,
          download: null,
          upload: null,
        },
        point,
      ];
    return [point];
  });
}
