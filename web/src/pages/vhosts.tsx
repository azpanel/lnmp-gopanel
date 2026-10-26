import { useEffect, useRef, useState } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import {
  AlertTriangle,
  Check,
  Code2,
  FolderOpen,
  Globe2,
  HardDrive,
  LayoutGrid,
  Loader2,
  LockKeyhole,
  Power,
  RefreshCw,
  Search,
  ShieldCheck,
  ShieldX,
  Table2,
} from "lucide-react";
import { toast } from "sonner";
import { Button } from "@/components/ui/button";
import { Badge } from "@/components/ui/badge";
import { Card, CardContent } from "@/components/ui/card";
import { Input } from "@/components/ui/input";
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from "@/components/ui/table";
import {
  Dialog,
  DialogClose,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog";
import { api } from "@/lib/api";
import { bytes } from "@/lib/format";
import type {
  SiteCertificate,
  Vhost,
  VhostConfig,
  VhostSnapshot,
} from "@/lib/types";
import { cn } from "@/lib/utils";
import { useWebsiteScreenshots } from "@/components/website-screenshot";

const certificateLabels = {
  valid: "有效",
  expiring: "即将到期",
  expired: "已过期",
  not_yet_valid: "尚未生效",
  unavailable: "不可读取",
};
function date(value: string | null) {
  return value
    ? new Date(value).toLocaleString("zh-CN", { hour12: false })
    : "—";
}
function CertificateStatus({ certificate }: { certificate: SiteCertificate }) {
  return (
    <Badge
      variant="outline"
      className={cn(
        "status-badge",
        certificate.status === "valid"
          ? "status-green"
          : certificate.status === "expired" ||
              certificate.status === "unavailable"
            ? "status-red"
            : "certificate-warning",
      )}
    >
      {certificateLabels[certificate.status]}
      {certificate.status === "valid" || certificate.status === "expiring"
        ? ` · 剩余 ${certificate.days_remaining} 天`
        : ""}
    </Badge>
  );
}
function SiteStatus({ enabled }: { enabled: boolean }) {
  return (
    <Badge
      variant="outline"
      className={cn("status-badge", enabled ? "status-green" : "status-muted")}
    >
      <span className="status-dot" />
      {enabled ? "已启用" : "已停用"}
    </Badge>
  );
}
function Certificate({ certificate }: { certificate: SiteCertificate }) {
  return (
    <div className="site-certificate">
      <div className="site-certificate-heading">
        <span>
          <LockKeyhole size={13} />
          SSL 证书
        </span>
        <CertificateStatus certificate={certificate} />
      </div>
      {certificate.error ? (
        <p className="site-detail-error">{certificate.error}</p>
      ) : (
        <dl className="certificate-fields">
          <div>
            <dt>签发者</dt>
            <dd>{certificate.issuer}</dd>
          </div>
          <div>
            <dt>生效时间</dt>
            <dd>{date(certificate.not_before)}</dd>
          </div>
          <div>
            <dt>到期时间</dt>
            <dd>{date(certificate.not_after)}</dd>
          </div>
          <div>
            <dt>证书域名</dt>
            <dd>{certificate.dns_names.join("、") || certificate.subject}</dd>
          </div>
        </dl>
      )}
      <details className="certificate-more">
        <summary>证书详情</summary>
        <dl className="certificate-fields">
          <div>
            <dt>文件</dt>
            <dd className="font-mono">{certificate.path}</dd>
          </div>
          <div>
            <dt>主体</dt>
            <dd>{certificate.subject || "—"}</dd>
          </div>
          <div>
            <dt>序列号</dt>
            <dd className="font-mono">{certificate.serial || "—"}</dd>
          </div>
          <div>
            <dt>SHA-256</dt>
            <dd className="font-mono">{certificate.fingerprint || "—"}</dd>
          </div>
        </dl>
      </details>
    </div>
  );
}
function SiteDirectory({
  site,
  usage: inventory,
  loading,
}: {
  site: Vhost;
  usage?: VhostSnapshot["usage"];
  loading: boolean;
}) {
  return (
    <div className="vhost-directory-panel">
      <div className="site-section-label">
        <HardDrive size={14} />
        站点目录占用
      </div>
      {site.roots.length ? (
        site.roots.map((root) => {
          const usage = inventory?.[root.path];
          return (
            <div className="site-root" key={root.configured}>
              <div className="site-root-size">
                {root.error || usage?.error ? (
                  <span className="site-detail-error">
                    {root.error || usage?.error}
                  </span>
                ) : usage?.bytes != null ? (
                  <strong>{bytes(usage.bytes)}</strong>
                ) : loading ? (
                  <span>
                    <Loader2 size={13} className="animate-spin" />
                    正在计算…
                  </span>
                ) : (
                  <span className="small-note">等待刷新统计</span>
                )}
              </div>
              <code>{root.path || root.configured}</code>
              {root.public_parent && (
                <div className="public-root-note">
                  入口 {root.configured}，统计上一级项目目录
                </div>
              )}
            </div>
          );
        })
      ) : (
        <div className="site-no-root">
          <FolderOpen size={25} />
          <span>
            {site.proxies.length
              ? "反向代理，无本地站点目录"
              : "未配置可统计的站点根目录"}
          </span>
        </div>
      )}
      {site.proxies.length > 0 && (
        <div className="site-upstreams">
          <span>反向代理</span>
          {site.proxies.map((proxy) => (
            <code key={proxy}>{proxy}</code>
          ))}
        </div>
      )}
      <div className="site-space-note">
        按实际磁盘块统计；public 入口统计其上一级目录。
      </div>
    </div>
  );
}
export function Vhosts() {
  const client = useQueryClient(),
    autoStarted = useRef(false);
  const [filter, setFilter] = useState("");
  const [view, setView] = useState<"cards" | "table">("cards");
  const [configSite, setConfigSite] = useState<Vhost | null>(null);
  const [selection, setSelection] = useState<Vhost | null>(null);
  const snapshot = useQuery({
    queryKey: ["vhosts"],
    queryFn: () => api<VhostSnapshot>("/vhosts"),
    refetchInterval: (query) =>
      query.state.data?.status === "loading" ||
      query.state.data?.status === "scanning"
        ? 1000
        : false,
  });
  const refresh = useMutation({
    mutationFn: () => api<VhostSnapshot>("/vhosts/refresh", { method: "POST" }),
    onSuccess: (data) => client.setQueryData(["vhosts"], data),
  });
  const configuration = useQuery({
    queryKey: ["vhost-config", configSite?.name],
    queryFn: () =>
      api<VhostConfig>(
        `/vhosts/config?name=${encodeURIComponent(configSite!.name)}`,
      ),
    enabled: Boolean(configSite),
    staleTime: 0,
  });
  const toggle = useMutation({
    mutationFn: (site: Vhost) =>
      api<{ message: string }>("/vhosts/toggle", {
        method: "POST",
        body: JSON.stringify({ name: site.name, enabled: !site.enabled }),
      }),
    onSuccess: (result) => {
      toast.success(result.message);
      setSelection(null);
      void client.invalidateQueries({ queryKey: ["vhosts"] });
      void client.invalidateQueries({ queryKey: ["services"] });
      void client.invalidateQueries({ queryKey: ["vhost-config"] });
    },
    onError: (error) => toast.error(error.message),
  });
  useEffect(() => {
    if (snapshot.data?.status === "idle" && !autoStarted.current) {
      autoStarted.current = true;
      refresh.mutate();
    }
  }, [snapshot.data?.status, refresh.mutate]);
  const data = snapshot.data;
  const all = data?.sites ?? [];
  const knownCount =
    data?.status === "ready" || data?.status === "scanning" ? all.length : null;
  useEffect(() => {
    if (knownCount != null)
      client.setQueryData(["vhost-count"], { count: knownCount });
  }, [client, knownCount]);
  const screenshots = useWebsiteScreenshots(all.length > 0);
  const sites = all.filter((site) =>
    `${site.name} ${site.domains.join(" ")} ${site.roots.map((root) => root.path).join(" ")}`
      .toLowerCase()
      .includes(filter.toLowerCase()),
  );
  const loading =
    snapshot.isPending || refresh.isPending || data?.status === "loading";
  const scanning = data?.status === "scanning";
  const certificateCount = all.filter(
    (site) => site.certificates.length > 0,
  ).length;
  const issues = all.filter((site) =>
    site.certificates.some((c) => c.status !== "valid"),
  ).length;
  const renderActions = (site: Vhost) => (
    <div className="vhost-actions">
      {screenshots.settings(site)}
      <Button variant="outline" size="sm" onClick={() => setConfigSite(site)}>
        <Code2 size={13} />
        查看配置
      </Button>
      <Button
        variant={site.enabled ? "outline" : "default"}
        size="sm"
        disabled={toggle.isPending}
        onClick={() => {
          toggle.reset();
          setSelection(site);
        }}
      >
        <Power size={13} />
        {site.enabled ? "停用" : "启用"}
      </Button>
    </div>
  );
  return (
    <>
      <div className="page-heading">
        <div>
          <div className="eyebrow muted">VIRTUAL HOSTS</div>
          <h1>虚拟主机</h1>
          <p>查看站点配置、SSL 证书及网站目录空间。</p>
        </div>
        <Button
          variant="outline"
          size="sm"
          disabled={loading || scanning}
          onClick={() => refresh.mutate()}
        >
          <RefreshCw
            size={14}
            className={loading || scanning ? "animate-spin" : ""}
          />
          {scanning ? "统计目录中…" : loading ? "读取中…" : "刷新站点"}
        </Button>
      </div>
      <div className="vhost-summary">
        {[
          {
            label: "站点配置",
            value: all.length,
            icon: Globe2,
            detail: `${all.filter((site) => site.enabled).length} 个已启用`,
          },
          {
            label: "SSL 站点",
            value: certificateCount,
            icon: ShieldCheck,
            detail: "从本地配置的证书文件读取",
          },
          {
            label: "证书需关注",
            value: issues,
            icon: issues ? ShieldX : Check,
            detail: "即将到期、已过期或不可读取",
          },
        ].map((item) => (
          <Card key={item.label}>
            <CardContent>
              <div className="stat-label">
                {item.label}
                <item.icon size={17} />
              </div>
              <div className="storage-summary-value">
                {item.value}
                <span className="vhost-stat-unit">个</span>
              </div>
              <div className="small-note mt-3">{item.detail}</div>
            </CardContent>
          </Card>
        ))}
      </div>
      <div className="vhost-toolbar">
        <span className="vhost-config-directory">
          <FolderOpen size={14} />
          /usr/local/nginx/conf/vhost
        </span>
        <div className="vhost-toolbar-controls">
          <div className="mount-search">
            <Search size={13} />
            <Input
              aria-label="筛选虚拟主机"
              placeholder="筛选域名、配置或目录…"
              value={filter}
              onChange={(event) => setFilter(event.target.value)}
            />
          </div>
          <div
            className="view-switch"
            role="group"
            aria-label="虚拟主机展示方式"
          >
            <Button
              variant={view === "cards" ? "secondary" : "ghost"}
              size="sm"
              aria-label="卡片视图"
              aria-pressed={view === "cards"}
              onClick={() => setView("cards")}
            >
              <LayoutGrid size={14} />
              卡片
            </Button>
            <Button
              variant={view === "table" ? "secondary" : "ghost"}
              size="sm"
              aria-label="表格视图"
              aria-pressed={view === "table"}
              onClick={() => setView("table")}
            >
              <Table2 size={14} />
              表格
            </Button>
          </div>
        </div>
      </div>
      {(snapshot.error || refresh.error || data?.error) && (
        <div className="notice" role="alert">
          {snapshot.error?.message || refresh.error?.message || data?.error}
        </div>
      )}
      {screenshots.notice && (
        <div className="notice screenshot-service-notice">
          网站截图：{screenshots.notice}
        </div>
      )}
      {scanning && (
        <div className="vhost-scan-note">
          <Loader2 size={13} className="animate-spin" />
          站点已加载，正在后台计算目录占用…
        </div>
      )}
      {view === "table" && sites.length > 0 && (
        <Card className="vhost-table-card">
          <CardContent className="table-content">
            <Table className="vhost-table" aria-label="虚拟主机列表">
              <TableHeader>
                <TableRow>
                  <TableHead>站点 / 配置</TableHead>
                  <TableHead>网站画面</TableHead>
                  <TableHead>状态</TableHead>
                  <TableHead>监听</TableHead>
                  <TableHead>站点目录 / 占用</TableHead>
                  <TableHead>SSL 证书</TableHead>
                  <TableHead>操作</TableHead>
                </TableRow>
              </TableHeader>
              <TableBody>
                {sites.map((site) => (
                  <TableRow key={site.name}>
                    <TableCell>
                      <strong className="vhost-table-name">
                        {site.domains[0] || site.name}
                      </strong>
                      <div className="small-note font-mono mt-1">
                        {site.filename}
                      </div>
                      {site.domains.length > 1 && (
                        <div className="vhost-table-domains">
                          {site.domains.slice(1).join(" · ")}
                        </div>
                      )}
                      {site.error && (
                        <p className="site-detail-error mt-2">{site.error}</p>
                      )}
                      {site.warnings.length > 0 && (
                        <div className="site-warnings">
                          <AlertTriangle size={12} />
                          {site.warnings.join("；")}
                        </div>
                      )}
                    </TableCell>
                    <TableCell>{screenshots.preview(site)}</TableCell>
                    <TableCell>
                      <SiteStatus enabled={site.enabled} />
                    </TableCell>
                    <TableCell className="font-mono text-xs">
                      {site.listens.join(" · ") || "—"}
                    </TableCell>
                    <TableCell>
                      <SiteDirectory
                        site={site}
                        usage={data?.usage}
                        loading={loading || scanning}
                      />
                    </TableCell>
                    <TableCell>
                      <div className="vhost-table-certificates">
                        {site.certificates.length ? (
                          site.certificates.map((certificate) => (
                            <details key={certificate.path}>
                              <summary>
                                <CertificateStatus certificate={certificate} />
                                <span>证书详情</span>
                              </summary>
                              <Certificate certificate={certificate} />
                            </details>
                          ))
                        ) : (
                          <span className="small-note">未配置 SSL 证书</span>
                        )}
                      </div>
                    </TableCell>
                    <TableCell>{renderActions(site)}</TableCell>
                  </TableRow>
                ))}
              </TableBody>
            </Table>
          </CardContent>
        </Card>
      )}
      <div className="vhost-list">
        {view === "cards" &&
          sites.map((site) => (
            <Card className="vhost-card" key={site.name}>
              <CardContent>
                <div className="vhost-card-heading">
                  <div className="vhost-icon">
                    <Globe2 size={21} />
                  </div>
                  <div className="vhost-title">
                    <h2>{site.domains[0] || site.name}</h2>
                    <span>{site.filename}</span>
                  </div>
                  <SiteStatus enabled={site.enabled} />
                  {renderActions(site)}
                </div>
                {site.error && <div className="notice mt-4">{site.error}</div>}
                <div className="vhost-domains">
                  <span>域名</span>
                  {site.domains.length ? (
                    site.domains.map((domain) => (
                      <span className="domain-chip" key={domain}>
                        {domain}
                      </span>
                    ))
                  ) : (
                    <span className="small-note">未解析到 server_name</span>
                  )}
                  <div className="vhost-listens">
                    监听 {site.listens.join(" · ") || "—"}
                  </div>
                </div>
                <div className="vhost-details">
                  {screenshots.preview(site)}
                  <SiteDirectory
                    site={site}
                    usage={data?.usage}
                    loading={loading || scanning}
                  />
                  <div className="vhost-certificates">
                    {site.certificates.length ? (
                      site.certificates.map((certificate) => (
                        <Certificate
                          key={certificate.path}
                          certificate={certificate}
                        />
                      ))
                    ) : (
                      <div className="site-no-certificate">
                        <LockKeyhole size={25} />
                        <span>未配置 SSL 证书</span>
                      </div>
                    )}
                  </div>
                </div>
                {site.warnings.length > 0 && (
                  <div className="site-warnings">
                    <AlertTriangle size={12} />
                    {site.warnings.join("；")}
                  </div>
                )}
              </CardContent>
            </Card>
          ))}
      </div>
      {!sites.length && (
        <Card>
          <CardContent className="large-empty">
            <Globe2 size={38} />
            <h2>
              {loading
                ? "正在读取虚拟主机…"
                : filter
                  ? "没有匹配的站点"
                  : "暂无站点配置"}
            </h2>
            <p>
              {loading
                ? "站点元数据和目录大小将分开加载。"
                : "页面展示 vhost 目录中的 .conf 与 .conf.disabled 文件。"}
            </p>
          </CardContent>
        </Card>
      )}
      <div className="overview-foot">
        <span>
          <span className="tiny-dot" />
          配置后缀表示启停状态
        </span>
        <span>
          {data?.updated_at
            ? `${date(data.updated_at)} 更新`
            : "目录占用与证书信息仅在内存中缓存"}
        </span>
      </div>
      {screenshots.dialogs}
      <Dialog
        open={configSite !== null}
        onOpenChange={(open) => {
          if (!open) setConfigSite(null);
        }}
      >
        <DialogContent className="vhost-config-dialog">
          <DialogHeader>
            <DialogTitle>
              站点配置 · {configuration.data?.filename || configSite?.filename}
            </DialogTitle>
            <DialogDescription>只读查看当前配置文件内容。</DialogDescription>
          </DialogHeader>
          {configuration.isPending ? (
            <div className="config-loading">
              <Loader2 className="animate-spin" />
              正在读取配置…
            </div>
          ) : configuration.error ? (
            <div className="notice" role="alert">
              {configuration.error.message}
            </div>
          ) : (
            <pre className="vhost-config-source">
              <code>{configuration.data?.content}</code>
            </pre>
          )}
        </DialogContent>
      </Dialog>
      <Dialog
        open={selection !== null}
        onOpenChange={(open) => {
          if (!open && !toggle.isPending) setSelection(null);
        }}
      >
        <DialogContent
          showCloseButton={!toggle.isPending}
          onEscapeKeyDown={(event) => {
            if (toggle.isPending) event.preventDefault();
          }}
          onInteractOutside={(event) => {
            if (toggle.isPending) event.preventDefault();
          }}
        >
          <DialogHeader>
            <DialogTitle>
              确认{selection?.enabled ? "停用" : "启用"}{" "}
              {selection?.domains[0] || selection?.name}？
            </DialogTitle>
            <DialogDescription>
              {selection?.enabled
                ? "停用后，此配置提供的网站将无法通过该 Nginx 配置访问。"
                : "启用后，Nginx 将加载该站点配置。"}
              <span className="block mt-3 font-mono break-all">
                {selection?.filename} →{" "}
                {selection?.enabled
                  ? `${selection.name}.disabled`
                  : selection?.name}
              </span>
              <span className="block mt-3">
                重命名后检查配置并重载 Nginx；失败时恢复原文件名。
              </span>
            </DialogDescription>
          </DialogHeader>
          {toggle.error && (
            <div className="notice" role="alert">
              {toggle.error.message}
            </div>
          )}
          <DialogFooter>
            <DialogClose asChild>
              <Button variant="outline" disabled={toggle.isPending}>
                取消
              </Button>
            </DialogClose>
            <Button
              variant={selection?.enabled ? "destructive" : "default"}
              disabled={toggle.isPending}
              onClick={() => selection && toggle.mutate(selection)}
            >
              {toggle.isPending ? (
                <>
                  <Loader2 className="animate-spin" />
                  正在检查并重载…
                </>
              ) : (
                `确认${selection?.enabled ? "停用" : "启用"}`
              )}
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>
    </>
  );
}
