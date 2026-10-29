import { describe, expect, it } from "vitest";
import {
  binaryAllowed,
  defaultModules,
  initialInstallRequest,
  installElapsedSeconds,
  type InstallJob,
} from "./lnmp-install";

describe("installation form defaults", () => {
  it("selects all modules and uses the prepared revision and versions", () => {
    const job: InstallJob = {
      id: "job",
      status: "ready",
      phase: "ready",
      created_at: "",
      updated_at: "",
      options: {
        revision: "prepared-sha",
        databases: [],
        php: [],
        default_database: 11,
        default_php: 14,
        default_allocator: 1,
        architecture: "amd64",
        ipv4: "192.0.2.1",
        modules: [],
      },
    };
    const form = initialInstallRequest(job);
    expect(form.nginx_modules).toEqual([
      "vts",
      "geoip2",
      "fancyindex",
      "substitutions_filter",
    ]);
    expect(form.nginx_modules).not.toBe(defaultModules);
    expect(form.revision).toBe("prepared-sha");
    expect(form.database).toBe(11);
    expect(form.php).toBe(14);
    expect(form.binary).toBe(true);
    expect(form.database_password).toBe("");
    expect(form.auth_password).toBe("");
  });
  it("restricts binary packages by database and architecture", () => {
    expect(binaryAllowed(1, "amd64")).toBe(false);
    expect(binaryAllowed(11, "arm64")).toBe(true);
    expect(binaryAllowed(4, "arm64")).toBe(false);
    expect(binaryAllowed(9, "386")).toBe(false);
  });
});

describe("installation elapsed time", () => {
  const start = Date.parse("2026-10-29T10:00:00Z");
  const job: InstallJob = {
    id: "job",
    status: "running",
    phase: "Nginx 编译",
    created_at: "2026-10-29T08:00:00Z",
    started_at: "2026-10-29T10:00:00Z",
    updated_at: "2026-10-29T10:00:10Z",
  };

  it("starts at zero after preparation and configuration, and keeps the same anchor across updates and reloads", () => {
    expect(installElapsedSeconds(job, start)).toBe(0);
    expect(installElapsedSeconds(job, start + 65_999)).toBe(65);
    const updated = {
      ...job,
      phase: "PHP 编译",
      updated_at: "2026-10-29T10:01:30Z",
    };
    expect(installElapsedSeconds(updated, start + 100_000)).toBe(100);
    expect(
      installElapsedSeconds(
        JSON.parse(JSON.stringify(updated)),
        start + 120_000,
      ),
    ).toBe(120);
  });

  it.each(["preparing", "ready"] as const)(
    "does not count time during %s",
    (status) => {
      expect(
        installElapsedSeconds(
          { ...job, status, started_at: undefined },
          start + 3_600_000,
        ),
      ).toBeNull();
    },
  );

  it.each(["succeeded", "failed", "interrupted"] as const)(
    "freezes at the final update for %s",
    (status) => {
      expect(installElapsedSeconds({ ...job, status }, start + 60_000)).toBe(
        10,
      );
      expect(installElapsedSeconds({ ...job, status }, start + 3_600_000)).toBe(
        10,
      );
    },
  );

  it("handles legacy jobs and invalid timestamps without using creation time", () => {
    expect(installElapsedSeconds(undefined, start)).toBeNull();
    for (const started_at of [
      undefined,
      "",
      "invalid",
      "0001-01-01T00:00:00Z",
    ]) {
      expect(installElapsedSeconds({ ...job, started_at }, start)).toBeNull();
    }
    expect(
      installElapsedSeconds(
        { ...job, status: "succeeded", updated_at: "invalid" },
        start,
      ),
    ).toBeNull();
    expect(installElapsedSeconds(job, Number.NaN)).toBeNull();
    expect(installElapsedSeconds(job, start - 10_000)).toBe(0);
    expect(
      installElapsedSeconds(
        { ...job, status: "failed", updated_at: "2026-10-29T09:59:59Z" },
        start,
      ),
    ).toBe(0);
  });
});
