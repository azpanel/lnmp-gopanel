import type { Destination } from "@/lib/backup-policies";

export type BackupRun = {
  id: string;
  policy_id: string;
  policy_name: string;
  revision: number;
  trigger: "manual" | "schedule";
  status:
    | "queued"
    | "running"
    | "succeeded"
    | "partial"
    | "failed"
    | "skipped"
    | "cancelled";
  phase: string;
  created_at: string;
  updated_at: string;
  started_at?: string;
  finished_at?: string;
  scheduled_for?: string;
  error?: string;
  categories: {
    category: "databases" | "files";
    status: string;
    error?: string;
    name?: string;
    bytes: number;
    sha256?: string;
    deliveries: {
      destination: Destination;
      status: string;
      object?: string;
      error?: string;
      cleanup_error?: string;
    }[];
  }[];
};
export type BackupRunList = {
  items: BackupRun[];
  total: number;
  page: number;
  page_size: number;
};
export type BackupRuntime = {
  scheduler: boolean;
  scheduler_error?: string;
  dependencies: { name: string; ready: boolean; error?: string }[];
};
export const runStatusLabels: Record<string, string> = {
  queued: "排队中",
  running: "备份中",
  succeeded: "成功",
  partial: "部分成功",
  failed: "失败",
  skipped: "已跳过",
  cancelled: "已取消",
};
export const runPhaseLabels: Record<string, string> = {
  queued: "等待执行",
  preflight: "执行前检查",
  databases: "导出并打包数据库",
  files: "复制并打包文件",
  delivery: "交付备份及清理旧版本",
  finished: "已结束",
};
export function activeBackupRun(run?: BackupRun) {
  return !!run && ["queued", "running"].includes(run.status);
}
export function backupRunTime(value?: string) {
  return value ? new Date(value).toLocaleString() : "—";
}
