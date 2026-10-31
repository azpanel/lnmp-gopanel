import { ArrowDown, ArrowUp, Network } from "lucide-react";
import { Badge } from "@/components/ui/badge";
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
import { bytes } from "@/lib/format";
import type { Snapshot } from "@/lib/types";

export function SystemNetwork({ snapshot }: { snapshot?: Snapshot }) {
  const info = snapshot?.network_info;
  const error = snapshot?.errors?.network_info;
  return (
    <Card className="system-network-card" role="region" aria-label="网络信息">
      <CardHeader className="block">
        <div className="flex items-center justify-between gap-3">
          <div className="card-title-icon">
            <Network size={17} />
            <CardTitle>网络信息</CardTitle>
          </div>
          {info && (
            <Badge
              variant="outline"
              className={
                info.up
                  ? "status-green text-[11px] leading-4"
                  : "status-red text-[11px] leading-4"
              }
              title="网卡链路状态"
            >
              {info.up ? "已连接" : "未连接"}
            </Badge>
          )}
        </div>
      </CardHeader>
      <CardContent>
        <dl className="system-network-fields">
          <div>
            <dt>主网卡</dt>
            <dd>{info?.name || "—"}</dd>
          </div>
          <div>
            <dt>IPv4</dt>
            <dd>{info ? info.ipv4.join(" · ") || "未分配" : "—"}</dd>
          </div>
          <div>
            <dt>IPv6</dt>
            <dd>{info ? info.ipv6.join(" · ") || "未分配" : "—"}</dd>
          </div>
          <div>
            <dt>实时流量</dt>
            <dd className="network-transfer">
              <span title="接收速率">
                <ArrowDown size={13} aria-hidden="true" />
                <span className="sr-only">接收速率：</span>
                {bytes(info?.rates?.read)}
                <small>/s</small>
              </span>
              <span title="发送速率">
                <ArrowUp size={13} aria-hidden="true" />
                <span className="sr-only">发送速率：</span>
                {bytes(info?.rates?.write)}
                <small>/s</small>
              </span>
            </dd>
          </div>
        </dl>
        {error && (
          <p className="network-info-error muted" role="status">
            {error}
          </p>
        )}
      </CardContent>
    </Card>
  );
}
