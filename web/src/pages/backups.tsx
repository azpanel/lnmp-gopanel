import { useState } from "react";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { Archive, Download, Loader2 } from "lucide-react";
import { Button } from "@/components/ui/button";
import { Card, CardContent } from "@/components/ui/card";
import { Checkbox } from "@/components/ui/checkbox";
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
import { useSessionMutation } from "@/lib/use-session-mutation";

type ComponentName = "gdrive" | "rclone";
type ComponentItem = {
  name: ComponentName;
  status: "missing" | "ready" | "failed";
  version: string;
  supported: boolean;
  error?: string;
};
type InstallResult = {
  name: ComponentName;
  status: "pending" | "running" | "succeeded" | "failed" | "skipped";
  version?: string;
  error?: string;
};
type InstallJob = {
  id: string;
  status: "running" | "succeeded" | "failed";
  items: InstallResult[];
  error?: string;
};
type ComponentSnapshot = { items: ComponentItem[]; job?: InstallJob };
const labels = {
  pending: "等待安装",
  running: "安装中…",
  succeeded: "安装成功",
  failed: "安装失败",
  skipped: "已安装，已跳过",
};
const descriptions = {
  gdrive: "Google Drive 命令行工具",
  rclone: "多种云存储的文件同步工具",
};

export function Backups() {
  const [open, setOpen] = useState(false);
  const [selected, setSelected] = useState<ComponentName[]>([]);
  const client = useQueryClient();
  const query = useQuery({
    queryKey: ["backup-components"],
    queryFn: ({ signal }) =>
      api<ComponentSnapshot>("/backups/components", { signal }),
    refetchInterval: (query) =>
      query.state.data?.job?.status === "running" ? 1000 : false,
    retry: 1,
  });
  const install = useSessionMutation({
    mutationFn: (components: ComponentName[]) =>
      api<InstallJob>("/backups/components/install", {
        method: "POST",
        body: JSON.stringify({ components }),
      }),
    onSuccess: (job) => {
      setSelected([]);
      client.setQueryData<ComponentSnapshot>(["backup-components"], (old) => ({
        items: old?.items ?? [],
        job,
      }));
      void client.invalidateQueries({ queryKey: ["backup-components"] });
    },
    onError: () => {
      void client.invalidateQueries({ queryKey: ["backup-components"] });
    },
  });
  const job = query.data?.job;
  const running = job?.status === "running" || install.isPending;
  const items = query.data?.items ?? [];
  const available = selected.filter((name) =>
    items.some(
      (item) => item.name === name && item.supported && item.status !== "ready",
    ),
  );
  return (
    <>
      <div className="page-heading">
        <div>
          <div className="eyebrow muted">WEBSITE BACKUP</div>
          <h1>网站备份</h1>
          <p>安装并查看网站备份所需的组件。</p>
        </div>
        <Dialog
          open={open}
          onOpenChange={(value) => {
            setOpen(value);
            if (value) {
              setSelected([]);
              if (!install.isPending) install.reset();
            }
          }}
        >
          <DialogTrigger asChild>
            <Button size="sm">
              <Download size={14} />
              安装组件
            </Button>
          </DialogTrigger>
          <DialogContent className="backup-install-dialog">
            <DialogHeader>
              <DialogTitle>安装组件</DialogTitle>
              <DialogDescription>
                选择需要安装的组件，将下载最新正式版。关闭弹窗后安装仍会继续。
              </DialogDescription>
            </DialogHeader>
            {query.isPending && <p role="status">正在读取组件状态…</p>}
            {query.error && (
              <div className="notice" role="alert">
                组件状态读取失败：{query.error.message}
                <Button
                  variant="outline"
                  size="sm"
                  onClick={() => void query.refetch()}
                >
                  重试
                </Button>
              </div>
            )}
            <div className="backup-component-list">
              {items.map((item) => {
                const result = job?.items.find(
                  (entry) => entry.name === item.name,
                );
                return (
                  <div className="backup-component" key={item.name}>
                    <label className="backup-component-choice">
                      <Checkbox
                        aria-label={`安装 ${item.name}`}
                        checked={available.includes(item.name)}
                        disabled={
                          running ||
                          item.status === "ready" ||
                          !item.supported ||
                          query.isError
                        }
                        onCheckedChange={(checked) =>
                          setSelected((old) =>
                            checked === true
                              ? [...old, item.name]
                              : old.filter((name) => name !== item.name),
                          )
                        }
                      />
                      <span>
                        <strong>{item.name}</strong>
                        <span className="backup-component-description">
                          {descriptions[item.name]}
                        </span>
                      </span>
                    </label>
                    <div className="backup-component-status" role="status">
                      {result ? (
                        <span>
                          {result.status === "running" && (
                            <Loader2 size={14} className="animate-spin" />
                          )}
                          {labels[result.status]}
                          {result.version ? ` · ${result.version}` : ""}
                        </span>
                      ) : (
                        <span>
                          {item.status === "ready"
                            ? `已安装 · ${item.version}`
                            : item.status === "failed"
                              ? "版本读取失败"
                              : "未安装"}
                        </span>
                      )}
                      {(result?.error || item.error) && (
                        <p className="small-note">
                          {result?.error || item.error}
                        </p>
                      )}
                    </div>
                  </div>
                );
              })}
            </div>
            {job?.error && (
              <div className="notice" role="alert">
                {job.error}
              </div>
            )}
            {install.error && (
              <div className="notice" role="alert">
                {install.error.message}
              </div>
            )}
            <DialogFooter>
              <Button variant="outline" onClick={() => setOpen(false)}>
                关闭
              </Button>
              <Button
                disabled={running || !available.length || query.isError}
                onClick={() => install.mutate(available)}
              >
                {running ? (
                  <>
                    <Loader2 size={14} className="animate-spin" />
                    安装中…
                  </>
                ) : (
                  "开始安装"
                )}
              </Button>
            </DialogFooter>
          </DialogContent>
        </Dialog>
      </div>
      {query.error && (
        <div className="notice" role="alert">
          组件状态读取失败：{query.error.message}
          <Button
            size="sm"
            variant="outline"
            onClick={() => void query.refetch()}
          >
            重试
          </Button>
        </div>
      )}
      {query.isPending ? (
        <p role="status">正在读取组件状态…</p>
      ) : (
        <Card>
          <CardContent className="backup-components-card">
            <h2 className="card-title-icon">
              <Archive size={17} />
              备份组件
            </h2>
            <div className="backup-component-list">
              {items.map((item) => {
                const result = job?.items.find(
                  (entry) => entry.name === item.name,
                );
                return (
                  <div className="backup-component" key={item.name}>
                    <div>
                      <strong>{item.name}</strong>
                      <span className="backup-component-description">
                        {descriptions[item.name]}
                      </span>
                    </div>
                    <div className="backup-component-status" role="status">
                      <span>
                        {running &&
                        result &&
                        ["pending", "running"].includes(result.status)
                          ? labels[result.status]
                          : item.status === "ready"
                            ? `已安装 · ${item.version}`
                            : item.status === "failed"
                              ? "版本读取失败"
                              : "未安装"}
                      </span>
                      {(item.error || result?.error) && (
                        <p className="small-note">
                          {item.error || result?.error}
                        </p>
                      )}
                    </div>
                  </div>
                );
              })}
            </div>
          </CardContent>
        </Card>
      )}
    </>
  );
}
