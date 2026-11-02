import { useId, useState } from "react";
import { useQuery } from "@tanstack/react-query";
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
import { api } from "@/lib/api";
import {
  destinationLabel,
  readableBytes,
  type PolicySummary,
} from "@/lib/backup-policies";
import {
  activeBackupRun,
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

function BackupRunDetails({ runId }: { runId: string }) {
  const query = useQuery({
    queryKey: ["backup-run", runId],
    queryFn: ({ signal }) =>
      api<BackupRun>(`/backups/runs/${runId}`, { signal }),
    refetchInterval: (q) => (activeBackupRun(q.state.data) ? 3000 : false),
  });
  if (query.isPending) return <p role="status">正在读取执行详情…</p>;
  if (query.error)
    return (
      <p role="alert">
        {query.error.message}
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
      <p className="small-note">
        创建：{backupRunTime(run.created_at)}
        <br />
        开始：{backupRunTime(run.started_at)}
        <br />
        结束：{backupRunTime(run.finished_at)}
      </p>
      {run.scheduled_for && (
        <p className="small-note">
          计划时间：{backupRunTime(run.scheduled_for)}
        </p>
      )}
      {run.error && (
        <p className="form-error" role="alert">
          {run.error}
        </p>
      )}
      {run.categories.map((category) => (
        <article className="backup-run-category" key={category.category}>
          <h4>
            {category.category === "databases" ? "数据库备份" : "文件备份"} ·{" "}
            {runStatusLabels[category.status] ?? category.status}
          </h4>
          {category.error && <p className="form-error">{category.error}</p>}
          {category.name && (
            <>
              <p>
                {category.name} · {readableBytes(category.bytes)}
              </p>
              <p className="small-note">
                SHA-256：<code>{category.sha256}</code>
              </p>
            </>
          )}
          {category.deliveries.map((delivery, index) => (
            <div className="backup-run-delivery" key={index}>
              <strong>
                {destinationLabel(delivery.destination)} ·{" "}
                {runStatusLabels[delivery.status] ?? delivery.status}
              </strong>
              {delivery.object && (
                <p className="small-note">存储位置：{delivery.object}</p>
              )}
              {delivery.error && <p className="form-error">{delivery.error}</p>}
              {delivery.cleanup_error && (
                <p className="form-error">
                  旧版本清理：{delivery.cleanup_error}
                </p>
              )}
            </div>
          ))}
        </article>
      ))}
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
  const [page, setPage] = useState(1);
  const [selected, setSelected] = useState<string>();
  const query = useQuery({
    queryKey: ["backup-runs", policy.id, page],
    queryFn: ({ signal }) =>
      api<BackupRunList>(`/backups/policies/${policy.id}/runs?page=${page}`, {
        signal,
      }),
    refetchInterval: (q) =>
      q.state.data?.items.some(activeBackupRun) ? 3000 : false,
  });
  const current = selected ?? query.data?.items[0]?.id;
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
          <Button
            size="sm"
            variant="outline"
            disabled={query.isFetching}
            onClick={() => void query.refetch()}
          >
            刷新历史
          </Button>
          {query.isPending ? (
            <p role="status">正在读取执行历史…</p>
          ) : query.error ? (
            <p role="alert">{query.error.message}</p>
          ) : (
            <>
              {!query.data.items.length && <p>暂无执行记录。</p>}
              <div className="backup-run-list">
                {query.data.items.map((run) => (
                  <button
                    type="button"
                    key={run.id}
                    className="backup-run-row"
                    aria-pressed={current === run.id}
                    onClick={() => setSelected(run.id)}
                  >
                    <span>{backupRunTime(run.created_at)}</span>
                    <span>
                      {run.trigger === "manual" ? "手动" : "计划"} ·{" "}
                      {runStatusLabels[run.status]}
                    </span>
                  </button>
                ))}
              </div>
              {query.data.total > query.data.page_size && (
                <div className="policy-inline">
                  <Button
                    size="sm"
                    variant="outline"
                    disabled={page <= 1}
                    onClick={() => {
                      setSelected(undefined);
                      setPage(page - 1);
                    }}
                  >
                    上一页
                  </Button>
                  <span>第 {page} 页</span>
                  <Button
                    size="sm"
                    variant="outline"
                    disabled={page * query.data.page_size >= query.data.total}
                    onClick={() => {
                      setSelected(undefined);
                      setPage(page + 1);
                    }}
                  >
                    下一页
                  </Button>
                </div>
              )}
            </>
          )}
          {current && <BackupRunDetails key={current} runId={current} />}
        </div>
      </DialogContent>
    </Dialog>
  );
}
