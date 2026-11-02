import { describe, it, expect } from "vitest";
import {
  destinationDiagnostics,
  policyMessage,
} from "./backup-policy-feedback";
import type { Destination, PathsResult } from "./backup-policies";

describe("backup policy feedback", () => {
  it("keeps a fresh empty row quiet until blur or submit, including old helper responses", () => {
    const targets: Destination[] = [{ type: "local", path: "" }];
    const legacy: PathsResult = {
      errors: [
        {
          field: "destinations",
          index: 0,
          message: "组件安装参数无效：本地存储位置须为规范的绝对目录",
        },
      ],
      warnings: [],
    };
    expect(destinationDiagnostics(targets, legacy, {}, false)).toEqual([]);
    expect(
      destinationDiagnostics(targets, legacy, { "0:path": true }, false),
    ).toEqual([
      {
        field: "destinations",
        index: 0,
        control: "path",
        message: "请填写本地目录",
      },
    ]);
    expect(destinationDiagnostics(targets, legacy, {}, true)).toHaveLength(1);
    const structured: PathsResult = {
      errors: [{ ...legacy.errors[0], control: "path" }],
      warnings: [],
    };
    expect(destinationDiagnostics(targets, structured, {}, false)).toEqual([]);
  });
  it("places format errors on a field and relationship errors on the destination card", () => {
    const format: PathsResult = {
      errors: [
        {
          field: "destinations",
          index: 1,
          control: "path",
          message: "组件安装参数无效：路径无效",
        },
      ],
      warnings: [],
    };
    const targets: Destination[] = [
      { type: "local", path: "/var/backups" },
      { type: "local", path: "relative" },
    ];
    expect(destinationDiagnostics(targets, format, {}, false)).toEqual([
      {
        field: "destinations",
        index: 1,
        control: "path",
        message: "请输入规范的绝对路径，例如 /var/backups/websites",
      },
    ]);
    const relation: PathsResult = {
      errors: [
        {
          field: "destinations",
          index: 0,
          message: "组件安装参数无效：存储位置重复",
        },
      ],
      warnings: [],
    };
    expect(destinationDiagnostics(targets, relation, {}, false)).toEqual([
      { field: "destinations", index: 0, message: "存储位置重复" },
    ]);
  });
  it("reports missing cloud fields separately and accepts the rclone root", () => {
    expect(
      destinationDiagnostics(
        [{ type: "gdrive", account: "", folder_id: "" }],
        undefined,
        {},
        true,
      ).map((i) => i.control),
    ).toEqual(["account", "folder_id"]);
    expect(
      destinationDiagnostics(
        [{ type: "rclone", remote: "archive" }],
        undefined,
        {},
        true,
      ),
    ).toEqual([]);
    expect(
      policyMessage("组件安装参数无效：组件安装参数无效：存储位置重复"),
    ).toBe("存储位置重复");
  });
});
