import { useEffect, useRef, useState } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import {
  Clock3,
  Database,
  FileSearch,
  FolderTree,
  HardDrive,
  Loader2,
  RefreshCw,
  Search,
} from "lucide-react";
import { Button } from "@/components/ui/button";
import { Badge } from "@/components/ui/badge";
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Switch } from "@/components/ui/switch";
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from "@/components/ui/table";
import { FileTreemap, fileColors } from "@/components/file-treemap";
import { Meter } from "@/pages/dashboard";
import { api } from "@/lib/api";
import { bytes, clock } from "@/lib/format";
import type { MountList, StorageScan } from "@/lib/types";

export function Storage() {
  const client = useQueryClient();
  const [filter, setFilter] = useState("");
  const [hideUnavailable, setHideUnavailable] = useState(false);
  const autoStarted = useRef(false);
  const mounts = useQuery({
    queryKey: ["storage-mounts"],
    queryFn: () => api<MountList>("/storage/mounts"),
    refetchInterval: 30000,
  });
  const scan = useQuery({
    queryKey: ["storage-scan"],
    queryFn: () => api<StorageScan>("/storage/scan"),
    refetchInterval: (query) =>
      query.state.data?.status === "running" ? 1000 : false,
  });
  const start = useMutation({
    mutationFn: () => api<StorageScan>("/storage/scan", { method: "POST" }),
    onSuccess: (job) => client.setQueryData(["storage-scan"], job),
  });
  useEffect(() => {
    if (scan.data?.status === "idle" && !autoStarted.current) {
      autoStarted.current = true;
      start.mutate();
    }
  }, [scan.data?.status, start.mutate]);
  const job = scan.data;
  const running = job?.status === "running" || start.isPending;
  const allMounts = mounts.data?.mounts ?? [];
  const root = allMounts.find((m) => m.path === "/");
  const shown = allMounts.filter(
    (m) =>
      (!hideUnavailable || (!m.error && m.total != null && m.total > 0)) &&
      `${m.path} ${m.device} ${m.fs_type}`
        .toLowerCase()
        .includes(filter.toLowerCase()),
  );
  const files = job?.files ?? [];
  const sum = files.reduce((total, file) => total + file.size, 0);
  const status = running
    ? "扫描中"
    : job?.status === "completed"
      ? "扫描完成"
      : job?.status === "partial"
        ? "部分结果"
        : job?.status === "failed"
          ? "扫描失败"
          : "等待扫描";
  return (
    <>
      <div className="page-heading">
        <div>
          <div className="eyebrow muted">STORAGE EXPLORER</div>
          <h1>硬盘空间</h1>
          <p>查看所有挂载，定位主磁盘中的大文件。</p>
        </div>
        <Button
          variant="outline"
          size="sm"
          disabled={mounts.isFetching}
          onClick={() => void mounts.refetch()}
        >
          <RefreshCw
            size={14}
            className={mounts.isFetching ? "animate-spin" : ""}
          />
          刷新挂载
        </Button>
      </div>
      <div className="storage-summary">
        {[
          {
            label: "主磁盘总容量",
            value: bytes(root?.total),
            detail: root?.device || "正在读取主文件系统",
            icon: HardDrive,
          },
          {
            label: "已使用空间",
            value: bytes(root?.used),
            detail:
              root?.percent == null
                ? "—"
                : `${root.percent.toFixed(1)}% 已使用`,
            icon: Database,
          },
          {
            label: "可用空间",
            value: bytes(root?.free),
            detail: `${allMounts.length} 个挂载 · 包含虚拟文件系统`,
            icon: FolderTree,
          },
        ].map((item) => (
          <Card key={item.label}>
            <CardContent>
              <div className="stat-label">
                {item.label}
                <item.icon size={17} />
              </div>
              <div className="storage-summary-value">{item.value}</div>
              <div className="small-note mt-3">{item.detail}</div>
            </CardContent>
          </Card>
        ))}
      </div>
      <Card className="mounts-card">
        <CardHeader className="storage-section-header">
          <div className="card-title-icon">
            <HardDrive size={16} />
            <CardTitle>所有挂载</CardTitle>
            <span className="count-label">
              {filter || hideUnavailable
                ? `${shown.length} / ${allMounts.length}`
                : allMounts.length}
            </span>
          </div>
          <div className="mount-controls">
            <div className="mount-capacity-toggle">
              <Switch
                id="hide-unavailable-mounts"
                checked={hideUnavailable}
                onCheckedChange={setHideUnavailable}
                aria-controls="storage-mounts-table"
              />
              <Label htmlFor="hide-unavailable-mounts">隐藏无容量挂载</Label>
            </div>
            <div className="mount-search">
              <Search size={13} />
              <Input
                aria-label="筛选挂载"
                placeholder="筛选挂载点、设备或类型…"
                value={filter}
                onChange={(event) => setFilter(event.target.value)}
              />
            </div>
          </div>
        </CardHeader>
        <CardContent className="table-content mounts-table">
          <Table id="storage-mounts-table" aria-label="所有挂载">
            <TableHeader>
              <TableRow>
                <TableHead>挂载点 / 设备</TableHead>
                <TableHead>文件系统</TableHead>
                <TableHead>已用 / 总容量</TableHead>
                <TableHead>可用空间</TableHead>
                <TableHead>使用率</TableHead>
              </TableRow>
            </TableHeader>
            <TableBody>
              {shown.map((m, i) => (
                <TableRow key={`${m.path}-${m.device}-${i}`}>
                  <TableCell>
                    <span className="font-mono mount-path">{m.path}</span>
                    {m.path === "/" && (
                      <Badge variant="outline" className="root-mount-badge">
                        主磁盘
                      </Badge>
                    )}
                    <div className="small-note mt-1 mount-device">
                      {m.device}
                    </div>
                  </TableCell>
                  <TableCell>
                    <span className="filesystem-label">{m.fs_type}</span>
                    {m.options.includes("ro") && (
                      <span className="small-note block mt-1">只读</span>
                    )}
                  </TableCell>
                  <TableCell className="font-mono text-xs">
                    {m.error ? (
                      <span className="text-muted-foreground">{m.error}</span>
                    ) : m.total === 0 ? (
                      <span className="text-muted-foreground">不提供容量</span>
                    ) : (
                      <>
                        {bytes(m.used)}{" "}
                        <span className="text-muted-foreground">
                          / {bytes(m.total)}
                        </span>
                      </>
                    )}
                  </TableCell>
                  <TableCell className="font-mono text-xs">
                    {m.total ? bytes(m.free) : "—"}
                  </TableCell>
                  <TableCell className="mount-meter">
                    {m.total && m.percent != null ? (
                      <>
                        <div className="disk-percent">
                          {m.percent.toFixed(1)}%
                        </div>
                        <Meter value={m.percent} />
                      </>
                    ) : (
                      <span className="text-muted-foreground">—</span>
                    )}
                  </TableCell>
                </TableRow>
              ))}
              {!shown.length && (
                <TableRow>
                  <TableCell colSpan={5} className="empty-cell">
                    {mounts.error
                      ? mounts.error.message
                      : mounts.isPending
                        ? "正在读取所有挂载…"
                        : filter
                          ? "没有匹配的挂载"
                          : hideUnavailable && allMounts.length > 0
                            ? "没有容量可读取的挂载"
                            : "暂无挂载数据"}
                  </TableCell>
                </TableRow>
              )}
            </TableBody>
          </Table>
        </CardContent>
        <div className="chart-foot">
          <span>
            {hideUnavailable
              ? "已隐藏不提供容量及容量不可读取的挂载"
              : "包含磁盘、内存、虚拟及绑定挂载"}
          </span>
          <span>
            {mounts.data
              ? `${clock(mounts.data.timestamp)} 更新 · 每 30 秒刷新`
              : "等待更新"}
          </span>
        </div>
      </Card>
      <Card className="file-ranking-card">
        <CardHeader className="storage-section-header">
          <div className="card-title-icon">
            <FileSearch size={17} />
            <CardTitle>主磁盘 · 大文件 Top 30</CardTitle>
            <Badge
              variant="outline"
              className={running ? "status-green" : "status-muted"}
            >
              {running && <Loader2 size={10} className="animate-spin" />}
              {status}
            </Badge>
          </div>
          <Button
            variant="outline"
            size="sm"
            disabled={running || scan.isPending}
            onClick={() => start.mutate()}
          >
            <RefreshCw size={13} className={running ? "animate-spin" : ""} />
            {running ? "正在扫描…" : "重新扫描"}
          </Button>
        </CardHeader>
        <CardContent className="ranking-content">
          <p className="ranking-description">
            扫描根目录 /
            所在文件系统，按单个普通文件的逻辑大小排序。其他挂载点不参与排行，结果仅保留在内存中。
          </p>
          {(scan.error ||
            start.error ||
            job?.status === "failed" ||
            job?.status === "partial") && (
            <div className="notice" role="alert">
              {start.error?.message || scan.error?.message || job?.message}
            </div>
          )}
          {running && (
            <div className="scan-progress" role="status">
              <div className="scan-progress-line">
                <span>
                  <Loader2 size={13} className="animate-spin" />
                  正在扫描文件，完成后自动更新
                </span>
                <span>
                  已读取 {(job?.scanned ?? 0).toLocaleString()} 个文件
                </span>
              </div>
              <div className="scan-progress-track">
                <span />
              </div>
            </div>
          )}
          <div className="ranking-meta">
            <span>
              {files.length} 个大文件 · 合计 <strong>{bytes(sum)}</strong>
            </span>
            <span>
              {job?.finished_at ? (
                <>
                  <Clock3 size={12} />
                  {clock(job.finished_at)} 完成
                </>
              ) : job?.started_at ? (
                `${clock(job.started_at)} 开始`
              ) : (
                "异步加载，不影响实时监控"
              )}
            </span>
          </div>
          {files.length > 0 ? (
            <div className="ranking-grid">
              <FileTreemap files={files} />
              <div className="file-ranking-table">
                <Table>
                  <TableHeader>
                    <TableRow>
                      <TableHead>#</TableHead>
                      <TableHead>文件路径</TableHead>
                      <TableHead className="text-right">
                        文件大小 / 占比
                      </TableHead>
                    </TableRow>
                  </TableHeader>
                  <TableBody>
                    {files.map((file, i) => (
                      <TableRow key={file.path}>
                        <TableCell className="file-rank">
                          <i
                            style={{
                              background: fileColors[i % fileColors.length],
                            }}
                          />
                          {i + 1}
                        </TableCell>
                        <TableCell>
                          <span className="large-file-path" title={file.path}>
                            {file.path}
                          </span>
                        </TableCell>
                        <TableCell className="file-size">
                          <strong>{bytes(file.size)}</strong>
                          <span>
                            {sum
                              ? ((file.size / sum) * 100).toFixed(2)
                              : "0.00"}
                            %
                          </span>
                        </TableCell>
                      </TableRow>
                    ))}
                  </TableBody>
                </Table>
              </div>
            </div>
          ) : (
            <div className="scan-empty">
              <FileSearch size={35} className={running ? "text-primary" : ""} />
              <strong>
                {running
                  ? "正在寻找主磁盘上的大文件…"
                  : job?.status === "failed"
                    ? "文件排行暂不可用"
                    : "暂无文件排行"}
              </strong>
              <p>
                {running
                  ? "首次扫描可能需要一些时间，你可以继续查看挂载信息或切换页面。"
                  : "点击重新扫描以获取最新的文件大小排行。"}
              </p>
            </div>
          )}
        </CardContent>
      </Card>
    </>
  );
}
