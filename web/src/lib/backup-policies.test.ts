import { describe, it, expect } from "vitest";
import {
  newPolicy,
  notificationValidation,
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
    p.destinations = [{ type: "local", path: "/var/backups/test" }];
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
  it("validates archive password compatibility before saving", () => {
    const p = newPolicy();
    p.name = "每日";
    p.destinations = [{ type: "local", path: "/backups" }];
    p.password_action = "replace";
    for (const password of [
      "中文密码",
      "x".repeat(100),
      "line\nnext",
      "null\0byte",
    ]) {
      p.password = password;
      expect(policyValidation(p)?.field).toBe("policy-password");
    }
    p.password = "ASCII-Password-$(literal)";
    expect(policyValidation(p)).toBeUndefined();
    p.compression.format = "7z";
    p.password = "中文密码";
    expect(policyValidation(p)).toBeUndefined();
  });
  it("requires destinations only for enabled policies and validates filled rows", () => {
    const p = newPolicy();
    p.name = "停用草稿";
    expect(policyValidation(p)?.tab).toBe("storage");
    p.enabled = false;
    expect(policyValidation(p)).toBeUndefined();
    p.destinations = [
      { type: "gdrive", account: "a@example.com", folder_id: "" },
    ];
    expect(policyValidation(p)?.tab).toBe("storage");
    p.destinations[0] = {
      type: "gdrive",
      account: "a@example.com",
      folder_id: "root",
    };
    expect(policyValidation(p)).toBeUndefined();
  });
});

describe("backup notifications", () => {
  it("defaults off and validates draft tests independently of the switch", () => {
    const p = newPolicy();
    expect(p.notification?.enabled).toBe(false);
    expect(notificationValidation(p)).toBeUndefined();
    expect(notificationValidation(p, true)?.field).toBe(
      "policy-notification-uid",
    );
    p.notification!.uid = "123456789";
    expect(notificationValidation(p, true)?.field).toBe(
      "policy-notification-token",
    );
    p.notification_token_action = "replace";
    p.notification_token = "123:abcdefghijklmnopqrstuv_0123456789";
    expect(notificationValidation(p, true)).toBeUndefined();
    p.notification!.enabled = true;
    for (const uid of ["", "0", "-123", "@user", "9223372036854775808"]) {
      p.notification!.uid = uid;
      expect(notificationValidation(p)?.field).toBe("policy-notification-uid");
    }
    p.notification!.uid = "123";
    p.notification_token_action = "keep";
    p.notification_token = "";
    p.notification!.has_token = true;
    expect(notificationValidation(p)).toBeUndefined();
    p.notification_token_action = "clear";
    expect(notificationValidation(p)?.field).toBe(
      "policy-notification-token-action",
    );
    p.notification!.enabled = false;
    expect(notificationValidation(p)).toBeUndefined();
    expect(notificationValidation(p, true)?.field).toBe(
      "policy-notification-token-action",
    );
  });
});
