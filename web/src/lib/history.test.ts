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

describe("optional dashboard trends", () => {
  it("maps all optional trend series and preserves real zeroes", () => {
    const value = sample(2000);
    value.disk_io = { read: 0, write: 2048 };
    value.load = { one: 0, five: 2, fifteen: 3 };
    value.swap = { used: 0, total: 1024, percent: 0 };
    value.nginx = { active: 0 };
    value.php_fpm = { active: 0, idle: 4, queue: 0 };
    value.database = { connections: 0, slow_queries: 5 };
    expect(chartPoints([value])[0]).toMatchObject({
      diskRead: 0,
      diskWrite: 2048,
      loadOne: 0,
      loadFive: 2,
      loadFifteen: 3,
      swapUsed: 0,
      nginxActive: 0,
      phpActive: 0,
      phpIdle: 4,
      phpQueue: 0,
      dbConnections: 0,
    });
  });
  it("leaves gaps for every series after disconnection or probe failure", () => {
    const value = { ...sample(1000), nginx: { active: 7 } };
    const points = chartPoints([value, sample(10000)]);
    expect(points).toHaveLength(3);
    for (const [key, field] of Object.entries(points[1])) {
      if (key !== "time") expect(field).toBeNull();
    }
    expect(points[2].nginxActive).toBeNull();
    expect(points[2].dbConnections).toBeNull();
    expect(points[2].diskRead).toBeNull();
  });
});
