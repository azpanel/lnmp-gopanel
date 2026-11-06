import { useEffect, useId, useState } from "react";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import {
  ArchiveRestore,
  Clock3,
  ChevronDown,
  CloudDownload,
  Database,
  FolderSync,
  Loader2,
  RefreshCw,
  ServerCog,
} from "lucide-react";
import { Button } from "@/components/ui/button";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
  DialogTrigger,
} from "@/components/ui/dialog";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select";
import { Progress } from "@/components/ui/progress";
import { api } from "@/lib/api";
import {
  destinationLabel,
  readableBytes,
  type PolicySummary,
} from "@/lib/backup-policies";
import {
  activeBackupRun,
  backupTimeline,
  backupETA,
  mergeBackupRuns,
  type BackupStep,
  backupRunTime,
  runPhaseLabels,
  runStatusLabels,
  type BackupRun,
  type BackupRunList,
  type BackupRuntime,
} from "@/lib/backup-runs";

export function BackupRuntimeStatus() {
  const [open, setOpen] = useState(false);
  const [helpOpen, setHelpOpen] = useState(false);
  const helpId = useId();
  const changeOpen = (value: boolean) => {
    setOpen(value);
    setHelpOpen(false);
  };
  const query = useQuery({
    queryKey: ["backup-runtime"],
    queryFn: ({ signal }) => api<BackupRuntime>("/backups/runtime", { signal }),
    enabled: open,
    refetchOnWindowFocus: false,
    retry: 1,
  });
  const tools = {
    "7zip": {
      name: "7-Zip",
      description: "压缩备份文件 · 7zz / 7z",
      icon: ArchiveRestore,
    },
    dump: {
      name: "数据库导出工具",
      description: "数据库备份需要 · mysqldump / mariadb-dump",
      icon: Database,
    },
    gdrive: {
      name: "gdrive",
      description: "备份至 Google Drive 时需要",
      icon: CloudDownload,
    },
    rclone: {
      name: "rclone",
      description: "备份至 rclone 远程存储时需要",
      icon: FolderSync,
    },
  };
  return (
    <Dialog open={open} onOpenChange={changeOpen}>
      <DialogTrigger asChild>
        <Button size="sm" variant="outline">
          <ServerCog size={14} aria-hidden="true" />
          备份运行环境
        </Button>
      </DialogTrigger>
      <DialogContent className="backup-runtime-dialog">
        <DialogHeader>
          <DialogTitle>备份运行环境</DialogTitle>
          <DialogDescription>
            检查定时调度和依赖工具，了解备份所需的运行条件。
          </DialogDescription>
        </DialogHeader>
        <div className="backup-runtime-scroll">
          {query.isPending ? (
            <div className="backup-runtime-loading" role="status">
              <Loader2 size={24} className="animate-spin" aria-hidden="true" />
              <span>正在检查调度与工具…</span>
            </div>
          ) : query.error ? (
            <p className="notice" role="alert">
              运行环境检查失败：{query.error.message}
            </p>
          ) : (
            <>
              <section className="backup-runtime-section" aria-label="定时调度">
                <h3>定时调度</h3>
                <div className="backup-runtime-item">
                  <Clock3 size={20} aria-hidden="true" />
                  <div className="backup-runtime-item-content">
                    <strong>Cron 调度服务</strong>
                    <p>按备份政策设定的时间自动执行</p>
                    {!query.data.scheduler && (
                      <p className="backup-runtime-error">
                        {query.data.scheduler_error ||
                          "请检查受管 Cron 文件和 cron/crond 服务"}
                      </p>
                    )}
                  </div>
                  <span
                    className="backup-runtime-badge"
                    data-ready={query.data.scheduler}
                  >
                    {query.data.scheduler ? "已就绪" : "未就绪"}
                  </span>
                </div>
              </section>
              <section className="backup-runtime-section" aria-label="依赖工具">
                <h3>依赖工具</h3>
                <div className="backup-runtime-tools">
                  {query.data.dependencies.map((dependency) => {
                    const tool = tools[dependency.name as keyof typeof tools];
                    const Icon = tool?.icon ?? ServerCog;
                    return (
                      <div
                        className="backup-runtime-item"
                        key={dependency.name}
                      >
                        <Icon size={20} aria-hidden="true" />
                        <div className="backup-runtime-item-content">
                          <strong>{tool?.name ?? dependency.name}</strong>
                          {tool && <p>{tool.description}</p>}
                          {!dependency.ready && (
                            <p className="backup-runtime-error">
                              {dependency.error ||
                                "未安装或程序权限不安全，请由管理员检查"}
                            </p>
                          )}
                        </div>
                        <span
                          className="backup-runtime-badge"
                          data-ready={dependency.ready}
                        >
                          {dependency.ready ? "可用" : "需检查"}
                        </span>
                      </div>
                    );
                  })}
                </div>
              </section>
            </>
          )}
          <section className="backup-runtime-help">
            <button
              type="button"
              className="backup-runtime-help-toggle"
              aria-expanded={helpOpen}
              aria-controls={helpId}
              onClick={() => setHelpOpen((value) => !value)}
            >
              使用说明
              <ChevronDown size={16} aria-hidden="true" />
            </button>
            <div
              id={helpId}
              className="backup-runtime-help-content"
              data-open={helpOpen}
              aria-hidden={!helpOpen}
              inert={!helpOpen}
            >
              <div className="backup-runtime-help-inner">
                <ul>
                  <li>缺少或权限不安全的工具，请由管理员安装或修复。</li>
                  <li>gdrive 和 rclone 仅在使用对应云存储时需要。</li>
                  <li>定时认证使用服务器 root 客户端配置或 Socket。</li>
                  <li>表单临时查询密码仅用于查询，不会用于自动备份。</li>
                </ul>
              </div>
            </div>
          </section>
        </div>
        <DialogFooter>
          <Button
            variant="outline"
            disabled={query.isFetching}
            onClick={() => void query.refetch()}
          >
            <RefreshCw
              size={14}
              className={query.isFetching ? "animate-spin" : ""}
              aria-hidden="true"
            />
            重新检测
          </Button>
          <Button variant="outline" onClick={() => changeOpen(false)}>
            关闭
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}

const stepStatusLabels: Record<string, string> = {
  ...runStatusLabels,
  pending: "等待执行",
  running: "执行中",
};
function stepTitle(step: BackupStep) {
  const category = step.category === "databases" ? "数据库" : "文件";
  const destination = step.destination
    ? destinationLabel(step.destination)
    : "";
  const titles: Record<string, string> = {
    queued: "等待执行",
    preflight: "执行前检查",
    prepare: step.category === "databases" ? "导出数据库" : "准备备份文件",
    compress: `压缩${category}`,
    verify: `校验${category}备份`,
    deliver: `${step.destination?.type === "local" ? "写入本地" : "上传备份"} · ${destination}`,
    cleanup: `清理旧版本 · ${destination}`,
    finished: "执行结果",
    legacy: `${category}备份`,
  };
  return titles[step.kind] ?? runPhaseLabels[step.kind] ?? step.kind;
}

function StepProgress({ step, now }: { step: BackupStep; now: number }) {
  const progress = step.progress;
  if (!progress && !["compress", "deliver"].includes(step.kind)) return null;
  if (!progress && step.status !== "running") return null;
  const percent = progress?.percent;
  const value =
    percent != null && Number.isFinite(percent)
      ? Math.min(100, Math.max(0, percent))
      : undefined;
  const bytes = (value?: number) =>
    value == null ? "—" : readableBytes(value);
  return (
    <div className="backup-step-progress">
      <div className="backup-progress-heading">
        <span>{stepStatusLabels[step.status] ?? step.status}</span>
        <strong>
          {value == null ? "进度待更新" : `${Math.round(value)}%`}
        </strong>
      </div>
      <Progress
        value={value}
        active={step.status === "running"}
        failed={step.status === "failed" || step.status === "cancelled"}
        aria-label={stepTitle(step)}
      />
      <div className="backup-progress-metrics">
        <span>
          已处理{progress?.estimated ? "（估算）" : ""}{" "}
          {bytes(progress?.processed_bytes)} / 总量{" "}
          {bytes(progress?.total_bytes)}
        </span>
        <span>
          预计剩余（ETA）：
          {progress
            ? backupETA(progress, step.status === "running", now)
            : "计算中"}
        </span>
      </div>
    </div>
  );
}

function BackupRunDetails({ runId }: { runId: string }) {
  const query = useQuery({
    queryKey: ["backup-run", runId],
    queryFn: ({ signal }) =>
      api<BackupRun>(`/backups/runs/${runId}`, { signal }),
    refetchInterval: (q) => (activeBackupRun(q.state.data) ? 1000 : false),
    refetchOnWindowFocus: false,
  });
  const [now, setNow] = useState(Date.now);
  const running = activeBackupRun(query.data);
  useEffect(() => {
    if (!running) return;
    const timer = setInterval(() => setNow(Date.now()), 1000);
    return () => clearInterval(timer);
  }, [running]);
  if (query.isPending) return <p role="status">正在读取执行详情…</p>;
  if (!query.data)
    return (
      <p role="alert">
        {query.error?.message}
        <Button
          variant="outline"
          size="sm"
          onClick={() => void query.refetch()}
        >
          重试
        </Button>
      </p>
    );
  const run = query.data;
  return (
    <section className="backup-run-details" aria-label="执行详情">
      <h3>执行详情 · {runStatusLabels[run.status]}</h3>
      <p className="small-note">
        政策版本 {run.revision} ·{" "}
        {run.trigger === "manual" ? "手动触发" : "计划触发"} ·{" "}
        {runPhaseLabels[run.phase] ?? run.phase}
      </p>
      <div className="backup-run-times small-note">
        <span>创建：{backupRunTime(run.created_at)}</span>
        <span>开始：{backupRunTime(run.started_at)}</span>
        <span>结束：{backupRunTime(run.finished_at)}</span>
        {run.scheduled_for && (
          <span>计划：{backupRunTime(run.scheduled_for)}</span>
        )}
      </div>
      {query.error && (
        <p className="form-error" role="alert">
          进度更新失败：{query.error.message}
          <Button
            size="sm"
            variant="outline"
            onClick={() => void query.refetch()}
          >
            重试
          </Button>
        </p>
      )}
      {run.error && (
        <p className="form-error" role="alert">
          {run.error}
        </p>
      )}
      <ol className="backup-timeline" aria-label="执行步骤">
        {backupTimeline(run).map((step) => {
          const category = run.categories.find(
            (item) => item.category === step.category,
          );
          const delivery = category?.deliveries.find(
            (item) =>
              step.destination &&
              JSON.stringify(item.destination) ===
                JSON.stringify(step.destination),
          );
          return (
            <li
              key={step.id}
              className="backup-timeline-step"
              data-status={step.status}
              aria-current={step.status === "running" ? "step" : undefined}
            >
              <div className="backup-step-heading">
                <h4>{stepTitle(step)}</h4>
                <span className="backup-step-status">
                  {stepStatusLabels[step.status] ?? step.status}
                </span>
              </div>
              {(step.started_at || step.finished_at) && (
                <p className="small-note">
                  {step.started_at && `开始 ${backupRunTime(step.started_at)}`}
                  {step.started_at && step.finished_at && " · "}
                  {step.finished_at &&
                    `结束 ${backupRunTime(step.finished_at)}`}
                </p>
              )}
              {step.error && (
                <p className="form-error">
                  {step.kind === "cleanup" ? "旧版本清理：" : ""}
                  {step.error}
                </p>
              )}
              <StepProgress step={step} now={now} />
              {["verify", "legacy"].includes(step.kind) && category?.name && (
                <div className="backup-step-artifact">
                  <p>
                    {category.name} · {readableBytes(category.bytes)}
                  </p>
                  <p className="small-note">
                    SHA-256：<code>{category.sha256}</code>
                  </p>
                </div>
              )}
              {step.kind === "deliver" && delivery?.object && (
                <p className="small-note">存储位置：{delivery.object}</p>
              )}
            </li>
          );
        })}
      </ol>
    </section>
  );
}

export function BackupRunHistory({
  policy,
  onClose,
}: {
  policy: PolicySummary;
  onClose: () => void;
}) {
  const [selected, setSelected] = useState<string>();
  const [refreshing, setRefreshing] = useState(false);
  const queryClient = useQueryClient();
  const query = useQuery({
    queryKey: ["backup-runs", policy.id, "history"],
    queryFn: async ({ signal }) => {
      const first = await api<BackupRunList>(
        `/backups/policies/${policy.id}/runs?page=1`,
        { signal },
      );
      const pageCount = Math.min(5, Math.ceil(first.total / first.page_size));
      const pages = await Promise.all(
        Array.from({ length: Math.max(0, pageCount - 1) }, (_, index) =>
          api<BackupRunList>(
            `/backups/policies/${policy.id}/runs?page=${index + 2}`,
            { signal },
          ),
        ),
      );
      // Publish all pages together. Mixing a refreshed first page with cached
      // later pages loses runs that move across a page boundary during polling.
      return mergeBackupRuns(first.items, ...pages.map((page) => page.items));
    },
    refetchInterval: (q) =>
      q.state.data?.some(activeBackupRun) ? 3000 : false,
    refetchOnWindowFocus: false,
  });
  const runs = query.data ?? [];
  const current = runs.some((run) => run.id === selected)
    ? selected
    : runs[0]?.id;
  const refresh = async () => {
    setRefreshing(true);
    try {
      await Promise.all([
        query.refetch(),
        ...(current
          ? [
              queryClient.refetchQueries({
                queryKey: ["backup-run", current],
                exact: true,
              }),
            ]
          : []),
      ]);
    } finally {
      setRefreshing(false);
    }
  };
  return (
    <Dialog
      open
      onOpenChange={(open) => {
        if (!open) onClose();
      }}
    >
      <DialogContent className="backup-history-dialog">
        <DialogHeader>
          <DialogTitle>执行历史 · {policy.name}</DialogTitle>
          <DialogDescription>
            保留最近 100 条执行记录。历史记录的保留不影响备份文件份数。
          </DialogDescription>
        </DialogHeader>
        <div className="backup-history-scroll">
          <div className="backup-history-toolbar">
            <Select
              value={current ?? ""}
              onValueChange={setSelected}
              disabled={!runs.length}
            >
              <SelectTrigger
                className="backup-history-select"
                aria-label="执行任务"
              >
                <SelectValue
                  placeholder={
                    query.isPending ? "正在读取执行历史…" : "暂无执行记录"
                  }
                />
              </SelectTrigger>
              <SelectContent className="backup-history-options">
                {runs.map((run) => (
                  <SelectItem key={run.id} value={run.id}>
                    {backupRunTime(run.created_at)} ·{" "}
                    {run.trigger === "manual" ? "手动" : "计划"} ·{" "}
                    {runStatusLabels[run.status]}
                  </SelectItem>
                ))}
              </SelectContent>
            </Select>
            <Button
              size="sm"
              variant="outline"
              disabled={refreshing}
              onClick={() => void refresh()}
            >
              <RefreshCw
                size={14}
                className={refreshing ? "animate-spin" : ""}
                aria-hidden="true"
              />
              刷新历史
            </Button>
          </div>
          {query.error && (
            <p role="alert" className="form-error">
              历史更新失败：{query.error.message}
            </p>
          )}
          {query.isFetching && (
            <p role="status" className="small-note">
              正在读取执行记录…
            </p>
          )}
          {query.data && !runs.length && <p>暂无执行记录。</p>}
          {current && <BackupRunDetails key={current} runId={current} />}
        </div>
      </DialogContent>
    </Dialog>
  );
}
