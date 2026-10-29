import { useRef, useState } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import {
  AlertTriangle,
  Check,
  CircuitBoard,
  Code2,
  ExternalLink,
  Loader2,
  Play,
  RefreshCw,
  RotateCw,
  Square,
} from "lucide-react";
import { toast } from "sonner";
import { Button } from "@/components/ui/button";
import { Card, CardContent } from "@/components/ui/card";
import {
  Dialog,
  DialogClose,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
  DialogTrigger,
} from "@/components/ui/dialog";
import { ServiceIcon, serviceInfo, ServiceStatus } from "@/components/status";
import { api } from "@/lib/api";
import { LNMPInstallNotice } from "@/components/lnmp-install-notice";
import { ServiceManagementRepair } from "@/components/service-management-repair";
import { clock } from "@/lib/format";
import type { Service, ServiceRelease, ComponentSnapshot } from "@/lib/types";
const actions = {
  start: { label: "启动", icon: Play },
  stop: { label: "停止", icon: Square },
  restart: { label: "重启", icon: RotateCw },
  reload: { label: "重载", icon: RefreshCw },
};
type Action = keyof typeof actions;
export function Services({
  services,
  error,
  loading,
}: {
  services?: Service[];
  error?: string;
  loading: boolean;
}) {
  const [selection, setSelection] = useState<{
    service: Service;
    action: Action;
  } | null>(null);
  const actionTrigger = useRef<HTMLButtonElement | null>(null);
  const client = useQueryClient();
  const components = useQuery({
    queryKey: ["components"],
    queryFn: () => api<ComponentSnapshot>("/components"),
    refetchInterval: (query) =>
      query.state.data?.status === "loading" ? 1000 : 60000,
  });
  const refreshComponents = useMutation({
    mutationFn: () =>
      api<ComponentSnapshot>("/components/refresh", { method: "POST" }),
    onSuccess: (data) => client.setQueryData(["components"], data),
    onError: (e) => toast.error(e.message),
  });
  const refreshingComponents =
    refreshComponents.isPending || components.data?.status === "loading";
  const releases = useQuery({
    queryKey: ["service-releases"],
    queryFn: () => api<Record<string, ServiceRelease>>("/services/releases"),
    refetchInterval: (query) =>
      Object.values(query.state.data ?? {}).some(
        (item) => item.status === "loading",
      )
        ? 1000
        : 60000,
  });
  const refreshReleases = useMutation({
    mutationFn: () =>
      api<Record<string, ServiceRelease>>("/services/releases/refresh", {
        method: "POST",
      }),
    onSuccess: (data) => client.setQueryData(["service-releases"], data),
    onError: (e) => toast.error(e.message),
  });
  const checkingReleases =
    refreshReleases.isPending ||
    Object.values(releases.data ?? {}).some(
      (item) => item.status === "loading",
    );
  const mutation = useMutation({
    mutationFn: ({ service, action }: { service: Service; action: Action }) =>
      api<{ message: string }>(`/services/${service.name}/actions/${action}`, {
        method: "POST",
      }),
    onSuccess: (result) => {
      toast.success(result.message);
      setSelection(null);
    },
    onError: (e) => toast.error(e.message),
    onSettled: () => client.invalidateQueries({ queryKey: ["services"] }),
  });
  return (
    <>
      <LNMPInstallNotice />
      <div className="page-heading">
        <div>
          <div className="eyebrow muted">SERVICE MANAGEMENT</div>
          <h1>LNMP 服务</h1>
          <p>查看运行状态，管理你的核心服务。</p>
        </div>
        <div className="service-heading-actions">
          <Button
            variant="outline"
            size="sm"
            disabled={checkingReleases || releases.isPending}
            onClick={() => refreshReleases.mutate()}
          >
            <RefreshCw
              size={14}
              className={checkingReleases ? "animate-spin" : ""}
            />
            {checkingReleases ? "查询版本中…" : "检查最新版本"}
          </Button>
          <Button
            variant="outline"
            size="sm"
            disabled={loading || refreshingComponents}
            onClick={() => {
              void client.invalidateQueries({ queryKey: ["services"] });
              refreshComponents.mutate();
            }}
          >
            <RefreshCw
              size={14}
              className={loading || refreshingComponents ? "animate-spin" : ""}
            />
            刷新状态
          </Button>
        </div>
      </div>
      {error && (
        <div className="notice" role="alert">
          状态更新失败：{error}。下方状态可能已过期。
        </div>
      )}
      {releases.error && (
        <div className="notice" role="alert">
          最新版本查询失败：{releases.error.message}
        </div>
      )}
      <div className="service-cards">
        {services?.map((service) => (
          <Card key={service.name}>
            <CardContent>
              <div className="service-card-head">
                <ServiceIcon name={service.name} />
                <div className="grow">
                  <h2>{serviceInfo[service.name]?.title}</h2>
                  <p className="small-note">
                    {serviceInfo[service.name]?.subtitle}
                  </p>
                </div>
                <ServiceStatus state={service.state} />
              </div>
              <div className="service-meta">
                <span>
                  已安装版本
                  <strong className="font-mono" title={service.version_error}>
                    {service.version ||
                      (service.version_error ? "读取失败" : "读取中…")}
                  </strong>
                </span>
                <span>
                  管理方式
                  <strong>
                    {service.manager === "systemd"
                      ? "systemd"
                      : service.manager === "init"
                        ? "init 脚本"
                        : "LNMP 命令"}
                  </strong>
                </span>
                <span>
                  最近检查
                  <strong className="font-mono">
                    {service.checked_at.startsWith("0001")
                      ? "等待检查"
                      : clock(service.checked_at)}
                  </strong>
                </span>
              </div>
              {service.version_error && (
                <p className="service-version-error">
                  版本读取失败：{service.version_error}
                </p>
              )}
              <div className="service-message">
                <span
                  className={
                    service.state === "running"
                      ? "text-primary"
                      : "text-muted-foreground"
                  }
                >
                  {service.state === "running" ? (
                    <Check size={14} />
                  ) : (
                    <AlertTriangle size={14} />
                  )}
                </span>
                {service.message || "等待状态更新"}
              </div>
              {(service.actions.length > 0 || service.name === "nginx") && (
                <div className="service-actions">
                  {service.actions.map((action) => {
                    const item = actions[action as Action];
                    if (!item) return null;
                    const Icon = item.icon;
                    const unavailable =
                      service.busy ||
                      service.state === "unmanaged" ||
                      mutation.isPending ||
                      Boolean(error) ||
                      (action === "start" && service.state === "running") ||
                      ((action === "stop" || action === "reload") &&
                        service.state === "stopped");
                    return (
                      <Button
                        key={action}
                        variant="outline"
                        size="sm"
                        disabled={unavailable}
                        onClick={(event) => {
                          actionTrigger.current = event.currentTarget;
                          setSelection({ service, action: action as Action });
                        }}
                      >
                        <Icon size={13} />
                        {item.label}
                      </Button>
                    );
                  })}
                  {service.name === "nginx" && (
                    <NginxBuildInfo service={service} />
                  )}
                </div>
              )}
              {service.name === "nginx" && service.state === "unmanaged" && (
                <ServiceManagementRepair
                  disabled={
                    service.busy || mutation.isPending || Boolean(error)
                  }
                />
              )}
              {service.name === "redis" && (
                <p className="small-note mt-3">只读监控运行状态和版本。</p>
              )}
              <ReleaseInfo
                release={releases.data?.[service.name]}
                failed={Boolean(releases.error)}
              />
              {service.busy && <p className="small-note mt-3">正在执行操作…</p>}
            </CardContent>
          </Card>
        ))}
      </div>
      {!services?.length && (
        <Card>
          <CardContent className="large-empty">
            <CircuitBoard size={40} />
            <h2>{loading ? "正在读取服务…" : "未检测到可管理的服务"}</h2>
            <p>
              请在服务器上检查 /etc/lnmp-gopanel/services.yaml，然后重启面板。
            </p>
          </CardContent>
        </Card>
      )}
      <section
        className="programming-components"
        aria-labelledby="components-title"
      >
        <div className="component-heading">
          <h2 id="components-title">编程组件</h2>
          <p className="small-note">查看本机开发工具版本与官方最新发布。</p>
        </div>
        {(components.error || components.data?.error) && (
          <div className="notice" role="alert">
            组件版本查询失败：
            {components.error?.message || components.data?.error}
            。已有版本信息可能已过期。
          </div>
        )}
        <div className="service-cards">
          {componentDefinitions.map(({ name, title, subtitle }) => {
            const item = components.data?.items.find(
              (item) => item.name === name,
            );
            const failed = Boolean(
              components.error ||
              components.data?.error ||
              item?.status === "failed",
            );
            return (
              <Card key={name} data-component={name}>
                <CardContent>
                  <div className="service-card-head">
                    <span className="component-logo" aria-hidden="true">
                      <img
                        src={`/brands/${name}.svg`}
                        alt=""
                        width={36}
                        height={36}
                      />
                    </span>
                    <div className="grow">
                      <h3>{title}</h3>
                      <p className="small-note">{subtitle}</p>
                    </div>
                  </div>
                  <div className="service-meta">
                    <span>
                      已安装版本
                      <strong
                        className={
                          !item?.version &&
                          !failed &&
                          item?.status === "missing"
                            ? "font-mono component-missing"
                            : "font-mono"
                        }
                      >
                        {item?.version ||
                          (failed
                            ? "读取失败"
                            : item?.status === "missing"
                              ? "未安装"
                              : "读取中…")}
                      </strong>
                    </span>
                    <span>
                      最近检查
                      <strong className="font-mono">
                        {item?.checked_at ? clock(item.checked_at) : "等待检查"}
                      </strong>
                    </span>
                  </div>
                  {name === "phpmyadmin" &&
                    item?.status === "ready" &&
                    item.access_url &&
                    !components.error &&
                    !components.data?.error && (
                      <a
                        className="install-link mt-3"
                        href={item.access_url}
                        target="_blank"
                        rel="noreferrer"
                      >
                        打开 phpMyAdmin <ExternalLink size={14} />
                      </a>
                    )}
                  {name === "phpmyadmin" &&
                    item?.status === "ready" &&
                    !item.access_url && (
                      <p className="small-note mt-3">未找到已保存的访问地址</p>
                    )}
                  {item?.path && (
                    <p className="component-path font-mono" title={item.path}>
                      {item.path}
                    </p>
                  )}
                  {item?.error && (
                    <p className="service-version-error">
                      {item.error}
                      {item.version ? " · 显示上次成功结果" : ""}
                    </p>
                  )}
                  <ReleaseInfo
                    release={releases.data?.[name]}
                    failed={Boolean(releases.error)}
                  />
                </CardContent>
              </Card>
            );
          })}
        </div>
      </section>
      <div className="info-note" role="note">
        <AlertTriangle size={18} aria-hidden="true" />
        <div>
          <strong>服务操作会影响正在运行的网站</strong>
          <p>
            重载 Nginx 前会先检查配置。服务状态每 5
            秒刷新一次，操作完成后立即重新检查。最新稳定版从官方发布源异步查询，缓存
            6 小时；手动检查间隔至少 1 分钟。本机编程组件版本缓存 1
            分钟，手动刷新间隔至少 5 秒。
          </p>
        </div>
      </div>
      <Dialog
        open={selection !== null}
        onOpenChange={(open) => {
          if (!open && !mutation.isPending) setSelection(null);
        }}
      >
        <DialogContent
          showCloseButton={!mutation.isPending}
          onCloseAutoFocus={(event) => {
            if (
              actionTrigger.current?.isConnected &&
              !actionTrigger.current.disabled
            ) {
              event.preventDefault();
              actionTrigger.current.focus();
            }
          }}
          onEscapeKeyDown={(e) => {
            if (mutation.isPending) e.preventDefault();
          }}
          onInteractOutside={(e) => {
            if (mutation.isPending) e.preventDefault();
          }}
        >
          <DialogHeader>
            <DialogTitle>
              确认{selection && actions[selection.action].label}{" "}
              {selection && serviceInfo[selection.service.name]?.title}？
            </DialogTitle>
            <DialogDescription>
              {selection?.action === "reload"
                ? "将先执行 Nginx 配置检查，通过后才会重载服务。"
                : "该操作可能中断当前连接或网站访问，请确认操作时机。"}
              {selection?.service.name === "nginx" &&
                selection.action !== "reload" && (
                  <span className="block mt-3 text-amber-500">
                    如果面板通过此 Nginx
                    代理访问，停止或重启它会中断面板连接；停止后需要通过 SSH
                    启动 Nginx。
                  </span>
                )}
            </DialogDescription>
          </DialogHeader>
          <DialogFooter>
            <DialogClose asChild>
              <Button variant="outline" disabled={mutation.isPending}>
                取消
              </Button>
            </DialogClose>
            <Button
              disabled={mutation.isPending}
              variant={selection?.action === "stop" ? "destructive" : "default"}
              onClick={() => selection && mutation.mutate(selection)}
            >
              {mutation.isPending ? (
                <>
                  <Loader2 className="animate-spin" />
                  执行中…
                </>
              ) : (
                `确认${selection ? actions[selection.action].label : "操作"}`
              )}
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>
    </>
  );
}

const componentDefinitions = [
  { name: "phpmyadmin", title: "phpMyAdmin", subtitle: "数据库管理工具" },
  { name: "python3", title: "Python 3", subtitle: "Python 解释器" },
  { name: "go", title: "Go", subtitle: "Go 工具链" },
  { name: "node", title: "Node.js", subtitle: "JavaScript 运行时" },
  { name: "codex", title: "Codex CLI", subtitle: "OpenAI 编程工具" },
  { name: "composer", title: "Composer", subtitle: "PHP 依赖管理工具" },
];

function NginxBuildInfo({ service }: { service: Service }) {
  return (
    <Dialog>
      <DialogTrigger asChild>
        <Button variant="outline" size="sm" className="nginx-build-trigger">
          <Code2 size={14} />
          编译信息
        </Button>
      </DialogTrigger>
      <DialogContent className="nginx-build-dialog">
        <DialogHeader>
          <DialogTitle>Nginx 编译信息</DialogTitle>
          <DialogDescription>
            nginx -V · 版本、构建环境与编译参数
          </DialogDescription>
        </DialogHeader>
        {service.version_error && (
          <p className="service-version-error" role="alert">
            读取失败：{service.version_error}
            {service.version_output ? "（显示上次成功结果）" : ""}
          </p>
        )}
        {service.version_output ? (
          <pre className="nginx-build-output" tabIndex={0}>
            {service.version_output}
          </pre>
        ) : !service.version_error ? (
          <p className="small-note" role="status">
            尚未读取编译信息，请稍后刷新状态。
          </p>
        ) : null}
      </DialogContent>
    </Dialog>
  );
}

function ReleaseInfo({
  release,
  failed,
}: {
  release?: ServiceRelease;
  failed: boolean;
}) {
  return (
    <div className="service-release" aria-live="polite">
      <div className="service-release-heading">
        <span>最新稳定版</span>
        <strong className="font-mono">
          {release?.version ||
            (release?.status === "failed" || failed ? "暂不可用" : "查询中…")}
        </strong>
        {release?.status === "loading" && (
          <Loader2
            size={13}
            className="animate-spin"
            aria-label="正在查询最新版本"
          />
        )}
        {(release?.source_url || release?.changelog_url) && (
          <div className="service-release-links">
            {release?.source_url && (
              <a href={release.source_url} target="_blank" rel="noreferrer">
                官方发布 <ExternalLink size={12} />
              </a>
            )}
            {release?.changelog_url && (
              <a href={release.changelog_url} target="_blank" rel="noreferrer">
                Changelog 原文 <ExternalLink size={12} />
              </a>
            )}
          </div>
        )}
      </div>
      {release?.checked_at && (
        <p className="small-note">
          查询于{" "}
          {new Date(release.checked_at).toLocaleString("zh-CN", {
            hour12: false,
          })}
          {release.status === "failed" ? " · 显示上次成功结果" : ""}
        </p>
      )}
      {release?.error && (
        <p className="service-version-error">联网查询失败：{release.error}</p>
      )}
      {release?.changelog_error && (
        <p className="service-version-error">
          Changelog 读取失败：{release.changelog_error}
        </p>
      )}
      {release?.changelog && (
        <details className="service-changelog">
          <summary>查看 {release.version} 更新日志</summary>
          <pre>{release.changelog}</pre>
        </details>
      )}
    </div>
  );
}
