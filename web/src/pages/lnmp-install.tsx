import {
  useEffect,
  useRef,
  useState,
  useId,
  cloneElement,
  type ReactElement,
} from "react";
import { Link } from "react-router-dom";
import { useMutation, useQueryClient } from "@tanstack/react-query";
import {
  ArrowLeft,
  CheckCircle2,
  Download,
  Loader2,
  Play,
  RefreshCw,
  Terminal,
} from "lucide-react";
import { toast } from "sonner";
import { Button } from "@/components/ui/button";
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
import { Input } from "@/components/ui/input";
import { Checkbox } from "@/components/ui/checkbox";
import { api, fetchSession, refreshStreamSession } from "@/lib/api";
import {
  activeInstall,
  binaryAllowed,
  initialInstallRequest,
  installationKey,
  useInstallation,
  type InstallJob,
  type InstallLine,
  type InstallLogPage,
  type InstallRequest,
} from "@/lib/lnmp-install";

const statusNames: Record<InstallJob["status"], string> = {
  preparing: "准备中",
  ready: "等待配置",
  running: "安装中",
  succeeded: "安装完成",
  failed: "安装失败",
  interrupted: "安装中断",
};
function Field({
  label,
  children,
  hint,
}: {
  label: string;
  children: ReactElement<{ id?: string; "aria-describedby"?: string }>;
  hint?: string;
}) {
  const id = useId();
  return (
    <div className="install-field">
      <label htmlFor={id}>{label}</label>
      {cloneElement(children, {
        id,
        "aria-describedby": hint ? `${id}-hint` : undefined,
      })}
      {hint && (
        <small id={`${id}-hint`} className="small-note">
          {hint}
        </small>
      )}
    </div>
  );
}
function InstallForm({
  job,
  blocked,
  onStart,
}: {
  job: InstallJob;
  blocked: boolean;
  onStart: () => void;
}) {
  const [form, setForm] = useState<InstallRequest>(() =>
    initialInstallRequest(job),
  );
  const [dbConfirmation, setDBConfirmation] = useState("");
  const [authConfirmation, setAuthConfirmation] = useState("");
  const client = useQueryClient();
  const o = job.options!;
  const change = <K extends keyof InstallRequest>(
    key: K,
    value: InstallRequest[K],
  ) => setForm((old) => ({ ...old, [key]: value }));
  const start = useMutation({
    mutationFn: () =>
      api<InstallJob>(`/lnmp-installation/jobs/${job.id}/start`, {
        method: "POST",
        body: JSON.stringify({
          ...form,
          database_password: form.database === 0 ? "" : form.database_password,
          auth_user: form.protection === "basic" ? form.auth_user : "",
          auth_password: form.protection === "basic" ? form.auth_password : "",
        }),
      }),
    onSuccess: () => {
      setForm((old) => ({ ...old, database_password: "", auth_password: "" }));
      setDBConfirmation("");
      setAuthConfirmation("");
      onStart();
      void client.invalidateQueries({ queryKey: installationKey });
    },
    onError: (e) => toast.error(e.message),
  });
  return (
    <Card>
      <CardHeader>
        <CardTitle>安装配置</CardTitle>
        <p className="small-note">
          源码提交 {o.revision.slice(0, 12)} · {o.architecture}
        </p>
      </CardHeader>
      <CardContent>
        <form
          onSubmit={(event) => {
            event.preventDefault();
            if (
              form.database !== 0 &&
              !/^[a-zA-Z0-9!@#$%^&*()_+=.,:?-]{12,72}$/.test(
                form.database_password,
              )
            ) {
              toast.error("数据库密码字符不符合安装脚本要求");
              return;
            }
            if (
              form.database !== 0 &&
              form.database_password !== dbConfirmation
            ) {
              toast.error("两次数据库密码不一致");
              return;
            }
            if (
              form.protection === "basic" &&
              form.auth_password !== authConfirmation
            ) {
              toast.error("两次 Basic Auth 密码不一致");
              return;
            }
            start.mutate();
          }}
        >
          <fieldset
            disabled={start.isPending || blocked}
            className="install-fieldset"
          >
            <div className="install-form-grid">
              <Field label="数据库">
                <select
                  className="install-select"
                  value={form.database}
                  onChange={(e) => {
                    const db = Number(e.target.value);
                    setForm((old) => ({
                      ...old,
                      database: db,
                      binary: binaryAllowed(db, o.architecture),
                    }));
                  }}
                >
                  {o.databases.map((c) => (
                    <option key={c.id} value={c.id}>
                      {c.name}
                    </option>
                  ))}
                </select>
              </Field>
              {form.database !== 0 && (
                <>
                  <Field label="数据库安装方式">
                    <select
                      className="install-select"
                      value={form.binary ? "binary" : "source"}
                      onChange={(e) =>
                        change("binary", e.target.value === "binary")
                      }
                    >
                      {binaryAllowed(form.database, o.architecture) && (
                        <option value="binary">通用二进制包</option>
                      )}
                      <option value="source">源码编译</option>
                    </select>
                  </Field>
                  <Field
                    label="数据库 root 密码"
                    hint="12–72 位：英文字母、数字及 !@#$%^&*()_+=.,:?-"
                  >
                    <Input
                      type="password"
                      required
                      minLength={12}
                      maxLength={72}
                      autoComplete="new-password"
                      value={form.database_password}
                      onChange={(e) =>
                        change("database_password", e.target.value)
                      }
                    />
                  </Field>
                  <Field label="确认数据库密码">
                    <Input
                      type="password"
                      required
                      autoComplete="new-password"
                      value={dbConfirmation}
                      onChange={(e) => setDBConfirmation(e.target.value)}
                    />
                  </Field>
                  <label className="install-check">
                    <Checkbox
                      checked={form.innodb}
                      onCheckedChange={(v) => change("innodb", v === true)}
                    />
                    启用 InnoDB 存储引擎
                  </label>
                </>
              )}
              <Field label="PHP 版本">
                <select
                  className="install-select"
                  value={form.php}
                  onChange={(e) => change("php", Number(e.target.value))}
                >
                  {o.php.map((c) => (
                    <option
                      key={c.id}
                      value={c.id}
                      disabled={c.id === 1 && form.database === 0}
                    >
                      {c.name}
                    </option>
                  ))}
                </select>
              </Field>
              <Field label="内存分配器">
                <select
                  className="install-select"
                  value={form.allocator}
                  onChange={(e) => change("allocator", Number(e.target.value))}
                >
                  <option value={1}>不安装</option>
                  <option value={2}>Jemalloc</option>
                  <option value={3}>TCMalloc</option>
                </select>
              </Field>
              <Field
                label="默认站点 IPv4"
                hint="用于默认站点与自签名证书；NAT 服务器可填写公网 IPv4。"
              >
                <Input
                  value={form.ipv4}
                  required
                  onChange={(e) => change("ipv4", e.target.value)}
                />
              </Field>
              <Field label="phpMyAdmin 保护">
                <select
                  className="install-select"
                  value={form.protection}
                  onChange={(e) =>
                    change("protection", e.target.value as "random" | "basic")
                  }
                >
                  <option value="random">随机访问路径</option>
                  <option value="basic">固定路径 + Basic Auth</option>
                </select>
              </Field>
              {form.protection === "basic" && (
                <>
                  <Field label="Basic Auth 用户名">
                    <Input
                      required
                      maxLength={64}
                      pattern="[a-zA-Z0-9_][a-zA-Z0-9_.\-]*"
                      value={form.auth_user}
                      onChange={(e) => change("auth_user", e.target.value)}
                      autoComplete="off"
                    />
                  </Field>
                  <Field label="Basic Auth 密码">
                    <Input
                      type="password"
                      required
                      minLength={12}
                      maxLength={128}
                      value={form.auth_password}
                      onChange={(e) => change("auth_password", e.target.value)}
                      autoComplete="new-password"
                    />
                  </Field>
                  <Field label="确认 Basic Auth 密码">
                    <Input
                      type="password"
                      required
                      value={authConfirmation}
                      onChange={(e) => setAuthConfirmation(e.target.value)}
                      autoComplete="new-password"
                    />
                  </Field>
                </>
              )}
            </div>
            <div className="install-modules">
              <h3>Nginx 扩展模块</h3>
              <p className="small-note">
                默认全部添加，可单独取消。模块随 Nginx 静态编译。
              </p>
              <div className="install-module-grid">
                {o.modules.map((m) => (
                  <label className="install-module" key={m.id}>
                    <Checkbox
                      checked={form.nginx_modules.includes(m.id)}
                      onCheckedChange={(v) =>
                        change(
                          "nginx_modules",
                          v === true
                            ? [...form.nginx_modules, m.id]
                            : form.nginx_modules.filter((id) => id !== m.id),
                        )
                      }
                    />
                    <span>
                      <strong>{m.name}</strong>
                      <small>
                        {m.repository
                          .replace("https://github.com/", "")
                          .replace(/\.git$/, "")}
                      </small>
                    </span>
                  </label>
                ))}
              </div>
              {form.nginx_modules.includes("geoip2") && (
                <p className="small-note">
                  GeoIP2 将自动安装 MaxMind 库、开发头文件与查询工具，支持 apt /
                  dnf / yum。MMDB 数据库需自行配置。
                </p>
              )}
            </div>
            <Button type="submit" disabled={start.isPending || blocked}>
              {start.isPending ? (
                <Loader2 size={16} className="animate-spin" />
              ) : (
                <Play size={16} />
              )}
              开始安装 LNMP
            </Button>
          </fieldset>
          {start.error && (
            <p role="alert" className="notice">
              {start.error.message}
            </p>
          )}
        </form>
      </CardContent>
    </Card>
  );
}

function InstallationOutput({ job }: { job: InstallJob }) {
  const client = useQueryClient();
  const [lines, setLines] = useState<InstallLine[]>([]);
  const [live, setLive] = useState(false);
  const [error, setError] = useState("");
  const [follow, setFollow] = useState(true);
  const [history, setHistory] = useState<InstallLogPage | null>(null);
  const [historyLoading, setHistoryLoading] = useState(false);
  const cursor = useRef(0);
  const viewport = useRef<HTMLPreElement>(null);
  useEffect(() => {
    let disposed = false;
    let source: EventSource | null = null;
    let polling = false;
    let lastFailureCheck = 0;
    cursor.current = 0;
    setLines([]);
    setHistory(null);
    setError("");
    const append = (incoming: InstallLine[]) => {
      if (disposed) return;
      setLines((old) =>
        [
          ...old,
          ...incoming.filter((line) => line.id > (old.at(-1)?.id ?? 0)),
        ].slice(-2000),
      );
    };
    const poll = async () => {
      if (polling || disposed) return;
      polling = true;
      try {
        let more = true;
        let pages = 0;
        while (more && !disposed && pages++ < 8) {
          const page = await api<InstallLogPage>(
            `/lnmp-installation/jobs/${job.id}/logs?after=${cursor.current}`,
          );
          if (disposed) return;
          append(page.lines);
          cursor.current = Math.max(cursor.current, page.cursor);
          more = page.more;
        }
        if (!disposed) setError("");
      } catch (e) {
        if (!disposed) setError((e as Error).message);
      } finally {
        polling = false;
      }
    };
    const connect = () => {
      source = new EventSource(
        `/api/v1/lnmp-installation/jobs/${job.id}/events?after=${cursor.current}`,
      );
      source.onopen = () => {
        if (!disposed) setLive(true);
      };
      source.addEventListener("log", (event) => {
        if (disposed) return;
        const line = JSON.parse((event as MessageEvent).data) as InstallLine;
        append([line]);
        cursor.current = Math.max(cursor.current, line.id);
      });
      source.addEventListener("status", (event) => {
        const update = JSON.parse((event as MessageEvent).data) as InstallJob;
        client.setQueryData<{
          installed: boolean;
          blocked: string[];
          job?: InstallJob;
        }>(installationKey, (old) => (old ? { ...old, job: update } : old));
      });
      source.addEventListener("complete", () => {
        source?.close();
        if (!disposed) {
          setLive(false);
          void poll();
          void client.invalidateQueries({ queryKey: installationKey });
        }
      });
      source.addEventListener("failure", (event) => {
        source?.close();
        if (!disposed) {
          setLive(false);
          setError(JSON.parse((event as MessageEvent).data).error);
        }
      });
      source.addEventListener("auth_expired", () => {
        source?.close();
        if (!disposed) {
          setLive(false);
          void refreshStreamSession().catch(() => {});
        }
      });
      source.onerror = () => {
        if (!disposed) {
          setLive(false);
          void poll();
          if (Date.now() - lastFailureCheck > 30000) {
            lastFailureCheck = Date.now();
            void fetchSession().catch(() => {});
          }
        }
      };
    };
    void poll().then(() => {
      if (!disposed && activeInstall(job)) connect();
    });
    const timer = window.setInterval(() => {
      if (!source || source.readyState !== EventSource.OPEN) void poll();
    }, 2000);
    return () => {
      disposed = true;
      source?.close();
      window.clearInterval(timer);
      setLive(false);
    };
    // Keep the connection and cursor across preparing/ready and running transitions.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [job.id, job.status === "running"]);
  useEffect(() => {
    if (follow && viewport.current)
      viewport.current.scrollTop = viewport.current.scrollHeight;
  }, [lines, follow]);
  async function loadHistory() {
    setHistoryLoading(true);
    try {
      const page = await api<InstallLogPage>(
        `/lnmp-installation/jobs/${job.id}/logs?after=${history?.cursor ?? 0}`,
      );
      setHistory(page);
    } catch (e) {
      toast.error((e as Error).message);
    } finally {
      setHistoryLoading(false);
    }
  }
  return (
    <Card className="install-output">
      <CardHeader className="install-output-heading">
        <div className="flex items-center gap-2">
          <Terminal size={17} />
          <CardTitle>安装与编译输出</CardTitle>
        </div>
        <span className="small-note">
          {live ? "实时连接" : activeInstall(job) ? "轮询更新" : "已保存"}
        </span>
      </CardHeader>
      <CardContent>
        <div className="install-log-toolbar">
          <label className="install-check">
            <Checkbox
              checked={follow}
              onCheckedChange={(v) => setFollow(v === true)}
            />
            跟随最新输出
          </label>
          <Button
            variant="outline"
            size="sm"
            onClick={() => void loadHistory()}
            disabled={historyLoading || (history !== null && !history.more)}
          >
            {historyLoading ? (
              <Loader2 size={14} className="animate-spin" />
            ) : (
              <RefreshCw size={14} />
            )}
            {history ? "下一页历史" : "读取历史日志"}
          </Button>
          <a
            className="install-link"
            href={`/api/v1/lnmp-installation/jobs/${job.id}/logs?download=1`}
          >
            <Download size={14} />
            下载完整日志
          </a>
        </div>
        {error && (
          <p role="alert" className="notice">
            {error}
          </p>
        )}
        {history && (
          <details open>
            <summary>历史日志 · 游标 {history.cursor}</summary>
            <pre className="install-terminal install-history">
              {history.lines.map((l) => l.text).join("\n") || "暂无输出"}
            </pre>
          </details>
        )}
        <pre
          ref={viewport}
          className="install-terminal"
          aria-label="安装编译输出"
          tabIndex={0}
        >
          {lines.map((l) => l.text).join("\n") || "等待任务输出…"}
        </pre>
        <p className="small-note">
          页面保留最近 2,000 行；历史日志与下载包含已保存的完整输出。
        </p>
      </CardContent>
    </Card>
  );
}

export function LNMPInstall() {
  const state = useInstallation();
  const client = useQueryClient();
  const [now, setNow] = useState(Date.now());
  const job = state.data?.job;
  useEffect(() => {
    const timer = window.setInterval(() => setNow(Date.now()), 1000);
    return () => window.clearInterval(timer);
  }, []);
  useEffect(() => {
    if (job?.status === "succeeded") {
      for (const key of ["services", "components", "vhosts", "vhost-count"])
        void client.invalidateQueries({ queryKey: [key] });
    }
  }, [job?.status, client]);
  const prepare = useMutation({
    mutationFn: () =>
      api<InstallJob>("/lnmp-installation/prepare", {
        method: "POST",
        body: JSON.stringify({ id: crypto.randomUUID() }),
      }),
    onSuccess: () =>
      void client.invalidateQueries({ queryKey: installationKey }),
    onError: (e) => toast.error(e.message),
  });
  const active = activeInstall(job);
  const blocked = Boolean(state.data?.installed || state.data?.blocked.length);
  const seconds = job
    ? Math.max(
        0,
        Math.floor(
          ((active ? now : new Date(job.updated_at).getTime()) -
            new Date(job.created_at).getTime()) /
            1000,
        ),
      )
    : 0;
  return (
    <>
      <div className="page-heading">
        <div>
          <div className="eyebrow muted">LNMP INSTALLATION</div>
          <h1>在线安装 LNMP</h1>
          <p>配置运行环境，实时查看安装与编译输出。</p>
        </div>
        <Link className="install-link" to="/services">
          <ArrowLeft size={15} />
          LNMP 服务
        </Link>
      </div>
      {state.isPending && (
        <p className="notice">
          <Loader2 className="animate-spin" size={16} />
          正在检查服务器…
        </p>
      )}
      {state.error && (
        <div role="alert" className="notice">
          {state.error.message}
          <Button
            variant="outline"
            size="sm"
            onClick={() => void state.refetch()}
          >
            重试
          </Button>
        </div>
      )}
      {state.data && (
        <>
          {!active && state.data.installed && (
            <div className="install-notice">
              <CheckCircle2 size={20} />
              <strong>此服务器已安装 LNMP</strong>
              <Link className="install-link" to="/services">
                进入服务管理 →
              </Link>
            </div>
          )}
          {!active &&
            !state.data.installed &&
            state.data.blocked.length > 0 && (
              <div role="alert" className="notice">
                <div>
                  <strong>安装前需要处理以下项目</strong>
                  <ul>
                    {state.data.blocked.map((b) => (
                      <li key={b}>{b}</li>
                    ))}
                  </ul>
                  <p>处理完成后刷新检查。已有数据和源码由管理员确认后清理。</p>
                </div>
              </div>
            )}
          {(!job || job.status === "failed" || job.status === "interrupted") &&
            !state.data.installed && (
              <Card>
                <CardContent className="install-prepare">
                  <div>
                    <h2>准备安装源码</h2>
                    <p className="small-note">
                      从 azpanel/lnmp2.3-offline 的 main
                      分支获取最新源码，读取安装选项。
                    </p>
                  </div>
                  <Button
                    disabled={blocked || prepare.isPending}
                    onClick={() => prepare.mutate()}
                  >
                    {prepare.isPending ? (
                      <Loader2 size={16} className="animate-spin" />
                    ) : (
                      <Download size={16} />
                    )}
                    {job ? "重新准备" : "获取源码与安装选项"}
                  </Button>
                </CardContent>
              </Card>
            )}
          {job && (
            <div className="install-progress">
              <span className={`install-status install-status-${job.status}`}>
                {active && <Loader2 size={16} className="animate-spin" />}
                {statusNames[job.status]}
              </span>
              <strong>{job.phase}</strong>
              <span className="small-note">
                {Math.floor(seconds / 60)} 分 {seconds % 60} 秒
              </span>
              {active && (
                <span className="small-note">
                  关闭页面或重启面板不会停止安装
                </span>
              )}
            </div>
          )}
          {job?.error && (
            <div role="alert" className="notice">
              {job.error}
            </div>
          )}
          {job?.result && (
            <div className="install-notice">
              <CheckCircle2 size={20} />
              <span>{job.result}</span>
            </div>
          )}
          {job?.status === "ready" && job.options && (
            <InstallForm
              key={job.id}
              job={job}
              blocked={blocked}
              onStart={() => void state.refetch()}
            />
          )}
          {job && <InstallationOutput job={job} />}
          {job?.module_revisions && (
            <details className="small-note">
              <summary>本次模块提交记录</summary>
              {Object.entries(job.module_revisions).map(([id, rev]) => (
                <p key={id}>
                  {id} · {rev}
                </p>
              ))}
            </details>
          )}
        </>
      )}
    </>
  );
}
