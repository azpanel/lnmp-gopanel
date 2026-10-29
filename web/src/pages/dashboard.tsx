import { Link } from "react-router-dom";
import { LNMPInstallNotice } from "@/components/lnmp-install-notice";
import {
  Activity,
  ArrowDownLeft,
  ArrowRight,
  ArrowUpRight,
  CircuitBoard,
  Clock3,
  Cpu,
  HardDrive,
  MemoryStick,
  Network,
} from "lucide-react";
import type { ReactNode } from "react";
import { Button } from "@/components/ui/button";
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from "@/components/ui/table";
import { Meter } from "@/components/meter";
import { MetricChart } from "@/components/metric-chart";
import { ServiceIcon, serviceInfo, ServiceStatus } from "@/components/status";
import { bytes, clock, percent, uptime } from "@/lib/format";
import type { Host, Service, Snapshot } from "@/lib/types";
import type { Monitor } from "@/lib/use-monitor";

function Stat({
  label,
  icon,
  value,
  unit,
  detail,
  children,
}: {
  label: string;
  icon: ReactNode;
  value: string;
  unit?: string;
  detail: ReactNode;
  children?: ReactNode;
}) {
  return (
    <Card className="stat-card">
      <CardContent>
        <div className="stat-label">
          {label}
          <span className="stat-icon" aria-hidden="true">
            {icon}
          </span>
        </div>
        <div className="stat-value">
          {value}
          <span>{unit}</span>
        </div>
        <div className="stat-detail">{detail}</div>
        {children}
      </CardContent>
    </Card>
  );
}
export function DiskTable({ snapshot }: { snapshot?: Snapshot }) {
  return (
    <Card>
      <CardHeader className="section-card-header">
        <div className="card-title-icon">
          <HardDrive size={16} />
          <CardTitle>磁盘空间</CardTitle>
        </div>
        <span className="small-note">每 30 秒更新</span>
      </CardHeader>
      <CardContent className="table-content">
        <Table>
          <TableHeader>
            <TableRow>
              <TableHead>挂载点 / 设备</TableHead>
              <TableHead>已用 / 总容量</TableHead>
              <TableHead className="w-[30%]">使用率</TableHead>
            </TableRow>
          </TableHeader>
          <TableBody>
            {snapshot?.disks.map((d) => (
              <TableRow key={d.path}>
                <TableCell>
                  <span className="font-mono text-foreground">{d.path}</span>
                  <div className="small-note mt-1">{d.device}</div>
                </TableCell>
                <TableCell className="font-mono text-xs">
                  {bytes(d.used)}{" "}
                  <span className="text-muted-foreground">
                    / {bytes(d.total)}
                  </span>
                </TableCell>
                <TableCell>
                  <div className="disk-percent">{d.percent.toFixed(1)}%</div>
                  <Meter value={d.percent} />
                </TableCell>
              </TableRow>
            ))}
            {!snapshot?.disks.length && (
              <TableRow>
                <TableCell colSpan={3} className="empty-cell">
                  {snapshot?.errors.disks || "暂无可用的磁盘容量数据"}
                </TableCell>
              </TableRow>
            )}
          </TableBody>
        </Table>
      </CardContent>
    </Card>
  );
}
export function Dashboard({
  monitor,
  host,
  services,
  serviceError,
}: {
  monitor: Monitor;
  host?: Host;
  services?: Service[];
  serviceError?: string;
}) {
  const s = monitor.latest;
  const load = s?.load;
  const healthy = services?.filter((v) => v.state === "running").length ?? 0;
  const errors = Object.values(s?.errors || {});
  return (
    <>
      <LNMPInstallNotice />
      <div className="page-heading">
        <div>
          <div className="eyebrow muted">SERVER OVERVIEW</div>
          <h1>系统概览</h1>
          <p>掌握服务器的每一次心跳。</p>
        </div>
        <div className="heading-time">
          <Clock3 size={14} />
          <span>{s ? clock(s.timestamp) : "等待采集"}</span>
          <span className="small-note">最后更新</span>
        </div>
      </div>
      {errors.length > 0 && (
        <div className="notice" role="status">
          部分指标不可用：{errors.join("、")}
        </div>
      )}
      {monitor.stale && s && (
        <div className="notice">
          {monitor.paused
            ? "实时更新已暂停，当前显示暂停前的数据。"
            : "实时数据已中断，当前数值可能已过期。连接恢复后自动更新。"}
        </div>
      )}
      <div className="stats-grid">
        <Stat
          label="CPU 使用率"
          icon={<Cpu size={17} />}
          value={percent(s?.cpu)}
          unit="%"
          detail={
            s?.cpu == null
              ? s?.errors.cpu || "正在计算使用率…"
              : `${host?.cores ?? "—"} 核心 · ${host?.arch ?? "—"}`
          }
        >
          <Meter value={s?.cpu} />
        </Stat>
        <Stat
          label="内存使用"
          icon={<MemoryStick size={17} />}
          value={percent(s?.memory?.percent)}
          unit="%"
          detail={`${bytes(s?.memory?.used)} / ${bytes(s?.memory?.total)}`}
        >
          <Meter value={s?.memory?.percent} />
        </Stat>
        <Stat
          label="网络接收 / 发送"
          icon={<Network size={17} />}
          value={s?.network ? bytes(s.network.read) : "—"}
          unit="/s"
          detail={
            <>
              <ArrowUpRight size={13} className="text-violet" />
              {s?.network
                ? `${bytes(s.network.write)}/s`
                : s?.errors.network || "正在计算速率…"}
            </>
          }
        >
          <div className="stat-bottom-label">
            <ArrowDownLeft size={12} />
            所有非回环网卡合计
          </div>
        </Stat>
        <Stat
          label="系统运行时间"
          icon={<Clock3 size={17} />}
          value={uptime(s?.uptime)}
          detail={
            <>
              <span className="tiny-dot" />
              {host?.hostname || "读取主机信息…"}
            </>
          }
        >
          <div className="stat-bottom-label">
            Load{" "}
            {load
              ? `${load.one.toFixed(2)} / ${load.five.toFixed(2)} / ${load.fifteen.toFixed(2)}`
              : "— / — / —"}
          </div>
        </Stat>
      </div>
      <div className="charts-grid">
        <MetricChart samples={monitor.samples} />
        <MetricChart samples={monitor.samples} network />
      </div>
      <div className="details-grid">
        <Card>
          <CardHeader className="section-card-header">
            <div className="card-title-icon">
              <Activity size={16} />
              <CardTitle>LNMP 服务</CardTitle>
              <span className="count-label">
                {healthy}/{services?.length ?? 0}
              </span>
            </div>
            <Button variant="ghost" size="sm" asChild>
              <Link to="/services">
                管理服务
                <ArrowRight size={13} />
              </Link>
            </Button>
          </CardHeader>
          <CardContent className="service-list">
            {services?.map((service) => (
              <div className="service-row" key={service.name}>
                <ServiceIcon name={service.name} />
                <div className="grow">
                  <strong>{serviceInfo[service.name]?.title}</strong>
                  <div className="small-note">
                    {serviceInfo[service.name]?.subtitle}
                  </div>
                </div>
                <ServiceStatus state={service.state} />
              </div>
            ))}
            {!services?.length && (
              <div className="empty-state">
                <CircuitBoard size={28} />
                <span>{serviceError || "未检测到 LNMP 服务"}</span>
                <p>安装后在服务配置中确认管理入口。</p>
              </div>
            )}
          </CardContent>
        </Card>
        <DiskTable snapshot={s} />
      </div>
      <div className="overview-foot">
        <span>
          <span className="tiny-dot" />
          单机实时监控
        </span>
        <span>每 2 秒采集 · 数据不落盘 · 曲线刷新后清空</span>
      </div>
    </>
  );
}
