import { describe, it, expect } from "vitest";
import {
  newPolicy,
  databaseSelected,
  toggleDatabase,
  policyValidation,
  readableBytes,
} from "./backup-policies";
describe("backup policy configuration", () => {
  it("automatically includes new databases and preserves explicit exclusions", () => {
    const p = newPolicy();
    expect(databaseSelected(p.databases, "future_app")).toBe(true);
    for (const name of [
      "mysql",
      "information_schema",
      "performance_schema",
      "phpmyadmin",
      "sys",
    ])
      expect(databaseSelected(p.databases, name)).toBe(false);
    p.databases = toggleDatabase(p.databases, "future_app", false);
    expect(databaseSelected(p.databases, "future_app")).toBe(false);
    p.databases = toggleDatabase(p.databases, "sys", true);
    expect(databaseSelected(p.databases, "sys")).toBe(true);
    p.databases.mode = "selected";
    p.databases.selected = ["app"];
    expect(databaseSelected(p.databases, "new_app")).toBe(false);
  });
  it("locates invalid fields and supports database-only and file-only policies", () => {
    const p = newPolicy();
    expect(policyValidation(p)?.field).toBe("policy-name");
    p.name = "每日";
    expect(policyValidation(p)).toBeUndefined();
    p.databases.mode = "selected";
    expect(policyValidation(p)?.tab).toBe("databases");
    p.databases.enabled = false;
    expect(policyValidation(p)?.tab).toBe("files");
    p.files = [{ path: "/srv/site", excludes: ["cache/"] }];
    expect(policyValidation(p)).toBeUndefined();
    p.password_action = "replace";
    expect(policyValidation(p)?.field).toBe("policy-password");
    p.password = "secret";
    expect(policyValidation(p)).toBeUndefined();
    p.retention.files = 0;
    expect(policyValidation(p)?.tab).toBe("basic");
  });
  it("does not mistake unknown sizes for empty files", () => {
    expect(readableBytes(null)).toBe("—");
    expect(readableBytes(0)).toBe("0 B");
    expect(readableBytes(2048)).toBe("2.0 KB");
  });
});
