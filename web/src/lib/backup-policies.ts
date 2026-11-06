import type { BackupRun } from "@/lib/backup-runs";

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
export type Destination =
  | { type: "local"; path: string }
  | { type: "gdrive"; account: string; folder_id: string }
  | { type: "rclone"; remote: string; path?: string };
export type PathDiagnostic = {
  control?: string;
  field: "files" | "destinations";
  index: number;
  message: string;
};
export type PathsResult = {
  errors: PathDiagnostic[];
  warnings: PathDiagnostic[];
};
export type BackupNotification = {
  enabled: boolean;
  channel: "telegram";
  uid: string;
  has_token: boolean;
};
export const emptyNotification = (): BackupNotification => ({
  enabled: false,
  channel: "telegram",
  uid: "",
  has_token: false,
});
export type Policy = {
  notification?: BackupNotification;
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
  destinations: Destination[];
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
  | "destinations"
> & {
  last_run?: BackupRun;
  database_enabled: boolean;
  database_mode: "all" | "selected";
  database_count: number;
  file_count: number;
};
export type PolicySave = Policy & {
  notification_token_action?: "keep" | "replace" | "clear";
  notification_token?: string;
  password_action: "keep" | "replace" | "clear";
  password: string;
  copy_from?: { id: string; revision: number };
};
export type FileSize = {
  path: string;
  type: "file" | "directory";
  valid: boolean;
  original_bytes: number;
  included_bytes: number;
  complete: boolean;
  rules?: {
    pattern: string;
    count: number;
    samples: string[];
    truncated: boolean;
  }[];
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
    destinations: [],
    created_at: "0001-01-01T00:00:00Z",
    updated_at: "0001-01-01T00:00:00Z",
    notification: emptyNotification(),
    notification_token_action: "keep",
    notification_token: "",
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
export function policyValidation(p: PolicySave):
  | {
      tab: "basic" | "databases" | "files" | "storage" | "notification";
      message: string;
      field?: string;
    }
  | undefined {
  const notificationIssue = notificationValidation(p);
  if (notificationIssue) return notificationIssue;
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
  if (/[\0\r\n]/.test(p.password))
    return {
      tab: "basic",
      message: "压缩密码不能包含 NUL 或换行符",
      field: "policy-password",
    };
  if (
    p.password_action === "replace" &&
    p.compression.format === "zip" &&
    (p.password.length > 99 || /[^\x20-\x7e]/.test(p.password))
  )
    return {
      tab: "basic",
      message: "ZIP 加密密码须为最多 99 位 ASCII 字符，中文密码请改用 7z",
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
  const destinations = p.destinations ?? [];
  if (p.enabled && !destinations.length)
    return { tab: "storage", message: "启用政策至少需要一个存储位置" };
  for (const d of destinations) {
    if (d.type === "local" && (!d.path.startsWith("/") || !d.path.trim()))
      return { tab: "storage", message: "本地存储位置须填写绝对目录" };
    if (d.type === "gdrive" && (!d.account || !d.folder_id))
      return { tab: "storage", message: "请选择 gdrive 账户并填写文件夹 ID" };
    if (d.type === "rclone" && !d.remote)
      return { tab: "storage", message: "请选择 rclone 远程" };
  }
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

export function destinationLabel(d: Destination): string {
  if (d.type === "local") return `本地：${d.path}`;
  if (d.type === "gdrive") return `gdrive：${d.account} / ${d.folder_id}`;
  return `rclone：${d.remote}:${d.path || "/"}`;
}

export function notificationValidation(
  p: PolicySave,
  test = false,
): { tab: "notification"; message: string; field: string } | undefined {
  const n = p.notification ?? emptyNotification();
  const error = (message: string, field: string) => ({
    tab: "notification" as const,
    message,
    field,
  });
  if (
    (test || n.enabled || n.uid) &&
    (!/^[1-9][0-9]{0,18}$/.test(n.uid) || BigInt(n.uid) > 9223372036854775807n)
  )
    return error("通知用户 UID 须为正整数", "policy-notification-uid");
  if (p.notification_token_action === "replace") {
    const token = p.notification_token ?? "";
    if (token.length > 256 || !/^[0-9]+:[A-Za-z0-9_-]{20,200}$/.test(token))
      return error("通知机器人 Token 格式无效", "policy-notification-token");
  } else if (
    (test || n.enabled) &&
    (p.notification_token_action === "clear" || !n.has_token)
  ) {
    return error(
      "请填写通知机器人 Token",
      p.notification_token_action === "clear"
        ? "policy-notification-token-action"
        : "policy-notification-token",
    );
  }
}
