import { useState } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import {
  AlertTriangle,
  Check,
  CircuitBoard,
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
} from "@/components/ui/dialog";
import { ServiceIcon, serviceInfo, ServiceStatus } from "@/components/status";
import { api } from "@/lib/api";
import { clock } from "@/lib/format";
import type { Service, ServiceRelease } from "@/lib/types";
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
  const client = useQueryClient();
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
            disabled={
              checkingReleases || releases.isPending || !services?.length
            }
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
            disabled={loading}
            onClick={() =>
              void client.invalidateQueries({ queryKey: ["services"] })
            }
          >
            <RefreshCw size={14} className={loading ? "animate-spin" : ""} />
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
              {service.actions.length > 0 && (
                <div className="service-actions">
                  {service.actions.map((action) => {
                    const item = actions[action as Action];
                    if (!item) return null;
                    const Icon = item.icon;
                    const unavailable =
                      service.busy ||
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
                        onClick={() =>
                          setSelection({ service, action: action as Action })
                        }
                      >
                        <Icon size={13} />
                        {item.label}
                      </Button>
                    );
                  })}
                </div>
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
      <div className="info-note">
        <AlertTriangle size={17} />
        <div>
          <strong>服务操作会影响正在运行的网站</strong>
          <p>
            重载 Nginx 前会先检查配置。服务状态每 5
            秒刷新一次，操作完成后立即重新检查。最新稳定版从官方发布源异步查询，缓存
            6 小时；手动检查间隔至少 1 分钟。
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
        {release?.source_url && (
          <a href={release.source_url} target="_blank" rel="noreferrer">
            官方发布 <ExternalLink size={12} />
          </a>
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
      {release?.changelog_url && (
        <a
          className="service-changelog-link"
          href={release.changelog_url}
          target="_blank"
          rel="noreferrer"
        >
          Changelog 原文 <ExternalLink size={12} />
        </a>
      )}
    </div>
  );
}
