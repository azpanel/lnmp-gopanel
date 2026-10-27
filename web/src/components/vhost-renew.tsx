import { useEffect, useRef, useState } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { Loader2, RefreshCw, Terminal } from "lucide-react";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Badge } from "@/components/ui/badge";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select";
import { api } from "@/lib/api";
import { useVhostJob } from "@/lib/use-vhost-job";
import type { Vhost } from "@/lib/types";
import type {
  VhostJob,
  VhostRenewItem,
  VhostRenewPreview,
} from "@/lib/vhost-create";

const labels = {
  queued: "等待中",
  running: "执行中",
  succeeded: "成功",
  failed: "失败",
};
const phases: Record<string, string> = {
  preflight: "检查配置",
  ssl: "签发证书",
  nginx: "检查 Nginx",
  deploy: "部署证书",
  done: "完成",
};
export function VhostRenew({
  names,
  sites,
  onStarted,
  table,
}: {
  names: string[];
  sites: Vhost[];
  onStarted: () => void;
  table: boolean;
}) {
  const client = useQueryClient();
  const [open, setOpen] = useState(false);
  const [selected, setSelected] = useState<string | null>(null);
  const [preview, setPreview] = useState<VhostRenewPreview[]>([]);
  const [error, setError] = useState("");
  const [logSite, setLogSite] = useState("all");
  const requestID = useRef(crypto.randomUUID());
  const logRef = useRef<HTMLPreElement>(null);
  const follow = useRef(true);
  const recent = useQuery({
    queryKey: ["vhost-jobs"],
    queryFn: () => api<VhostJob[]>("/vhosts/jobs"),
    refetchInterval: 5000,
  });
  const history = recent.data?.filter((job) => job.kind === "ssl-renew") || [];
  const task = useVhostJob(selected, open);
  const job = task.data;
  const check = useMutation({
    mutationFn: (input: { names: string[]; overrides?: VhostRenewItem[] }) =>
      api<VhostRenewPreview[]>("/vhosts/ssl/renew/preview", {
        method: "POST",
        body: JSON.stringify({ names: input.names }),
      }),
    onSuccess: (data, input) =>
      setPreview(
        data.map((row) => {
          const override = input.overrides?.find(
            (item) => item.name === row.name,
          );
          return override
            ? { ...row, provider: override.provider, email: override.email }
            : row;
        }),
      ),
    onError: (e) => setError(e.message),
  });
  const submit = useMutation({
    mutationFn: () =>
      api<VhostJob>("/vhosts/ssl/renew", {
        method: "POST",
        body: JSON.stringify({
          request_id: requestID.current,
          items: preview
            .filter((item) => item.eligible)
            .map(({ name, provider, email }) => ({ name, provider, email })),
        }),
      }),
    onSuccess: (data) => {
      client.setQueryData(["vhost-job", data.id], data);
      setSelected(data.id);
      setError("");
      onStarted();
      void client.invalidateQueries({ queryKey: ["vhost-jobs"] });
    },
    onError: (e) => setError(e.message),
  });
  function begin(selectedNames: string[], overrides?: VhostRenewItem[]) {
    requestID.current = crypto.randomUUID();
    setSelected(null);
    setPreview([]);
    setError("");
    setLogSite("all");
    follow.current = true;
    setOpen(true);
    check.mutate({ names: selectedNames, overrides });
  }
  useEffect(() => {
    if (follow.current && logRef.current)
      logRef.current.scrollTop = logRef.current.scrollHeight;
  }, [job?.lines.at(-1)?.id, logSite]);
  function update(name: string, values: Partial<VhostRenewPreview>) {
    setPreview((old) =>
      old.map((item) => (item.name === name ? { ...item, ...values } : item)),
    );
  }
  const eligible = preview.filter((item) => item.eligible);
  const latest = history[0];
  const siteName = (name: string) =>
    sites.find((site) => site.name === name)?.domains[0] || name;
  return (
    <>
      {latest && (
        <Button
          size="sm"
          variant="outline"
          onClick={() => {
            setSelected(latest.id);
            setLogSite("all");
            setError("");
            setOpen(true);
          }}
        >
          <Terminal size={14} />
          {latest.status === "running" ? "查看续签进度" : "最近续签结果"}
        </Button>
      )}
      {table && (
        <Button
          size="sm"
          variant="outline"
          disabled={!names.length}
          onClick={() => begin(names)}
        >
          <RefreshCw size={14} />
          提前续签 SSL{names.length ? `（${names.length}）` : ""}
        </Button>
      )}
      <Dialog open={open} onOpenChange={setOpen}>
        <DialogContent className="vhost-renew-dialog">
          <DialogHeader>
            <DialogTitle>
              {selected ? "SSL 续签任务" : "提前续签 SSL"}
            </DialogTitle>
            <DialogDescription>
              {selected
                ? "关闭窗口后任务继续执行。面板进程重启后任务记录不保留，请刷新站点确认实际状态。"
                : "逐站选择签发机构和联系邮箱，按列表顺序重新签发。使用 HTTP 验证，域名需指向本机且 80 端口可访问。"}
            </DialogDescription>
          </DialogHeader>
          {(error || task.error) && (
            <div className="notice" role="alert">
              {error || task.error?.message}
            </div>
          )}
          {selected ? (
            job ? (
              <>
                <div className="renew-progress" role="status">
                  <strong>
                    已完成 {job.completed || 0} / {job.items?.length || 0}
                  </strong>
                  <span>
                    成功 {job.succeeded || 0} · 失败 {job.failed || 0} ·{" "}
                    {job.status === "running" ? "执行中" : "已结束"}
                  </span>
                  <progress
                    aria-label="SSL 续签进度"
                    max={job.items?.length || 1}
                    value={job.completed || 0}
                  />
                </div>
                <div className="renew-items">
                  {job.items?.map((item) => (
                    <div className="renew-item" key={item.name}>
                      <div>
                        <strong>{siteName(item.name)}</strong>
                        <div className="small-note">
                          {item.name} ·{" "}
                          {item.provider === "zerossl"
                            ? "ZeroSSL"
                            : "Let's Encrypt"}
                        </div>
                        {item.error && (
                          <p className="site-detail-error" role="alert">
                            {item.error}
                          </p>
                        )}
                      </div>
                      <Badge
                        variant="outline"
                        className={
                          item.status === "failed"
                            ? "status-red"
                            : item.status === "succeeded"
                              ? "status-green"
                              : ""
                        }
                      >
                        {item.status === "running" && (
                          <Loader2 size={12} className="animate-spin" />
                        )}
                        {labels[item.status || "queued"]}
                        {item.status === "running" && item.phase
                          ? ` · ${phases[item.phase] || item.phase}`
                          : ""}
                      </Badge>
                    </div>
                  ))}
                </div>
                <label className="small-note" htmlFor="renew-log-site">
                  签发输出
                </label>
                <Select
                  value={logSite}
                  onValueChange={(value) => {
                    setLogSite(value);
                    follow.current = true;
                  }}
                >
                  <SelectTrigger id="renew-log-site">
                    <SelectValue />
                  </SelectTrigger>
                  <SelectContent>
                    <SelectItem value="all">全部站点</SelectItem>
                    {job.items?.map((item) => (
                      <SelectItem key={item.name} value={item.name}>
                        {siteName(item.name)}
                      </SelectItem>
                    ))}
                  </SelectContent>
                </Select>
                <pre
                  className="create-task-log"
                  aria-label="SSL 签发输出"
                  ref={logRef}
                  onScroll={(e) => {
                    const el = e.currentTarget;
                    follow.current =
                      el.scrollHeight - el.scrollTop - el.clientHeight < 40;
                  }}
                >
                  {job.lines
                    .filter(
                      (line) => logSite === "all" || line.name === logSite,
                    )
                    .map((line) => (
                      <div key={line.id}>
                        <span className="create-log-time">
                          {new Date(line.time).toLocaleTimeString("zh-CN", {
                            hour12: false,
                          })}{" "}
                          [{line.name || "任务"}]{" "}
                        </span>
                        {line.text}
                      </div>
                    ))}
                  {!job.lines.length && "等待签发输出…"}
                </pre>
                <p className="small-note">
                  保留最近 1000 行输出。
                  {job.status === "running"
                    ? task.live
                      ? "实时连接已建立"
                      : "正在重连，使用轮询更新"
                    : "任务已结束"}
                </p>
                <DialogFooter>
                  {job.status !== "running" && Boolean(job.failed) && (
                    <Button
                      variant="outline"
                      onClick={() => {
                        const failed = job.items!.filter(
                          (item) => item.status === "failed",
                        );
                        begin(
                          failed.map((item) => item.name),
                          failed,
                        );
                      }}
                    >
                      重试失败项
                    </Button>
                  )}
                  <Button variant="outline" onClick={() => setOpen(false)}>
                    关闭
                  </Button>
                </DialogFooter>
              </>
            ) : (
              <div className="small-note">正在读取任务…</div>
            )
          ) : check.isPending ? (
            <div className="flex items-center gap-2">
              <Loader2 className="animate-spin" />
              正在检查站点配置…
            </div>
          ) : (
            <form
              onSubmit={(e) => {
                e.preventDefault();
                setError("");
                submit.mutate();
              }}
            >
              <div className="renew-items">
                {preview.map((item) => {
                  const site = sites.find((site) => site.name === item.name);
                  return (
                    <div className="renew-config-item" key={item.name}>
                      <strong>{siteName(item.name)}</strong>
                      <div className="small-note font-mono">{item.name}</div>
                      {site?.certificates[0] && (
                        <div className="small-note">
                          当前签发者：{site.certificates[0].issuer || "未知"} ·
                          到期：
                          {site.certificates[0].not_after
                            ? new Date(
                                site.certificates[0].not_after,
                              ).toLocaleDateString("zh-CN")
                            : "未知"}
                        </div>
                      )}
                      {item.domains.length > 0 && (
                        <div className="small-note">
                          签发域名：{item.domains.join("、")}
                        </div>
                      )}
                      {item.eligible ? (
                        <div className="renew-config-fields">
                          <div>
                            <label htmlFor={`provider-${item.name}`}>
                              签发机构
                            </label>
                            <Select
                              value={item.provider}
                              onValueChange={(value) =>
                                update(item.name, {
                                  provider: value as VhostRenewItem["provider"],
                                })
                              }
                              disabled={submit.isPending}
                            >
                              <SelectTrigger id={`provider-${item.name}`}>
                                <SelectValue />
                              </SelectTrigger>
                              <SelectContent>
                                <SelectItem value="letsencrypt">
                                  Let's Encrypt
                                </SelectItem>
                                <SelectItem value="zerossl">ZeroSSL</SelectItem>
                              </SelectContent>
                            </Select>
                          </div>
                          <div>
                            <label htmlFor={`email-${item.name}`}>
                              联系邮箱
                            </label>
                            <Input
                              id={`email-${item.name}`}
                              type="email"
                              maxLength={254}
                              required
                              value={item.email}
                              disabled={submit.isPending}
                              onChange={(e) =>
                                update(item.name, { email: e.target.value })
                              }
                            />
                          </div>
                        </div>
                      ) : (
                        <p className="site-detail-error">
                          无法续签：{item.reason}
                        </p>
                      )}
                    </div>
                  );
                })}
              </div>
              <DialogFooter>
                <Button
                  type="button"
                  variant="outline"
                  disabled={submit.isPending}
                  onClick={() => setOpen(false)}
                >
                  取消
                </Button>
                <Button
                  type="submit"
                  disabled={submit.isPending || !eligible.length}
                >
                  {submit.isPending && <Loader2 className="animate-spin" />}
                  开始逐个续签（{eligible.length}）
                </Button>
              </DialogFooter>
            </form>
          )}
        </DialogContent>
      </Dialog>
    </>
  );
}
