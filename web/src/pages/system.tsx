import { Cpu, HardDrive, Server, ShieldCheck } from "lucide-react";
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
import { DiskTable, Meter } from "./dashboard";
import type { Host, Snapshot } from "@/lib/types";
import { bytes, uptime } from "@/lib/format";

export function System({
  host,
  snapshot,
  error,
}: {
  host?: Host;
  snapshot?: Snapshot;
  error?: string;
}) {
  const fields = [
    ["主机名称", host?.hostname],
    ["操作系统", host ? `${host.platform} ${host.version}` : undefined],
    ["内核版本", host?.kernel],
    ["系统架构", host?.arch],
    ["CPU 型号", host?.cpu_model],
    ["逻辑核心", host?.cores ? `${host.cores} 核心` : undefined],
    ["运行时间", uptime(snapshot?.uptime)],
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
              {fields.map(([label, value]) => (
                <div key={label}>
                  <dt>{label}</dt>
                  <dd>{value || "—"}</dd>
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
