import { describe, expect, it } from "vitest";
import { sortMounts } from "./storage-sort";
import type { StorageMount } from "./types";

function mount(
  path: string,
  total: number | null,
  used: number | null,
  error?: string,
): StorageMount {
  return {
    path,
    device: path,
    fs_type: "ext4",
    options: [],
    total,
    used,
    free: total == null || used == null ? null : total - used,
    percent: total && used != null ? (used / total) * 100 : null,
    error,
  };
}
describe("mount sorting", () => {
  it("sorts raw byte values, retains zero usage and puts unavailable values last in both directions", () => {
    const mounts = [
      mount("/unreadable", 10000, 10000, "读取失败"),
      mount("/small", 4096, 900),
      mount("/virtual", 0, 0),
      mount("/empty", 4096, 0),
      mount("/large", 8192, 2048),
      mount("/missing", null, null),
    ];
    expect(
      sortMounts(mounts, { key: "used", direction: "ascending" }).map(
        (m) => m.path,
      ),
    ).toEqual([
      "/empty",
      "/small",
      "/large",
      "/missing",
      "/unreadable",
      "/virtual",
    ]);
    expect(
      sortMounts(mounts, { key: "used", direction: "descending" }).map(
        (m) => m.path,
      ),
    ).toEqual([
      "/large",
      "/small",
      "/empty",
      "/missing",
      "/unreadable",
      "/virtual",
    ]);
    expect(
      sortMounts(mounts, { key: "total", direction: "descending" })[0].path,
    ).toBe("/large");
    expect(
      sortMounts(mounts, { key: "free", direction: "ascending" })[0].path,
    ).toBe("/small");
    expect(
      sortMounts(mounts, { key: "percent", direction: "descending" })[0].path,
    ).toBe("/large");
  });
  it("uses natural text order and never mutates the cached mount list", () => {
    const mounts = Object.freeze([
      mount("/disk10", 1, 0),
      mount("/disk2", 1, 0),
    ]);
    expect(
      sortMounts(mounts, { key: "path", direction: "ascending" }).map(
        (m) => m.path,
      ),
    ).toEqual(["/disk2", "/disk10"]);
    expect(
      sortMounts(mounts, { key: "device", direction: "descending" }).map(
        (m) => m.path,
      ),
    ).toEqual(["/disk10", "/disk2"]);
    expect(sortMounts(mounts, null).map((m) => m.path)).toEqual([
      "/disk10",
      "/disk2",
    ]);
  });
});
