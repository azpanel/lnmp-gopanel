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
  steps?: BackupStep[];
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

export function backupRunDuration(run: BackupRun, now: number): string {
  if (!run.started_at) return "—";
  const end = run.finished_at
    ? Date.parse(run.finished_at)
    : activeBackupRun(run)
      ? now
      : Number.NaN;
  const elapsed = end - Date.parse(run.started_at);
  if (!Number.isFinite(elapsed)) return "—";
  const seconds = Math.max(0, Math.floor(elapsed / 1000));
  const hours = Math.floor(seconds / 3600);
  const minutes = Math.floor((seconds % 3600) / 60);
  return `${hours ? `${hours} 小时 ` : ""}${hours || minutes ? `${minutes} 分 ` : ""}${seconds % 60} 秒`;
}

export type BackupProgress = {
  total_bytes?: number;
  processed_bytes?: number;
  percent?: number;
  eta_seconds?: number;
  updated_at: string;
  estimated?: boolean;
};
export type BackupStep = {
  id: string;
  kind: string;
  category?: "databases" | "files";
  destination?: Destination;
  status: string;
  started_at?: string;
  finished_at?: string;
  error?: string;
  progress?: BackupProgress;
};

export function mergeBackupRuns(...pages: BackupRun[][]): BackupRun[] {
  const runs = new Map<string, BackupRun>();
  for (const page of pages)
    for (const run of page) if (!runs.has(run.id)) runs.set(run.id, run);
  return [...runs.values()]
    .sort(
      (a, b) =>
        b.created_at.localeCompare(a.created_at) || b.id.localeCompare(a.id),
    )
    .slice(0, 100);
}

export function backupTimeline(run: BackupRun): BackupStep[] {
  if (run.steps?.length) return run.steps;
  const steps: BackupStep[] = [];
  for (const category of run.categories) {
    steps.push({
      id: category.category,
      kind: "legacy",
      category: category.category,
      status: category.status,
      error: category.error,
    });
    category.deliveries.forEach((delivery, index) => {
      steps.push({
        id: `${category.category}-delivery-${index}`,
        kind: "deliver",
        category: category.category,
        destination: delivery.destination,
        status: delivery.status,
        error: delivery.error,
      });
      if (delivery.cleanup_error)
        steps.push({
          id: `${category.category}-cleanup-${index}`,
          kind: "cleanup",
          category: category.category,
          destination: delivery.destination,
          status: "failed",
          error: delivery.cleanup_error,
        });
    });
  }
  if (!steps.length && activeBackupRun(run))
    steps.push({ id: "current", kind: run.phase, status: "running" });
  if (!activeBackupRun(run))
    steps.push({
      id: "finished",
      kind: "finished",
      status: run.status,
      finished_at: run.finished_at,
      error: run.error,
    });
  return steps;
}

export function backupETA(
  progress: BackupProgress,
  running: boolean,
  now = Date.now(),
): string {
  if (!running) return "—";
  if (
    progress.eta_seconds == null ||
    !Number.isFinite(progress.eta_seconds) ||
    progress.eta_seconds < 0 ||
    now - Date.parse(progress.updated_at) > 15000
  )
    return "计算中";
  const seconds = Math.ceil(progress.eta_seconds);
  if (seconds < 60) return `${seconds} 秒`;
  if (seconds < 3600)
    return `${Math.floor(seconds / 60)} 分 ${seconds % 60} 秒`;
  return `${Math.floor(seconds / 3600)} 小时 ${Math.floor((seconds % 3600) / 60)} 分`;
}
