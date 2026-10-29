import {
  CircuitBoard,
  Clock3,
  Cpu,
  HardDrive,
  Layers3,
  Monitor,
  Server,
  Settings2,
  ShieldCheck,
} from "lucide-react";
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
import { DiskTable } from "./dashboard";
import { Meter } from "@/components/meter";
import type { Host, Snapshot } from "@/lib/types";
import { bytes, uptime } from "@/lib/format";
import { hostBrands } from "@/lib/host-brands";

export function System({
  host,
  snapshot,
  error,
}: {
  host?: Host;
  snapshot?: Snapshot;
  error?: string;
}) {
  const brands = hostBrands(host);
  const fields = [
    { label: "主机名称", value: host?.hostname, icon: Server },
    {
      label: "操作系统",
      value: host
        ? [host.platform.trim(), host.version.trim()].filter(Boolean).join(" ")
        : undefined,
      icon: Monitor,
      brand: brands.platform,
    },
    {
      label: "内核版本",
      value: host?.kernel,
      icon: Settings2,
      brand: brands.kernel,
    },
    {
      label: "系统架构",
      value: host?.arch,
      icon: CircuitBoard,
      brand: brands.arch,
    },
    {
      label: "CPU 型号",
      value: host?.cpu_model,
      icon: Cpu,
      brand: brands.cpu_model,
    },
    {
      label: "逻辑核心",
      value: host?.cores ? `${host.cores} 核心` : undefined,
      icon: Layers3,
    },
    { label: "运行时间", value: uptime(snapshot?.uptime), icon: Clock3 },
  ];
  return (
    <>
      <div className="page-heading">
        <div>
          <div className="eyebrow muted">SYSTEM INFORMATION</div>
          <h1>系统信息</h1>
          <p>查看本机环境与资源配置。</p>
        </div>
      </div>
      {error && <div className="notice">主机信息读取失败：{error}</div>}
      <div className="system-grid">
        <Card>
          <CardHeader>
            <div className="card-title-icon">
              <Server size={17} />
              <CardTitle>主机环境</CardTitle>
            </div>
          </CardHeader>
          <CardContent>
            <dl className="host-fields">
              {fields.map(({ label, value, icon: Icon, brand }) => (
                <div key={label}>
                  <dt>
                    <Icon size={15} aria-hidden="true" />
                    {label}
                  </dt>
                  <dd>
                    {brand && value?.trim() && (
                      <img
                        key={brand}
                        className="host-brand-icon"
                        src={`/brands/${brand}.svg`}
                        alt=""
                        aria-hidden="true"
                        width={24}
                        height={24}
                        onError={(event) => {
                          event.currentTarget.style.display = "none";
                        }}
                      />
                    )}
                    <span>{value?.trim() || "—"}</span>
                  </dd>
                </div>
              ))}
            </dl>
          </CardContent>
        </Card>
        <div className="system-right">
          <Card>
            <CardHeader>
              <div className="card-title-icon">
                <Cpu size={17} />
                <CardTitle>负载与交换空间</CardTitle>
              </div>
            </CardHeader>
            <CardContent>
              <div className="load-grid">
                {["one", "five", "fifteen"].map((key, i) => (
                  <div key={key}>
                    <span>{[1, 5, 15][i]} 分钟负载</span>
                    <strong>
                      {snapshot?.load?.[key as "one"]?.toFixed(2) ?? "—"}
                    </strong>
                  </div>
                ))}
              </div>
              <div className="swap-label">
                <span>Swap 使用率</span>
                <span>
                  {bytes(snapshot?.swap?.used)} / {bytes(snapshot?.swap?.total)}
                </span>
              </div>
              <Meter value={snapshot?.swap?.percent} />
            </CardContent>
          </Card>
          <Card>
            <CardHeader>
              <div className="card-title-icon">
                <HardDrive size={17} />
                <CardTitle>磁盘 I/O</CardTitle>
              </div>
            </CardHeader>
            <CardContent>
              <div className="io-grid">
                <div>
                  <span>读取速率</span>
                  <strong>
                    {snapshot?.disk_io ? bytes(snapshot.disk_io.read) : "—"}
                    <small>/s</small>
                  </strong>
                </div>
                <div>
                  <span>写入速率</span>
                  <strong>
                    {snapshot?.disk_io ? bytes(snapshot.disk_io.write) : "—"}
                    <small>/s</small>
                  </strong>
                </div>
              </div>
            </CardContent>
          </Card>
          <div className="privacy-note">
            <ShieldCheck size={18} />
            <span>监控指标仅在内存中处理，不写入数据库或磁盘。</span>
          </div>
        </div>
      </div>
      <DiskTable snapshot={snapshot} />
    </>
  );
}
