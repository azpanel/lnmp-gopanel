import { useState } from "react";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { toast } from "sonner";
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
import type { Policy, PolicySummary } from "@/lib/backup-policies";

export function BackupPolicyList({
  onOpen,
}: {
  onOpen: (id: string, readOnly: boolean) => void;
}) {
  const client = useQueryClient();
  const [remove, setRemove] = useState<PolicySummary>();
  const [search, setSearch] = useState("");
  const query = useQuery({
    queryKey: ["backup-policies"],
    queryFn: ({ signal }) =>
      api<PolicySummary[]>("/backups/policies", { signal }),
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
  const items = (query.data ?? []).filter((p) =>
    p.name.toLowerCase().includes(search.toLowerCase()),
  );
  return (
    <Card className="backup-policy-list">
      <CardHeader>
        <CardTitle>备份政策</CardTitle>
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
                      <p className="small-note">
                        保留数据库备份 {p.retention.databases} 份，文件备份{" "}
                        {p.retention.files} 份（暂不自动清理）
                      </p>
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
    </Card>
  );
}
