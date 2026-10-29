import { describe, expect, it } from "vitest";
import {
  binaryAllowed,
  defaultModules,
  initialInstallRequest,
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
