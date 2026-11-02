import { useState } from "react";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { toast } from "sonner";
import { RefreshCw } from "lucide-react";
import { Button } from "@/components/ui/button";
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
import { Switch } from "@/components/ui/switch";
import {
  Dialog,
  DialogContent,
  DialogHeader,
  DialogTitle,
  DialogDescription,
  DialogFooter,
} from "@/components/ui/dialog";
import { api } from "@/lib/api";
import { useSessionMutation } from "@/lib/use-session-mutation";
import { destinationLabel } from "@/lib/backup-policies";
import type { Policy, PolicySummary } from "@/lib/backup-policies";
import {
  activeBackupRun,
  backupRunTime,
  runStatusLabels,
  type BackupRun,
} from "@/lib/backup-runs";
import { BackupRunHistory } from "@/components/backup-runs";

export function BackupPolicyList({
  onOpen,
}: {
  onOpen: (id: string, readOnly: boolean, copy?: boolean) => void;
}) {
  const client = useQueryClient();
  const [remove, setRemove] = useState<PolicySummary>();
  const [search, setSearch] = useState("");
  const [history, setHistory] = useState<PolicySummary>();
  const query = useQuery({
    queryKey: ["backup-policies"],
    queryFn: ({ signal }) =>
      api<PolicySummary[]>("/backups/policies", { signal }),
    refetchInterval: (q) =>
      q.state.data?.some((p) => activeBackupRun(p.last_run)) ? 3000 : false,
  });
  const invalidate = () => {
    void client.invalidateQueries({ queryKey: ["backup-policies"] });
    void client.invalidateQueries({ queryKey: ["backup-policy"] });
  };
  const toggle = useSessionMutation({
    mutationFn: async ({ id, enabled }: { id: string; enabled: boolean }) => {
      const p = await api<Policy>(`/backups/policies/${id}`);
      return api<Policy>(`/backups/policies/${id}`, {
        method: "PUT",
        body: JSON.stringify({
          ...p,
          enabled,
          password_action: "keep",
          password: "",
        }),
      });
    },
    onSuccess: (saved) => {
      client.setQueryData(["backup-policy", saved.id], saved);
      invalidate();
    },
    onError: (e) => toast.error(e.message),
  });
  const deletion = useSessionMutation({
    mutationFn: (p: PolicySummary) =>
      api(`/backups/policies/${p.id}`, {
        method: "DELETE",
        body: JSON.stringify({ revision: p.revision }),
      }),
    onSuccess: () => {
      setRemove(undefined);
      invalidate();
      toast.success("备份政策已删除");
    },
    onError: (e) => toast.error(e.message),
  });
  const execution = useSessionMutation({
    mutationFn: (policy: PolicySummary) =>
      api<BackupRun>(`/backups/policies/${policy.id}/run`, {
        method: "POST",
        body: JSON.stringify({ revision: policy.revision }),
      }),
    onSuccess: (run, policy) => {
      client.setQueryData<PolicySummary[]>(["backup-policies"], (old) =>
        old?.map((p) => (p.id === policy.id ? { ...p, last_run: run } : p)),
      );
      invalidate();
      void client.invalidateQueries({ queryKey: ["backup-runs", policy.id] });
      setHistory(policy);
      toast.success("备份任务已提交");
    },
    onError: (e) => toast.error(e.message),
  });
  const items = (query.data ?? []).filter((p) =>
    p.name.toLowerCase().includes(search.toLowerCase()),
  );
  return (
    <Card className="backup-policy-list">
      <CardHeader className="flex items-center justify-between">
        <CardTitle>备份政策</CardTitle>
        <Button
          size="icon-sm"
          variant="outline"
          aria-label="刷新政策"
          title="刷新政策"
          disabled={query.isFetching}
          onClick={() => void query.refetch()}
        >
          <RefreshCw
            size={14}
            className={query.isFetching ? "animate-spin" : ""}
            aria-hidden="true"
          />
        </Button>
      </CardHeader>
      <CardContent>
        {query.isPending ? (
          <p role="status">正在读取政策…</p>
        ) : query.error ? (
          <div className="notice" role="alert">
            {query.error.message}
            <Button
              size="sm"
              variant="outline"
              onClick={() => void query.refetch()}
            >
              重试
            </Button>
          </div>
        ) : (
          <>
            {!!query.data?.length && (
              <input
                className="policy-select"
                aria-label="搜索政策"
                placeholder="搜索政策"
                value={search}
                onChange={(e) => setSearch(e.target.value)}
              />
            )}
            {!items.length ? (
              <p className="small-note">
                {search
                  ? "没有匹配的政策。"
                  : "尚未添加备份政策，点击右上角按钮开始配置。"}
              </p>
            ) : (
              <div className="policy-summary-list">
                {items.map((p) => (
                  <article className="policy-summary" key={p.id}>
                    <div className="policy-summary-main">
                      <strong>{p.name}</strong>
                      <code>{p.schedule}</code>
                      <p className="small-note">
                        {p.compression.format.toUpperCase()} · 等级{" "}
                        {p.compression.level} ·{" "}
                        {p.compression.has_password
                          ? "已设置压缩密码"
                          : "不加密"}
                      </p>
                      <p className="small-note">
                        {p.database_enabled
                          ? p.database_mode === "all"
                            ? "自动包含新增数据库"
                            : `${p.database_count} 个数据库`
                          : "不备份数据库"}{" "}
                        · {p.file_count} 个文件来源
                      </p>
                      <p className="small-note policy-destination-summary">
                        {p.destinations?.length
                          ? p.destinations.map(destinationLabel).join("； ")
                          : "尚未配置存储位置"}
                      </p>
                      <p className="small-note">
                        保留数据库备份 {p.retention.databases} 份，文件备份{" "}
                        {p.retention.files}{" "}
                        份，每次按类别打包，交付成功后清理旧版本
                      </p>
                      <p className="small-note" role="status">
                        {p.last_run
                          ? `最近执行：${runStatusLabels[p.last_run.status]} · ${backupRunTime(p.last_run.created_at)}`
                          : "尚未执行备份"}
                      </p>
                      {p.last_run?.error && (
                        <p className="form-error">{p.last_run.error}</p>
                      )}
                    </div>
                    <div className="policy-summary-actions">
                      <label className="policy-switch">
                        <Switch
                          aria-label={`启用 ${p.name}`}
                          checked={p.enabled}
                          disabled={toggle.isPending}
                          onCheckedChange={(enabled) =>
                            toggle.mutate({ id: p.id, enabled })
                          }
                        />
                        {p.enabled ? "已启用" : "未启用"}
                      </label>
                      <div className="policy-inline">
                        <Button
                          size="sm"
                          disabled={
                            execution.isPending || activeBackupRun(p.last_run)
                          }
                          onClick={() => execution.mutate(p)}
                        >
                          立即备份
                        </Button>
                        <Button
                          size="sm"
                          variant="outline"
                          onClick={() => setHistory(p)}
                        >
                          执行历史
                        </Button>
                        <Button
                          size="sm"
                          variant="outline"
                          onClick={() => onOpen(p.id, true)}
                        >
                          查看
                        </Button>
                        <Button
                          size="sm"
                          variant="outline"
                          onClick={() => onOpen(p.id, false)}
                        >
                          编辑
                        </Button>
                        <Button
                          size="sm"
                          variant="outline"
                          onClick={() => onOpen(p.id, false, true)}
                        >
                          复制
                        </Button>
                        <Button
                          size="sm"
                          variant="outline"
                          disabled={p.last_run?.status === "running"}
                          onClick={() => {
                            deletion.reset();
                            setRemove(p);
                          }}
                        >
                          删除
                        </Button>
                      </div>
                    </div>
                  </article>
                ))}
              </div>
            )}
          </>
        )}
        <Dialog
          open={!!remove}
          onOpenChange={(open) => {
            if (!open && !deletion.isPending) setRemove(undefined);
          }}
        >
          <DialogContent>
            <DialogHeader>
              <DialogTitle>删除备份政策</DialogTitle>
              <DialogDescription>
                确认删除“{remove?.name}”？仅删除政策配置，保留已有备份文件。
              </DialogDescription>
            </DialogHeader>
            {deletion.error && <p role="alert">{deletion.error.message}</p>}
            <DialogFooter>
              <Button
                variant="outline"
                disabled={deletion.isPending}
                onClick={() => setRemove(undefined)}
              >
                取消
              </Button>
              <Button
                variant="destructive"
                disabled={deletion.isPending}
                onClick={() => {
                  if (remove) deletion.mutate(remove);
                }}
              >
                确认删除
              </Button>
            </DialogFooter>
          </DialogContent>
        </Dialog>
      </CardContent>
      {history && (
        <BackupRunHistory
          key={history.id}
          policy={history}
          onClose={() => setHistory(undefined)}
        />
      )}
    </Card>
  );
}
