import { useSessionMutation } from "@/lib/use-session-mutation";
import { useEffect, useRef, useState } from "react";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import {
  BadgeCheck,
  CalendarClock,
  CircleHelp,
  FileKey,
  Globe2,
  Loader2,
  Mail,
  RefreshCw,
  ShieldCheck,
  Terminal,
} from "lucide-react";
import { Tooltip } from "radix-ui";
import { CertificateProvider } from "@/components/certificate-provider";
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
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from "@/components/ui/table";
import { api } from "@/lib/api";
import { certificateIssuer } from "@/lib/certificate-issuer";
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
  canceled: "已取消",
};
const phases: Record<string, string> = {
  preflight: "检查配置",
  ssl: "签发证书",
  nginx: "检查 Nginx",
  deploy: "部署证书",
  done: "完成",
};

function BulkEmailHelp() {
  const [open, setOpen] = useState(false);
  return (
    <Tooltip.Provider delayDuration={150}>
      <Tooltip.Root open={open} onOpenChange={setOpen}>
        <Tooltip.Trigger asChild>
          <Button
            type="button"
            variant="ghost"
            size="icon"
            className="create-help-button"
            aria-label="批量联系邮箱说明"
            onClick={(event) => {
              event.preventDefault();
              setOpen(true);
            }}
          >
            <CircleHelp size={14} aria-hidden="true" />
          </Button>
        </Tooltip.Trigger>
        <Tooltip.Portal>
          <Tooltip.Content
            className="create-help-tooltip"
            style={{ pointerEvents: "none" }}
            side="top"
            sideOffset={6}
            collisionPadding={16}
          >
            应用后仍可逐站修改联系邮箱。
            <Tooltip.Arrow className="create-help-arrow" />
          </Tooltip.Content>
        </Tooltip.Portal>
      </Tooltip.Root>
    </Tooltip.Provider>
  );
}

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
  const [bulkEmail, setBulkEmail] = useState("");
  const bulkEmailRef = useRef<HTMLInputElement>(null);
  const [logSite, setLogSite] = useState("all");
  const requestID = useRef(crypto.randomUUID());
  const previewRequest = useRef<{
    sequence: number;
    controller?: AbortController;
  }>({ sequence: 0 });
  const invalidatePreview = () => {
    previewRequest.current.controller?.abort();
    previewRequest.current = { sequence: previewRequest.current.sequence + 1 };
  };
  useEffect(
    () => () => {
      previewRequest.current.controller?.abort();
      previewRequest.current.sequence++;
    },
    [],
  );
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
  const check = useSessionMutation({
    mutationFn: (input: {
      names: string[];
      overrides?: VhostRenewItem[];
      sequence: number;
      signal: AbortSignal;
    }) =>
      api<VhostRenewPreview[]>("/vhosts/ssl/renew/preview", {
        method: "POST",
        signal: input.signal,
        body: JSON.stringify({ names: input.names }),
      }),
    onSuccess: (data, input) => {
      if (
        input.sequence !== previewRequest.current.sequence ||
        input.signal.aborted
      )
        return;
      setPreview(
        data.map((row) => {
          const override = input.overrides?.find(
            (item) => item.name === row.name,
          );
          return override
            ? { ...row, provider: override.provider, email: override.email }
            : row;
        }),
      );
    },
    onError: (e, input) => {
      if (
        input.sequence === previewRequest.current.sequence &&
        !input.signal.aborted
      )
        setError(e.message);
    },
  });
  const submit = useSessionMutation({
    mutationFn: (input: { request_id: string; items: VhostRenewItem[] }) =>
      api<VhostJob>("/vhosts/ssl/renew", {
        method: "POST",
        body: JSON.stringify(input),
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
    invalidatePreview();
    requestID.current = crypto.randomUUID();
    setSelected(null);
    setPreview([]);
    setBulkEmail("");
    setError("");
    setLogSite("all");
    follow.current = true;
    setOpen(true);
    const controller = new AbortController();
    previewRequest.current.controller = controller;
    check.mutate({
      names: [...selectedNames],
      overrides,
      sequence: previewRequest.current.sequence,
      signal: controller.signal,
    });
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
            invalidatePreview();
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
      <Dialog
        open={open}
        onOpenChange={(value) => {
          if (!value) invalidatePreview();
          setOpen(value);
        }}
      >
        <DialogContent className="vhost-renew-dialog">
          <DialogHeader>
            <DialogTitle className="renew-dialog-title">
              <FileKey size={18} aria-hidden="true" />
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
                    成功 {job.succeeded || 0} · 失败 {job.failed || 0} · 取消{" "}
                    {job.canceled || 0} ·{" "}
                    {job.status === "running"
                      ? "执行中"
                      : job.status === "canceled"
                        ? "已取消"
                        : "已结束"}
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
                          <CertificateProvider
                            name={
                              item.provider === "zerossl"
                                ? "ZeroSSL"
                                : "Let's Encrypt"
                            }
                          />
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
                  <Button
                    variant="outline"
                    onClick={() => {
                      invalidatePreview();
                      setOpen(false);
                    }}
                  >
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
                if (check.isPending || submit.isPending || !eligible.length)
                  return;
                submit.mutate({
                  request_id: requestID.current,
                  items: eligible.map(({ name, provider, email }) => ({
                    name,
                    provider,
                    email,
                  })),
                });
              }}
            >
              {eligible.length > 1 && (
                <div className="renew-bulk-email">
                  <div className="renew-bulk-email-label">
                    <label htmlFor="renew-bulk-email">批量联系邮箱</label>
                    <BulkEmailHelp />
                  </div>
                  <div className="renew-bulk-email-controls">
                    <Input
                      id="renew-bulk-email"
                      ref={bulkEmailRef}
                      type="email"
                      maxLength={254}
                      placeholder="输入统一的联系邮箱"
                      value={bulkEmail}
                      disabled={submit.isPending}
                      onChange={(e) => setBulkEmail(e.target.value)}
                    />
                    <Button
                      type="button"
                      variant="outline"
                      size="sm"
                      disabled={submit.isPending || !bulkEmail.trim()}
                      onClick={() => {
                        if (!bulkEmailRef.current?.reportValidity()) return;
                        const email = bulkEmail.trim();
                        setPreview((old) =>
                          old.map((item) =>
                            item.eligible ? { ...item, email } : item,
                          ),
                        );
                      }}
                    >
                      应用到全部可续签站点
                    </Button>
                  </div>
                </div>
              )}
              <Table className="renew-config-table" aria-label="续签站点配置">
                <TableHeader>
                  <TableRow>
                    <TableHead>
                      <span>
                        <Globe2 size={14} aria-hidden="true" />
                        站点
                      </span>
                    </TableHead>
                    <TableHead>
                      <span>
                        <BadgeCheck size={14} aria-hidden="true" />
                        当前签发者
                      </span>
                    </TableHead>
                    <TableHead>
                      <span>
                        <CalendarClock size={14} aria-hidden="true" />
                        到期时间
                      </span>
                    </TableHead>
                    <TableHead>
                      <span>
                        <ShieldCheck size={14} aria-hidden="true" />
                        签发机构
                      </span>
                    </TableHead>
                    <TableHead>
                      <span>
                        <Mail size={14} aria-hidden="true" />
                        联系邮箱
                      </span>
                    </TableHead>
                  </TableRow>
                </TableHeader>
                {preview.map((item) => {
                  const site = sites.find((site) => site.name === item.name);
                  const certificate = site?.certificates[0];
                  return (
                    <TableBody className="renew-config-item" key={item.name}>
                      <TableRow>
                        <TableCell data-label="站点">
                          <strong>{siteName(item.name)}</strong>
                        </TableCell>
                        <TableCell
                          data-label="当前签发者"
                          title={certificate?.issuer}
                        >
                          <CertificateProvider
                            name={
                              certificateIssuer(certificate?.issuer || "").label
                            }
                          />
                        </TableCell>
                        <TableCell data-label="到期时间">
                          {certificate?.not_after
                            ? new Date(
                                certificate.not_after,
                              ).toLocaleDateString("zh-CN")
                            : "未知"}
                        </TableCell>
                        {item.eligible ? (
                          <>
                            <TableCell data-label="签发机构">
                              <label
                                className="sr-only"
                                htmlFor={`provider-${item.name}`}
                              >
                                签发机构
                              </label>
                              <Select
                                value={item.provider}
                                onValueChange={(value) =>
                                  update(item.name, {
                                    provider:
                                      value as VhostRenewItem["provider"],
                                  })
                                }
                                disabled={submit.isPending}
                              >
                                <SelectTrigger id={`provider-${item.name}`}>
                                  <SelectValue />
                                </SelectTrigger>
                                <SelectContent>
                                  <SelectItem value="letsencrypt">
                                    <CertificateProvider name="Let's Encrypt" />
                                  </SelectItem>
                                  <SelectItem value="zerossl">
                                    <CertificateProvider name="ZeroSSL" />
                                  </SelectItem>
                                </SelectContent>
                              </Select>
                            </TableCell>
                            <TableCell data-label="联系邮箱">
                              <label
                                className="sr-only"
                                htmlFor={`email-${item.name}`}
                              >
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
                            </TableCell>
                          </>
                        ) : (
                          <TableCell colSpan={2} className="renew-config-error">
                            <p className="site-detail-error">
                              无法续签：{item.reason}
                            </p>
                          </TableCell>
                        )}
                      </TableRow>
                    </TableBody>
                  );
                })}
              </Table>
              <DialogFooter>
                <Button
                  type="button"
                  variant="outline"
                  disabled={submit.isPending}
                  onClick={() => {
                    invalidatePreview();
                    setOpen(false);
                  }}
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
