import { useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { Button } from "@/components/ui/button";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogHeader,
  DialogTitle,
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
  const query = useQuery({
    queryKey: ["backup-runtime"],
    queryFn: ({ signal }) => api<BackupRuntime>("/backups/runtime", { signal }),
    retry: 1,
  });
  const names: Record<string, string> = {
    "7zip": "7-Zip",
    dump: "数据库导出工具",
    gdrive: "gdrive",
    rclone: "rclone",
  };
  return (
    <section className="backup-runtime notice" aria-label="备份运行环境">
      <div className="policy-inline">
        <strong>备份运行环境</strong>
        <Button
          size="sm"
          variant="outline"
          disabled={query.isFetching}
          onClick={() => void query.refetch()}
        >
          重新检测
        </Button>
      </div>
      {query.isPending ? (
        <p role="status">正在检查调度与工具…</p>
      ) : query.error ? (
        <p role="alert">运行环境检查失败：{query.error.message}</p>
      ) : (
        <>
          <p>
            {query.data.scheduler
              ? "定时调度已就绪"
              : query.data.scheduler_error}
          </p>
          <p>
            {query.data.dependencies
              .map(
                (d) =>
                  `${names[d.name] ?? d.name}：${d.ready ? "可用" : "未安装或权限不安全"}`,
              )
              .join("；")}
          </p>
        </>
      )}
      <p className="small-note">
        压缩需要 7zz 或 7z；数据库备份还需要 mysqldump 或
        mariadb-dump。缺少的工具请由管理员安装。定时认证使用服务器 root
        客户端配置或 Socket，表单临时查询密码不会用于自动备份。
      </p>
    </section>
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
