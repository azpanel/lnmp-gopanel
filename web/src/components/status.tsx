import { Badge } from "@/components/ui/badge";
import { cn } from "@/lib/utils";
const labels: Record<string, string> = {
  running: "运行中",
  stopped: "已停止",
  unknown: "未知",
  transitioning: "切换中",
};
export function ServiceStatus({ state }: { state: string }) {
  return (
    <Badge
      variant="outline"
      className={cn(
        "status-badge",
        state === "running"
          ? "status-green"
          : state === "stopped"
            ? "status-red"
            : "status-muted",
      )}
    >
      <span className="status-dot" />
      {labels[state] || "未知"}
    </Badge>
  );
}
export const serviceInfo: Record<
  string,
  { title: string; subtitle: string; letter: string; color: string }
> = {
  nginx: {
    title: "Nginx",
    subtitle: "Web 服务器",
    letter: "N",
    color: "#71c792",
  },
  "php-fpm": {
    title: "PHP-FPM",
    subtitle: "PHP 进程管理器",
    letter: "P",
    color: "#a6a4ec",
  },
  mysql: {
    title: "MySQL",
    subtitle: "关系型数据库",
    letter: "M",
    color: "#e7b576",
  },
  mariadb: {
    title: "MariaDB",
    subtitle: "关系型数据库",
    letter: "M",
    color: "#e7b576",
  },
  redis: {
    title: "Redis",
    subtitle: "内存数据库与缓存",
    letter: "R",
    color: "#dc7777",
  },
};
export function ServiceIcon({ name }: { name: string }) {
  const info = serviceInfo[name];
  return (
    <span
      className="service-icon"
      style={{
        color: info?.color,
        background: `${info?.color}12`,
        borderColor: `${info?.color}28`,
      }}
    >
      {info?.letter || "?"}
    </span>
  );
}
