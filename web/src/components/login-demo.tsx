import { Meter } from "@/components/meter";
import { ServiceIcon, ServiceStatus, serviceInfo } from "@/components/status";
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
import type { Snapshot } from "@/lib/types";
import { FileText, HardDrive } from "lucide-react";
import { lazy, Suspense } from "react";
const FileTreemap = lazy(() =>
  import("@/components/file-treemap").then((module) => ({
    default: module.FileTreemap,
  })),
);
const LogViewport = lazy(() =>
  import("@/components/log-viewport").then((module) => ({
    default: module.LogViewport,
  })),
);
const MetricChart = lazy(() =>
  import("@/components/metric-chart").then((module) => ({
    default: module.MetricChart,
  })),
);

// Fixed examples are independent of the host and require no authenticated APIs.
const samples: Snapshot[] = Array.from({ length: 61 }, (_, index) => ({
  timestamp: new Date(Date.UTC(2026, 0, 1, 2, 0, index * 5)).toISOString(),
  cpu: Math.round(24 + 11 * Math.sin(index / 5) + 6 * Math.cos(index / 3)),
  memory: {
    used: 3.2 * 1024 ** 3,
    total: 8 * 1024 ** 3,
    percent: 40 + 3 * Math.sin(index / 9),
  },
  swap: null,
  load: null,
  uptime: null,
  network: {
    read: Math.round((320 + 100 * Math.sin(index / 6)) * 1024),
    write: Math.round((90 + 35 * Math.cos(index / 7)) * 1024),
  },
  disk_io: null,
  disks: [],
  errors: {},
}));
const files = [
  { path: "/demo/media", size: 18 * 1024 ** 3 },
  { path: "/demo/database", size: 12 * 1024 ** 3 },
  { path: "/demo/backups", size: 8 * 1024 ** 3 },
  { path: "/demo/assets", size: 4 * 1024 ** 3 },
];
const logLines = [
  "10:05:01 GET / 200",
  "10:05:02 GET /api 200",
  "10:05:03 GET /app 304",
  "10:05:04 cache hit",
  "10:05:05 reload done",
].map((text, index) => ({ offset: String(index * 32), text }));

export default function Demo({ theme }: { theme: string }) {
  if (theme === "resources") return <MetricChart samples={samples} />;
  if (theme === "network") return <MetricChart samples={samples} network />;
  if (theme === "logs") {
    return (
      <Card className="login-demo-card">
        <CardHeader className="login-demo-log-heading">
          <CardTitle>
            <FileText size={16} />
            日志速览
          </CardTitle>
        </CardHeader>
        <CardContent>
          <Suspense fallback={<div role="status" aria-label="正在加载演示" />}>
            <LogViewport lines={logLines} follow={false} onFollow={() => {}} />
          </Suspense>
        </CardContent>
      </Card>
    );
  }
  if (theme === "services") {
    return (
      <Card className="login-demo-card">
        <CardHeader>
          <CardTitle>LNMP 服务</CardTitle>
        </CardHeader>
        <CardContent className="login-demo-services">
          {["nginx", "php-fpm", "mysql", "redis"].map((name) => (
            <div className="login-demo-service" key={name}>
              <ServiceIcon name={name} />
              <span>{serviceInfo[name].title}</span>
              <ServiceStatus state="running" />
            </div>
          ))}
        </CardContent>
      </Card>
    );
  }
  return (
    <Card className="login-demo-card">
      <CardHeader className="login-demo-disk-heading">
        <CardTitle>
          <HardDrive size={16} />
          磁盘空间
        </CardTitle>
        <span>42 / 100 GiB</span>
      </CardHeader>
      <CardContent className="login-demo-storage">
        <Meter value={42} />
        <Suspense fallback={<div role="status" aria-label="正在加载演示" />}>
          <FileTreemap files={files} hint="点击色块查看示例文件占用" />
        </Suspense>
      </CardContent>
    </Card>
  );
}
