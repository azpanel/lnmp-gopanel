import { useEffect, useRef, useState, type ReactNode } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import {
  ArrowRightLeft,
  BadgeCheck,
  Check,
  CircleHelp,
  Code2,
  Database,
  Eye,
  EyeOff,
  FileBadge,
  FileKey2,
  FileText,
  FolderOpen,
  Globe,
  KeyRound,
  Link,
  Loader2,
  LockKeyhole,
  Mail,
  Network,
  Plus,
  RefreshCw,
  Route,
  Search,
  ScrollText,
  ShieldCheck,
  Shuffle,
  Terminal,
  UserRoundCog,
  UserRound,
  type LucideIcon,
} from "lucide-react";
import { Tooltip } from "radix-ui";
import { toast } from "sonner";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Checkbox } from "@/components/ui/checkbox";
import {
  Dialog,
  DialogContent,
  DialogDescription,
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
import { api, fetchSession } from "@/lib/api";
import {
  certificateCovers,
  generateDatabasePassword,
  newVhostInput,
  splitDomains,
  type DNSCheck,
  type VhostCreateInput,
  type VhostCreateOptions,
  type VhostJob,
  type VhostJobLine,
} from "@/lib/vhost-create";

const statusLabels = {
  running: "执行中",
  succeeded: "已完成",
  partial: "部分完成",
  failed: "失败",
};
const phaseLabels: Record<string, string> = {
  preflight: "检查配置",
  http: "创建网站",
  database: "创建数据库",
  ssl: "配置 SSL",
  nginx: "检查 Nginx",
  done: "执行结束",
};
const dnsLabels = {
  matched: "已解析到本机",
  partial: "部分 IP 匹配",
  mismatch: "未解析到本机",
  no_records: "无 A／AAAA 记录",
  unknown: "无法确认",
};

function FieldLabel({
  htmlFor,
  icon: Icon,
  children,
}: {
  htmlFor: string;
  icon: LucideIcon;
  children: ReactNode;
}) {
  return (
    <Label htmlFor={htmlFor} className="create-field-label">
      <Icon size={14} aria-hidden="true" />
      {children}
    </Label>
  );
}

function DNSHelp({ warnings }: { warnings: string[] }) {
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
            aria-label="解析校验说明"
            onClick={(event) => {
              event.preventDefault();
              setOpen(true);
            }}
          >
            <CircleHelp size={15} aria-hidden="true" />
          </Button>
        </Tooltip.Trigger>
        <Tooltip.Portal>
          <Tooltip.Content
            className="create-help-tooltip"
            side="top"
            sideOffset={6}
            collisionPadding={16}
          >
            {[...new Set(warnings)].map((warning) => (
              <p key={warning}>{warning}</p>
            ))}
            <p>解析校验仅供参考，未通过仍可继续；CDN 或代理可能显示其他 IP。</p>
            <Tooltip.Arrow className="create-help-arrow" />
          </Tooltip.Content>
        </Tooltip.Portal>
      </Tooltip.Root>
    </Tooltip.Provider>
  );
}

function Choice({
  id,
  label,
  icon,
  value,
  onChange,
  children,
}: {
  id: string;
  label: string;
  icon: LucideIcon;
  value: string;
  onChange: (value: string) => void;
  children: ReactNode;
}) {
  return (
    <div className="create-field">
      <FieldLabel htmlFor={id} icon={icon}>
        {label}
      </FieldLabel>
      <Select value={value} onValueChange={onChange}>
        <SelectTrigger id={id}>
          <SelectValue />
        </SelectTrigger>
        <SelectContent>{children}</SelectContent>
      </Select>
    </div>
  );
}
function Toggle({
  id,
  label,
  icon,
  checked,
  onChange,
  disabled,
}: {
  id: string;
  label: string;
  icon: LucideIcon;
  checked: boolean;
  onChange: (value: boolean) => void;
  disabled?: boolean;
}) {
  return (
    <div className="create-toggle">
      <Checkbox
        id={id}
        checked={checked}
        onCheckedChange={(value) => onChange(value === true)}
        disabled={disabled}
      />
      <FieldLabel htmlFor={id} icon={icon}>
        {label}
      </FieldLabel>
    </div>
  );
}

export function VhostCreate() {
  const client = useQueryClient();
  const [open, setOpen] = useState(false);
  const [input, setInput] = useState<VhostCreateInput>(newVhostInput);
  const [aliases, setAliases] = useState("");
  const [retryName, setRetryName] = useState<string | null>(null);
  const [selected, setSelected] = useState<string | null>(null);
  const [live, setLive] = useState(false);
  const [error, setError] = useState("");
  const [dns, setDNS] = useState<DNSCheck | null>(null);
  const [showDatabasePassword, setShowDatabasePassword] = useState(false);
  const logsRef = useRef<HTMLPreElement>(null);
  const followLogs = useRef(true);
  const completed = useRef(new Set<string>());
  const recent = useQuery({
    queryKey: ["vhost-jobs"],
    queryFn: () => api<VhostJob[]>("/vhosts/jobs"),
    refetchInterval: 5000,
  });
  const options = useQuery({
    queryKey: ["vhost-create-options"],
    queryFn: () => api<VhostCreateOptions>("/vhosts/options"),
    enabled: open && !selected,
    staleTime: 0,
  });
  const task = useQuery({
    queryKey: ["vhost-job", selected],
    queryFn: () =>
      api<VhostJob>(`/vhosts/jobs/${encodeURIComponent(selected!)}`),
    enabled: open && Boolean(selected),
    refetchInterval: (query) =>
      !live && query.state.data?.status === "running" ? 2000 : false,
  });
  const running = task.data?.status === "running";
  const data = options.data;
  const domains = [input.domain.trim(), ...splitDomains(aliases)].filter(
    Boolean,
  );
  const autoSSL =
    input.ssl.mode === "letsencrypt" || input.ssl.mode === "zerossl";
  const selectedCertificate = data?.certificates.find(
    (cert) => cert.id === input.ssl.certificate_id,
  );
  const covered =
    selectedCertificate &&
    selectedCertificate.usable &&
    certificateCovers(selectedCertificate.certificate.dns_names, domains);
  const set = <K extends keyof VhostCreateInput>(
    key: K,
    value: VhostCreateInput[K],
  ) => setInput((old) => ({ ...old, [key]: value }));
  const refreshSites = () => {
    for (const key of [
      "vhosts",
      "vhost-count",
      "services",
      "vhost-config",
      "vhost-jobs",
    ])
      void client.invalidateQueries({ queryKey: [key] });
  };
  useEffect(() => {
    setDNS(null);
  }, [input.domain, aliases]);
  useEffect(() => {
    if (!open || !selected || !running) return;
    const source = new EventSource(
      `/api/v1/vhosts/jobs/${encodeURIComponent(selected)}/events`,
    );
    const key = ["vhost-job", selected];
    source.onopen = () => setLive(true);
    source.addEventListener("snapshot", (event) =>
      client.setQueryData(
        key,
        JSON.parse((event as MessageEvent).data) as VhostJob,
      ),
    );
    source.addEventListener("status", (event) => {
      const update = JSON.parse((event as MessageEvent).data) as VhostJob;
      client.setQueryData<VhostJob>(key, (old) => ({
        ...update,
        lines: old?.lines || [],
      }));
    });
    source.addEventListener("log", (event) => {
      const line = JSON.parse((event as MessageEvent).data) as VhostJobLine;
      client.setQueryData<VhostJob>(key, (old) =>
        old
          ? {
              ...old,
              lines: [
                ...old.lines.filter((previous) => previous.id !== line.id),
                line,
              ]
                .sort((a, b) => a.id - b.id)
                .slice(-1000),
            }
          : old,
      );
    });
    source.addEventListener("complete", () => {
      source.close();
      setLive(false);
      refreshSites();
    });
    source.addEventListener("auth_expired", () => {
      source.close();
      window.dispatchEvent(new Event("session-expired"));
    });
    source.onerror = () => {
      setLive(false);
      void fetchSession().catch(() => {});
    };
    return () => {
      source.close();
      setLive(false);
    };
    // The stream lives for one running job; data changes must not reopen it.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [open, selected, running, client]);
  useEffect(() => {
    if (task.data && task.data.status !== "running") refreshSites();
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [task.data?.status]);
  useEffect(() => {
    const finished =
      recent.data?.filter(
        (job) => job.status !== "running" && !completed.current.has(job.id),
      ) || [];
    if (finished.length) {
      for (const job of finished) completed.current.add(job.id);
      refreshSites();
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [recent.data]);
  useEffect(() => {
    if (
      data &&
      input.php !== "none" &&
      !data.php.some((php) => php.id === input.php)
    )
      setInput((old) => ({
        ...old,
        php: data.php[0]?.id || "none",
        path_info: false,
      }));
  }, [data, input.php]);
  useEffect(() => {
    const pre = logsRef.current;
    if (pre && followLogs.current) pre.scrollTop = pre.scrollHeight;
  }, [task.data?.lines.at(-1)?.id]);
  useEffect(() => {
    followLogs.current = true;
  }, [selected]);

  const checkDNS = useMutation({
    mutationFn: (names: string[]) =>
      api<DNSCheck>("/vhosts/check-dns", {
        method: "POST",
        body: JSON.stringify({ domains: names }),
      }),
    onSuccess: (result, names) => {
      if (JSON.stringify(names) === JSON.stringify(domains)) setDNS(result);
    },
    onError: (e) => toast.error(e.message),
  });
  const submit = useMutation({
    mutationFn: (request: VhostCreateInput) =>
      api<VhostJob>(retryName ? "/vhosts/ssl/retry" : "/vhosts", {
        method: "POST",
        body: JSON.stringify(
          retryName
            ? {
                request_id: request.request_id,
                name: retryName,
                ssl: request.ssl,
              }
            : request,
        ),
      }),
    onSuccess: (job) => {
      client.setQueryData(["vhost-job", job.id], job);
      setSelected(job.id);
      setError("");
      setInput((old) => ({
        ...old,
        database: { ...old.database, password: "", admin_password: "" },
      }));
      void client.invalidateQueries({ queryKey: ["vhost-jobs"] });
    },
    onError: (e) => setError(e.message),
  });
  function begin() {
    setInput(newVhostInput());
    setAliases("");
    setSelected(null);
    setRetryName(null);
    setDNS(null);
    setShowDatabasePassword(false);
    setError("");
    setOpen(true);
  }
  function retry(job: VhostJob) {
    setInput({
      ...newVhostInput(),
      ssl: { ...input.ssl, mode: "letsencrypt" },
    });
    setRetryName(job.name);
    setSelected(null);
    setError("");
    setDNS(null);
  }
  const existingJob =
    recent.data?.find((job) => job.status === "running") || recent.data?.[0];
  return (
    <>
      <div className="flex flex-wrap items-center justify-end gap-2">
        {existingJob && (
          <Button
            size="sm"
            variant="outline"
            onClick={() => {
              setSelected(existingJob.id);
              setOpen(true);
            }}
          >
            <Terminal size={14} />
            {existingJob.status === "running" ? "查看建站进度" : "最近建站结果"}
          </Button>
        )}
        <Button size="sm" onClick={begin}>
          <Plus size={15} />
          添加站点
        </Button>
      </div>
      <Dialog open={open} onOpenChange={setOpen}>
        <DialogContent className="vhost-create-dialog">
          <DialogHeader>
            <DialogTitle>
              {selected ? "建站任务" : retryName ? "重试 SSL 申请" : "添加站点"}
            </DialogTitle>
            <DialogDescription>
              {selected
                ? "关闭窗口后任务继续执行，可重新打开查看进度。面板重启后请刷新站点确认实际状态。"
                : retryName
                  ? `为 ${retryName} 申请证书，保留网站和数据库。`
                  : "填写网站配置，可选创建数据库及配置 SSL 证书。"}
            </DialogDescription>
          </DialogHeader>
          {selected ? (
            <div className="create-task">
              {task.error && (
                <p className="site-detail-error" role="alert">
                  {task.error.message}
                </p>
              )}
              {!task.data && !task.error && (
                <p className="small-note">正在读取任务…</p>
              )}
              {task.data && (
                <>
                  <div className="create-task-heading">
                    <strong>{task.data.name}</strong>
                    <span
                      className={
                        task.data.status === "failed" ||
                        task.data.status === "partial"
                          ? "site-detail-error"
                          : "small-note"
                      }
                    >
                      {running && (
                        <Loader2 size={14} className="animate-spin" />
                      )}
                      {statusLabels[task.data.status]} ·{" "}
                      {phaseLabels[task.data.phase] || task.data.phase}
                    </span>
                  </div>
                  {task.data.result && (
                    <div className="create-result-grid">
                      <span>
                        网站：
                        {task.data.result.site_created ? "已创建" : "待创建"}
                      </span>
                      <span>
                        数据库：
                        {{
                          skipped: "未创建",
                          created: "已创建",
                          failed: "失败",
                        }[task.data.result.database] ||
                          task.data.result.database}
                      </span>
                      <span>
                        SSL：
                        {{
                          skipped: "未启用",
                          enabled: "已启用",
                          failed: "失败",
                        }[task.data.result.ssl] || task.data.result.ssl}
                      </span>
                    </div>
                  )}
                  {task.data.error && (
                    <p className="site-detail-error" role="alert">
                      {task.data.error}
                    </p>
                  )}
                  {task.data.result?.message && (
                    <p className="small-note">{task.data.result.message}</p>
                  )}
                  <pre
                    className="create-task-log"
                    ref={logsRef}
                    onScroll={(event) => {
                      const pre = event.currentTarget;
                      followLogs.current =
                        pre.scrollHeight - pre.scrollTop - pre.clientHeight <
                        80;
                    }}
                    aria-label="建站实时输出"
                    aria-live="polite"
                  >
                    {task.data.lines.map((line) => (
                      <div key={line.id}>
                        <span className="create-log-time">
                          {new Date(line.time).toLocaleTimeString("zh-CN", {
                            hour12: false,
                          })}
                        </span>{" "}
                        {line.text}
                      </div>
                    ))}
                    {!task.data.lines.length && "等待执行输出…"}
                  </pre>
                  {running && (
                    <p className="small-note">
                      {live
                        ? "实时连接已建立"
                        : "正在连接实时输出，自动查询任务状态…"}
                    </p>
                  )}
                  <div className="flex flex-wrap justify-end gap-2">
                    {!running &&
                      task.data.result?.site_created &&
                      task.data.result.ssl === "failed" && (
                        <Button onClick={() => retry(task.data!)}>
                          重试 SSL
                        </Button>
                      )}
                    {!running &&
                      !task.data.result?.site_created &&
                      task.data.error && (
                        <Button
                          onClick={() => {
                            setSelected(null);
                            setInput((old) => ({
                              ...old,
                              request_id: crypto.randomUUID(),
                            }));
                          }}
                        >
                          {task.data.error.includes("管理员密码")
                            ? "填写管理员密码"
                            : "修改表单重试"}
                        </Button>
                      )}
                    <Button variant="outline" onClick={() => setOpen(false)}>
                      关闭
                    </Button>
                  </div>
                </>
              )}
            </div>
          ) : (
            <form
              onSubmit={(event) => {
                event.preventDefault();
                setError("");
                if (!retryName && input.ssl.mode === "existing" && !covered) {
                  setError("请选择有效且覆盖全部域名的通配符证书");
                  return;
                }
                submit.mutate({
                  ...input,
                  domain: input.domain.trim(),
                  aliases: splitDomains(aliases),
                });
              }}
            >
              <div className="create-form-body">
                {options.isPending && (
                  <p className="small-note">
                    <Loader2 size={14} className="animate-spin" />
                    正在读取本机配置…
                  </p>
                )}
                {options.error && (
                  <p role="alert" className="site-detail-error">
                    {options.error.message}
                    <Button
                      type="button"
                      size="sm"
                      variant="outline"
                      onClick={() => void options.refetch()}
                    >
                      重试
                    </Button>
                  </p>
                )}
                {data?.warnings.map((warning) => (
                  <p className="small-note" key={warning}>
                    {warning}
                  </p>
                ))}
                {!retryName && (
                  <>
                    <fieldset
                      disabled={submit.isPending}
                      className="create-section create-section-website"
                    >
                      <legend className="sr-only">网站配置</legend>
                      <div className="create-domain-row">
                        <div className="create-field">
                          <FieldLabel htmlFor="site-domain" icon={Globe}>
                            主域名
                          </FieldLabel>
                          <Input
                            id="site-domain"
                            required
                            value={input.domain}
                            onChange={(e) => set("domain", e.target.value)}
                            placeholder="example.com"
                            autoCapitalize="none"
                          />
                        </div>
                        <div className="create-field">
                          <FieldLabel htmlFor="site-aliases" icon={Link}>
                            附加域名
                          </FieldLabel>
                          <Input
                            id="site-aliases"
                            value={aliases}
                            onChange={(e) => setAliases(e.target.value)}
                            placeholder="www.example.com，多个域名用空格分隔"
                          />
                        </div>
                        <div className="create-dns-actions">
                          <Button
                            type="button"
                            size="sm"
                            variant="outline"
                            disabled={checkDNS.isPending || !domains.length}
                            onClick={() => checkDNS.mutate(domains)}
                          >
                            {checkDNS.isPending ? (
                              <Loader2 size={14} className="animate-spin" />
                            ) : (
                              <Search size={14} />
                            )}
                            校验解析
                          </Button>
                          <DNSHelp warnings={dns?.warnings ?? []} />
                        </div>
                      </div>
                      {dns && (
                        <div className="create-dns">
                          <p className="small-note">
                            本机参考 IP：
                            {dns.server_ips.join("、") || "无法获取"}
                          </p>
                          {dns.records.map((record) => (
                            <div key={record.domain} className="create-dns-row">
                              <strong>{record.domain}</strong>
                              <span
                                className={
                                  record.status === "matched"
                                    ? "text-emerald-600"
                                    : "small-note"
                                }
                              >
                                {record.status === "matched" && (
                                  <Check size={13} />
                                )}
                                {dnsLabels[record.status]}
                              </span>
                              <code>
                                {record.addresses.join("、") ||
                                  record.error ||
                                  "无解析记录"}
                              </code>
                            </div>
                          ))}
                        </div>
                      )}
                      <div className="create-field">
                        <FieldLabel htmlFor="site-root" icon={FolderOpen}>
                          站点目录（绝对路径）
                        </FieldLabel>
                        <Input
                          id="site-root"
                          value={input.root}
                          onChange={(e) => set("root", e.target.value)}
                          placeholder={`/home/wwwroot/${input.domain || "example.com"}`}
                        />
                      </div>
                      <div className="create-grid">
                        <Choice
                          id="site-php"
                          label="PHP 版本"
                          icon={Code2}
                          value={input.php}
                          onChange={(value) => {
                            set("php", value);
                            set("path_info", false);
                          }}
                        >
                          <SelectItem value="none">
                            纯静态（不启用 PHP）
                          </SelectItem>
                          {data?.php.map((php) => (
                            <SelectItem key={php.id} value={php.id}>
                              {php.label}
                            </SelectItem>
                          ))}
                        </Choice>
                        <Choice
                          id="site-rewrite"
                          label="伪静态"
                          icon={ArrowRightLeft}
                          value={input.rewrite}
                          onChange={(value) => set("rewrite", value)}
                        >
                          {(data?.rewrites || ["none"]).map((name) => (
                            <SelectItem key={name} value={name}>
                              {name === "none" ? "不启用" : name}
                            </SelectItem>
                          ))}
                        </Choice>
                      </div>
                      <div className="create-toggles">
                        <Toggle
                          id="site-pathinfo"
                          label="启用 Pathinfo"
                          icon={Route}
                          checked={input.path_info}
                          onChange={(value) => set("path_info", value)}
                          disabled={
                            !data?.php.find((php) => php.id === input.php)
                              ?.path_info_available
                          }
                        />
                        <Toggle
                          id="site-ipv6"
                          label="启用 IPv6"
                          icon={Network}
                          checked={input.ipv6}
                          onChange={(value) => set("ipv6", value)}
                        />
                        <Toggle
                          id="site-log"
                          label="记录访问日志"
                          icon={ScrollText}
                          checked={input.access_log}
                          onChange={(value) => set("access_log", value)}
                        />
                      </div>
                      {input.access_log && (
                        <div className="create-field">
                          <FieldLabel htmlFor="site-log-name" icon={FileText}>
                            访问日志文件名
                          </FieldLabel>
                          <Input
                            id="site-log-name"
                            value={input.log_name}
                            onChange={(e) => set("log_name", e.target.value)}
                            placeholder={`${input.domain || "example.com"}.log`}
                          />
                          <span className="small-note">
                            保存在 /home/wwwlogs/ 下
                          </span>
                        </div>
                      )}
                    </fieldset>
                    <fieldset
                      disabled={submit.isPending}
                      className="create-section create-section-database"
                    >
                      <legend className="sr-only">数据库</legend>
                      <Toggle
                        id="site-db"
                        label="创建数据库及同名本机用户"
                        icon={Database}
                        checked={input.database.enabled}
                        onChange={(value) =>
                          set("database", { ...input.database, enabled: value })
                        }
                        disabled={!data?.database_available}
                      />
                      {input.database.enabled && (
                        <>
                          <div className="create-grid create-database-grid">
                            <div className="create-field">
                              <FieldLabel
                                htmlFor="site-db-name"
                                icon={UserRound}
                              >
                                数据库／用户名
                              </FieldLabel>
                              <Input
                                id="site-db-name"
                                required
                                pattern="[A-Za-z0-9_]{1,32}"
                                value={input.database.name}
                                onChange={(e) =>
                                  set("database", {
                                    ...input.database,
                                    name: e.target.value,
                                  })
                                }
                              />
                            </div>
                            <div className="create-field">
                              <div className="create-password-heading">
                                <FieldLabel
                                  htmlFor="site-db-pass"
                                  icon={KeyRound}
                                >
                                  数据库密码
                                </FieldLabel>
                                <Button
                                  type="button"
                                  size="sm"
                                  variant="outline"
                                  onClick={() => {
                                    try {
                                      set("database", {
                                        ...input.database,
                                        password: generateDatabasePassword(),
                                      });
                                    } catch {
                                      toast.error(
                                        "无法生成安全随机密码，请手动输入",
                                      );
                                    }
                                  }}
                                >
                                  <Shuffle size={13} aria-hidden="true" />
                                  随机生成强密码
                                </Button>
                              </div>
                              <div className="create-password-input">
                                <Input
                                  id="site-db-pass"
                                  type={
                                    showDatabasePassword ? "text" : "password"
                                  }
                                  required
                                  minLength={8}
                                  autoComplete="new-password"
                                  value={input.database.password}
                                  onChange={(e) =>
                                    set("database", {
                                      ...input.database,
                                      password: e.target.value,
                                    })
                                  }
                                />
                                <Button
                                  type="button"
                                  size="icon"
                                  variant="ghost"
                                  aria-label={
                                    showDatabasePassword
                                      ? "隐藏数据库密码"
                                      : "显示数据库密码"
                                  }
                                  aria-pressed={showDatabasePassword}
                                  onClick={() =>
                                    setShowDatabasePassword((old) => !old)
                                  }
                                >
                                  {showDatabasePassword ? (
                                    <EyeOff size={16} aria-hidden="true" />
                                  ) : (
                                    <Eye size={16} aria-hidden="true" />
                                  )}
                                </Button>
                              </div>
                            </div>
                          </div>
                          <div className="create-field">
                            <FieldLabel
                              htmlFor="site-db-admin"
                              icon={UserRoundCog}
                            >
                              数据库管理员密码
                              {data?.database_auth_available
                                ? "（可留空）"
                                : "（自动认证不可用时填写）"}
                            </FieldLabel>
                            <Input
                              id="site-db-admin"
                              type="password"
                              autoComplete="off"
                              value={input.database.admin_password}
                              onChange={(e) =>
                                set("database", {
                                  ...input.database,
                                  admin_password: e.target.value,
                                })
                              }
                            />
                            <span className="small-note">
                              优先使用本机凭据或 Unix
                              socket；填写的密码仅用于本次操作。
                            </span>
                          </div>
                        </>
                      )}
                    </fieldset>
                  </>
                )}
                <fieldset
                  disabled={submit.isPending}
                  className="create-section create-section-ssl"
                >
                  <legend className="sr-only">SSL 证书</legend>
                  <Choice
                    id="site-ssl"
                    label="证书来源"
                    icon={ShieldCheck}
                    value={input.ssl.mode}
                    onChange={(value) =>
                      set("ssl", {
                        ...input.ssl,
                        mode: value as VhostCreateInput["ssl"]["mode"],
                      })
                    }
                  >
                    {!retryName && (
                      <SelectItem value="none">不启用 SSL</SelectItem>
                    )}
                    <SelectItem
                      value="letsencrypt"
                      disabled={!data?.acme_available}
                    >
                      申请 Let's Encrypt
                    </SelectItem>
                    <SelectItem
                      value="zerossl"
                      disabled={!data?.acme_available}
                    >
                      申请 ZeroSSL
                    </SelectItem>
                    {!retryName && (
                      <>
                        <SelectItem value="files">
                          指定 cert / key 文件
                        </SelectItem>
                        <SelectItem
                          value="existing"
                          disabled={!data?.certificates.length}
                        >
                          已有通配符证书
                          {data ? `（${data.certificates.length} 张）` : ""}
                        </SelectItem>
                      </>
                    )}
                  </Choice>
                  {autoSSL && (
                    <>
                      <div className="create-field">
                        <FieldLabel htmlFor="site-ssl-email" icon={Mail}>
                          联系邮箱
                        </FieldLabel>
                        <Input
                          id="site-ssl-email"
                          type="email"
                          required
                          value={input.ssl.email}
                          onChange={(e) =>
                            set("ssl", {
                              ...input.ssl,
                              email: e.target.value.trim(),
                            })
                          }
                          placeholder="admin@example.com"
                        />
                      </div>
                      <p className="small-note">
                        使用 HTTP 验证，全部域名需能从公网 80
                        端口访问本站。新证书自动续期；申请失败保留 HTTP 站点。
                      </p>
                    </>
                  )}
                  {input.ssl.mode === "files" && (
                    <>
                      <div className="create-field">
                        <FieldLabel htmlFor="site-cert" icon={FileBadge}>
                          cert 文件绝对路径（完整证书链）
                        </FieldLabel>
                        <Input
                          id="site-cert"
                          required
                          value={input.ssl.cert_file}
                          onChange={(e) =>
                            set("ssl", {
                              ...input.ssl,
                              cert_file: e.target.value,
                            })
                          }
                          placeholder="/path/to/fullchain.pem"
                        />
                      </div>
                      <div className="create-field">
                        <FieldLabel htmlFor="site-key" icon={FileKey2}>
                          key 文件绝对路径
                        </FieldLabel>
                        <Input
                          id="site-key"
                          required
                          value={input.ssl.key_file}
                          onChange={(e) =>
                            set("ssl", {
                              ...input.ssl,
                              key_file: e.target.value,
                            })
                          }
                          placeholder="/path/to/privkey.pem"
                        />
                      </div>
                    </>
                  )}
                  {input.ssl.mode === "existing" && (
                    <>
                      <Choice
                        id="site-existing-cert"
                        label="通配符证书"
                        icon={BadgeCheck}
                        value={input.ssl.certificate_id}
                        onChange={(value) =>
                          set("ssl", { ...input.ssl, certificate_id: value })
                        }
                      >
                        {data?.certificates.map((cert) => (
                          <SelectItem
                            key={cert.id}
                            value={cert.id}
                            disabled={
                              !cert.usable ||
                              !certificateCovers(
                                cert.certificate.dns_names,
                                domains,
                              )
                            }
                          >
                            {cert.certificate.dns_names.join("、")} ·{" "}
                            {cert.certificate.not_after
                              ? new Date(
                                  cert.certificate.not_after,
                                ).toLocaleDateString("zh-CN")
                              : "未知到期时间"}
                            {!cert.usable
                              ? ` · ${cert.error}`
                              : !certificateCovers(
                                    cert.certificate.dns_names,
                                    domains,
                                  )
                                ? " · 未覆盖全部域名"
                                : ""}
                          </SelectItem>
                        ))}
                      </Choice>
                      {selectedCertificate && (
                        <div className="create-certificate-info">
                          <p>{selectedCertificate.certificate.issuer}</p>
                          <p>
                            {selectedCertificate.certificate.dns_names.join(
                              "、",
                            )}
                          </p>
                          <p>
                            到期时间：
                            {selectedCertificate.certificate.not_after
                              ? new Date(
                                  selectedCertificate.certificate.not_after,
                                ).toLocaleString("zh-CN", { hour12: false })
                              : "未知"}
                          </p>
                          <code>{selectedCertificate.cert_file}</code>
                          <code>{selectedCertificate.key_file}</code>
                          {!covered && (
                            <p className="site-detail-error">
                              此证书无效或未覆盖全部域名，请重新选择。
                            </p>
                          )}
                        </div>
                      )}
                      <p className="small-note">
                        扫描 /root/.acme.sh/
                        下的通配符证书，沿用其原有续期和加载设置。
                      </p>
                    </>
                  )}
                  {input.ssl.mode !== "none" && (
                    <Toggle
                      id="site-redirect"
                      label="强制 HTTPS（保留 HTTP 证书验证路径）"
                      icon={LockKeyhole}
                      checked={input.ssl.redirect}
                      onChange={(value) =>
                        set("ssl", { ...input.ssl, redirect: value })
                      }
                    />
                  )}
                  <Button
                    type="button"
                    variant="outline"
                    size="sm"
                    disabled={options.isFetching}
                    onClick={() => void options.refetch()}
                  >
                    <RefreshCw
                      size={13}
                      className={options.isFetching ? "animate-spin" : ""}
                    />
                    重新扫描配置与证书
                  </Button>
                </fieldset>
                {error && (
                  <p className="site-detail-error" role="alert">
                    {error}
                  </p>
                )}
              </div>
              <div className="create-form-footer">
                <Button
                  type="button"
                  variant="outline"
                  onClick={() => setOpen(false)}
                >
                  取消
                </Button>
                <Button
                  type="submit"
                  disabled={
                    submit.isPending ||
                    !data ||
                    Boolean(retryName && !data.acme_available)
                  }
                >
                  {submit.isPending && (
                    <Loader2 size={14} className="animate-spin" />
                  )}
                  {retryName ? "开始申请" : "创建站点"}
                </Button>
              </div>
            </form>
          )}
        </DialogContent>
      </Dialog>
    </>
  );
}
