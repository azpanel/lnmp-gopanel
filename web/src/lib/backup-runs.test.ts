import { describe, expect, it } from "vitest";
import {
  backupETA,
  backupTimeline,
  mergeBackupRuns,
  type BackupRun,
} from "./backup-runs";

const run = (id: string, created = "2026-11-06T12:00:00Z"): BackupRun => ({
  id,
  policy_id: "policy",
  policy_name: "备份",
  revision: 1,
  trigger: "manual",
  status: "succeeded",
  phase: "finished",
  created_at: created,
  updated_at: created,
  categories: [],
});

describe("backup history", () => {
  it("deduplicates overlapping pages, keeps fresh summaries and caps at 100", () => {
    const first = { ...run("x"), status: "running" as const };
    const older = Array.from({ length: 110 }, (_, n) =>
      run(`old-${n}`, "2026-11-05T12:00:00Z"),
    );
    const merged = mergeBackupRuns([first], [run("x"), ...older]);
    expect(merged).toHaveLength(100);
    expect(merged[0]).toEqual(first);
    expect(new Set(merged.map((r) => r.id)).size).toBe(100);
  });
  it("uses explicit steps and preserves legacy errors without inventing progress", () => {
    const current = {
      ...run("x"),
      steps: [{ id: "compress", kind: "compress", status: "running" }],
    };
    expect(backupTimeline(current)).toBe(current.steps);
    const legacy: BackupRun = {
      ...run("old"),
      status: "partial",
      categories: [
        {
          category: "files",
          status: "partial",
          bytes: 123,
          deliveries: [
            {
              destination: { type: "local", path: "/backup" },
              status: "succeeded",
              cleanup_error: "cleanup failed",
            },
          ],
        },
      ],
    };
    const steps = backupTimeline(legacy);
    expect(steps.map((s) => s.kind)).toEqual([
      "legacy",
      "deliver",
      "cleanup",
      "finished",
    ]);
    expect(steps[2].error).toBe("cleanup failed");
    expect(steps.every((s) => !s.progress)).toBe(true);
  });
  it("does not fabricate ETA for unknown, stale or completed progress", () => {
    const now = Date.parse("2026-11-06T12:00:00Z");
    const progress = {
      updated_at: new Date(now).toISOString(),
      eta_seconds: 75,
    };
    expect(backupETA(progress, true, now)).toBe("1 分 15 秒");
    expect(backupETA(progress, true, now + 16000)).toBe("计算中");
    expect(backupETA(progress, false, now)).toBe("—");
    expect(backupETA({ ...progress, eta_seconds: undefined }, true, now)).toBe(
      "计算中",
    );
  });
});
