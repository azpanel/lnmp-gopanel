export type Compression = {
  format: "zip" | "7z";
  level: number;
  has_password: boolean;
};
export type TableRule = {
  database: string;
  mode: "all" | "include" | "exclude";
  tables: string[];
};
export type DatabaseSelection = {
  enabled: boolean;
  mode: "all" | "selected";
  excluded: string[];
  selected: string[];
  rules: TableRule[];
};
export type FileSource = { path: string; excludes: string[] };
export type Policy = {
  id: string;
  revision: number;
  name: string;
  notes: string;
  enabled: boolean;
  schedule: string;
  compression: Compression;
  retention: { databases: number; files: number };
  databases: DatabaseSelection;
  files: FileSource[];
  created_at: string;
  updated_at: string;
};
export type PolicySummary = Pick<
  Policy,
  | "id"
  | "revision"
  | "name"
  | "enabled"
  | "schedule"
  | "compression"
  | "retention"
  | "updated_at"
> & {
  database_enabled: boolean;
  database_mode: "all" | "selected";
  database_count: number;
  file_count: number;
};
export type PolicySave = Policy & {
  password_action: "keep" | "replace" | "clear";
  password: string;
};
export type FileSize = {
  path: string;
  type: "file" | "directory";
  valid: boolean;
  original_bytes: number;
  included_bytes: number;
  complete: boolean;
  error?: string;
};
export type DatabaseItem = { name: string; size: number };
export type TableList = {
  items: { name: string; type: string; size: number | null }[];
  total: number;
  page: number;
  page_size: number;
};
export const defaultExcludedDatabases = [
  "mysql",
  "information_schema",
  "performance_schema",
  "phpmyadmin",
  "sys",
];
export function newPolicy(): PolicySave {
  return {
    id: "",
    revision: 0,
    name: "",
    notes: "",
    enabled: true,
    schedule: "0 2 * * *",
    compression: { format: "zip", level: 5, has_password: false },
    retention: { databases: 7, files: 7 },
    databases: {
      enabled: true,
      mode: "all",
      excluded: [...defaultExcludedDatabases],
      selected: [],
      rules: [],
    },
    files: [],
    created_at: "0001-01-01T00:00:00Z",
    updated_at: "0001-01-01T00:00:00Z",
    password_action: "keep",
    password: "",
  };
}
export function databaseSelected(db: DatabaseSelection, name: string): boolean {
  return db.mode === "all"
    ? !db.excluded.includes(name)
    : db.selected.includes(name);
}
export function toggleDatabase(
  db: DatabaseSelection,
  name: string,
  checked: boolean,
): DatabaseSelection {
  const field = db.mode === "all" ? "excluded" : "selected";
  const include = db.mode === "all" ? !checked : checked;
  return {
    ...db,
    [field]: include
      ? [...new Set([...db[field], name])]
      : db[field].filter((n) => n !== name),
  };
}
export function policyValidation(
  p: PolicySave,
):
  | { tab: "basic" | "databases" | "files"; message: string; field?: string }
  | undefined {
  if (!p.name.trim() || [...p.name].length > 80)
    return {
      tab: "basic",
      message: "请输入 1–80 字的政策名称",
      field: "policy-name",
    };
  if (p.schedule.trim().split(/\s+/).length !== 5)
    return {
      tab: "basic",
      message: "请输入五字段 Cron 表达式",
      field: "policy-cron",
    };
  if (
    ![p.retention.databases, p.retention.files].every(
      (n) => Number.isInteger(n) && n >= 1 && n <= 1000000,
    )
  )
    return {
      tab: "basic",
      message: "保留份数须为 1–1000000",
      field: "policy-retention-databases",
    };
  if (p.password_action === "replace" && !p.password)
    return {
      tab: "basic",
      message: "请输入压缩密码，或选择不加密",
      field: "policy-password",
    };
  if (
    p.databases.enabled &&
    p.databases.mode === "selected" &&
    !p.databases.selected.length
  )
    return { tab: "databases", message: "请至少选择一个数据库" };
  if (
    p.databases.enabled &&
    p.databases.rules.some(
      (r) =>
        databaseSelected(p.databases, r.database) &&
        r.mode === "include" &&
        !r.tables.length,
    )
  )
    return { tab: "databases", message: "仅备份所选表时至少选择一张表" };
  if (!p.databases.enabled && !p.files.length)
    return { tab: "files", message: "请至少配置数据库或文件来源" };
  return undefined;
}
export function readableBytes(bytes: number | null | undefined): string {
  if (bytes == null) return "—";
  const units = ["B", "KB", "MB", "GB", "TB"];
  let value = bytes,
    index = 0;
  while (value >= 1024 && index < units.length - 1) {
    value /= 1024;
    index++;
  }
  return `${value.toFixed(index ? 1 : 0)} ${units[index]}`;
}
