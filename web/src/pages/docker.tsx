import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { Container, HardDrive, Loader2, RefreshCw } from "lucide-react";
import { Button } from "@/components/ui/button";
import { Badge } from "@/components/ui/badge";
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from "@/components/ui/table";
import { api } from "@/lib/api";
import type { DockerSnapshot } from "@/lib/types";

const diskTypes: Record<string, string> = {
  Images: "镜像",
  Containers: "容器可写层",
  "Local Volumes": "本地数据卷",
  "Build Cache": "构建缓存",
};

export function Docker() {
  const client = useQueryClient();
  const query = useQuery({
    queryKey: ["docker"],
    queryFn: () => api<DockerSnapshot>("/docker"),
    refetchInterval: (query) =>
      query.state.data?.status === "loading" ? 1000 : 30000,
  });
  const refresh = useMutation({
    mutationFn: () =>
      api<DockerSnapshot>("/docker/refresh", { method: "POST" }),
    onSuccess: (data) => client.setQueryData(["docker"], data),
  });
  const data = query.data;
  const loading =
    query.isPending || data?.status === "loading" || refresh.isPending;
  const containers = data?.containers ?? [];
  const diskUsage = data?.disk_usage ?? [];
  const unavailable = data?.status === "unavailable";
  const failed = data?.status === "failed";
  return (
    <>
      <div className="page-heading">
        <div>
          <div className="eyebrow muted">DOCKER OVERVIEW</div>
          <h1>Docker 管理</h1>
          <p>查看运行中的容器与 Docker 磁盘空间使用情况。</p>
        </div>
        <Button
          variant="outline"
          size="sm"
          disabled={loading}
          onClick={() => refresh.mutate()}
        >
          <RefreshCw size={14} className={loading ? "animate-spin" : ""} />
          {loading ? "读取中…" : "刷新"}
        </Button>
      </div>
      {(query.error || refresh.error) && (
        <div className="notice" role="alert">
          Docker 信息读取失败：{query.error?.message || refresh.error?.message}
          。已有数据可能已过期。
        </div>
      )}
      {data?.message && (
        <div className="notice" role="status">
          {data.message}
          {failed && data.updated_at ? "，下方保留上次成功读取的数据。" : ""}
        </div>
      )}
      {Object.entries(data?.errors ?? {}).map(([key, error]) => (
        <div className="notice" role="alert" key={key}>
          {error}。{data?.updated_at ? "对应数据可能已过期。" : ""}
        </div>
      ))}
      {unavailable ? (
        <Card>
          <CardContent className="large-empty">
            <Container size={40} />
            <h2>本机未安装 Docker</h2>
            <p>安装并启动 Docker 后，刷新此页即可查看。</p>
          </CardContent>
        </Card>
      ) : (
        <>
          <Card className="docker-space-card">
            <CardHeader className="storage-section-header">
              <CardTitle className="card-title-icon">
                <HardDrive size={17} />
                空间占用
              </CardTitle>
              <span className="small-note">镜像、容器、数据卷与构建缓存</span>
            </CardHeader>
            <CardContent className="table-content">
              <Table aria-label="Docker 空间占用">
                <TableHeader>
                  <TableRow>
                    <TableHead>类型</TableHead>
                    <TableHead>总数</TableHead>
                    <TableHead>使用中</TableHead>
                    <TableHead>占用空间</TableHead>
                    <TableHead>可回收空间</TableHead>
                  </TableRow>
                </TableHeader>
                <TableBody>
                  {diskUsage.map((item) => (
                    <TableRow key={item.type}>
                      <TableCell>{diskTypes[item.type] || item.type}</TableCell>
                      <TableCell className="font-mono">{item.total}</TableCell>
                      <TableCell className="font-mono">{item.active}</TableCell>
                      <TableCell className="font-mono">{item.size}</TableCell>
                      <TableCell className="font-mono text-muted-foreground">
                        {item.reclaimable || "—"}
                      </TableCell>
                    </TableRow>
                  ))}
                  {!diskUsage.length && (
                    <TableRow>
                      <TableCell colSpan={5} className="empty-cell">
                        {loading
                          ? "正在统计 Docker 空间…"
                          : "暂无可用的空间统计"}
                      </TableCell>
                    </TableRow>
                  )}
                </TableBody>
              </Table>
            </CardContent>
          </Card>
          <Card>
            <CardHeader className="storage-section-header">
              <CardTitle className="card-title-icon">
                <Container size={17} />
                运行中的容器{" "}
                <span className="count-label">
                  {data?.containers ? containers.length : "—"}
                </span>
              </CardTitle>
              <span className="small-note">每 30 秒自动刷新</span>
            </CardHeader>
            <CardContent className="table-content">
              <Table
                className="docker-containers-table"
                aria-label="运行中的 Docker 容器"
              >
                <TableHeader>
                  <TableRow>
                    <TableHead>容器 / ID</TableHead>
                    <TableHead>镜像</TableHead>
                    <TableHead>启动命令</TableHead>
                    <TableHead>创建时间</TableHead>
                    <TableHead>状态</TableHead>
                    <TableHead>端口</TableHead>
                    <TableHead>可写层 / 虚拟大小</TableHead>
                  </TableRow>
                </TableHeader>
                <TableBody>
                  {containers.map((item) => {
                    const size = item.size.match(
                      /^(.*?)\s*\(virtual\s+(.*?)\)$/,
                    );
                    return (
                      <TableRow key={item.id}>
                        <TableCell>
                          <strong className="docker-container-name">
                            {item.name}
                          </strong>
                          <span
                            className="docker-secondary font-mono"
                            title={item.id}
                          >
                            {item.id.slice(0, 12)}
                          </span>
                        </TableCell>
                        <TableCell>
                          <span className="docker-image font-mono">
                            {item.image}
                          </span>
                        </TableCell>
                        <TableCell>
                          <code className="docker-command">
                            {item.command || "—"}
                          </code>
                        </TableCell>
                        <TableCell>
                          <span className="docker-created">
                            {item.created_at || "—"}
                          </span>
                          <span className="docker-secondary">
                            {item.running_for}
                          </span>
                        </TableCell>
                        <TableCell>
                          <Badge
                            variant="outline"
                            className={
                              item.state === "running"
                                ? "status-badge status-green"
                                : "status-badge status-muted"
                            }
                          >
                            {item.status || item.state}
                          </Badge>
                        </TableCell>
                        <TableCell>
                          <span className="docker-ports font-mono">
                            {item.ports || "—"}
                          </span>
                        </TableCell>
                        <TableCell className="font-mono">
                          {size ? (
                            <>
                              {size[1]}
                              <span className="docker-secondary">
                                虚拟 {size[2]}
                              </span>
                            </>
                          ) : (
                            item.size || "—"
                          )}
                        </TableCell>
                      </TableRow>
                    );
                  })}
                  {!containers.length && (
                    <TableRow>
                      <TableCell colSpan={7} className="empty-cell">
                        {loading ? (
                          <span className="docker-loading">
                            <Loader2 size={16} className="animate-spin" />
                            正在读取容器…
                          </span>
                        ) : failed || data?.errors.containers || query.error ? (
                          "容器列表暂不可用"
                        ) : (
                          "当前没有运行中的容器"
                        )}
                      </TableCell>
                    </TableRow>
                  )}
                </TableBody>
              </Table>
            </CardContent>
          </Card>
          <div className="docker-footnote">
            <p>
              可写层不含容器日志和挂载目录；数据卷单独汇总。虚拟大小包含共享镜像层，不能按容器直接相加。
            </p>
            {data?.updated_at && (
              <span>
                最近采集：
                {new Date(data.updated_at).toLocaleString("zh-CN", {
                  hour12: false,
                })}
              </span>
            )}
          </div>
        </>
      )}
    </>
  );
}
