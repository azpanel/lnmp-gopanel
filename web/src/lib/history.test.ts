import { describe, expect, it } from "vitest";
import { appendSample, chartPoints, MAX_POINTS } from "./history";
import type { Snapshot } from "./types";
function sample(time: number): Snapshot {
  return {
    timestamp: new Date(time).toISOString(),
    cpu: 15,
    memory: null,
    swap: null,
    load: null,
    uptime: 1,
    network: null,
    disk_io: null,
    disks: [],
    errors: {},
  };
}
describe("temporary monitoring window", () => {
  it("never keeps more than 150 points", () => {
    let points: Snapshot[] = [];
    for (let i = 0; i < 500; i++)
      points = appendSample(points, sample(i * 1000));
    expect(points).toHaveLength(MAX_POINTS);
  });
  it("discards points older than five minutes", () => {
    const points = appendSample([sample(0), sample(1000)], sample(301000));
    expect(points).toHaveLength(1);
  });
  it("ignores duplicate, out-of-order and invalid timestamps", () => {
    const points = [sample(2000)];
    expect(appendSample(points, sample(2000))).toBe(points);
    expect(appendSample(points, sample(1000))).toBe(points);
    expect(
      appendSample(points, { ...sample(3000), timestamp: "invalid" }),
    ).toBe(points);
  });
  it("leaves a gap after disconnection instead of inventing samples", () => {
    const points = chartPoints([sample(0), sample(2000), sample(10000)]);
    expect(points).toHaveLength(4);
    expect(points[2].cpu).toBeNull();
    expect(points[3].cpu).toBe(15);
  });
  it("preserves unavailable fields as null", () => {
    expect(chartPoints([sample(0)])[0].download).toBeNull();
  });
});
