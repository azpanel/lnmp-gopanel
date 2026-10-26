import { useId } from "react";
import { Area, AreaChart, CartesianGrid, XAxis, YAxis } from "recharts";
import {
  ChartContainer,
  ChartTooltip,
  ChartTooltipContent,
} from "@/components/ui/chart";
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
import { chartPoints } from "@/lib/history";
import { bytes, clock } from "@/lib/format";
import type { Snapshot } from "@/lib/types";

export function MetricChart({
  samples,
  network = false,
}: {
  samples: Snapshot[];
  network?: boolean;
}) {
  const id = useId().replace(/[^a-z0-9]/gi, "");
  const data = chartPoints(samples);
  const lines = network
    ? [
        { key: "download", label: "接收", color: "#77d9c2" },
        { key: "upload", label: "发送", color: "#a4a0e8" },
      ]
    : [
        { key: "cpu", label: "CPU", color: "#77d9c2" },
        { key: "memory", label: "内存", color: "#a4a0e8" },
      ];
  const config = Object.fromEntries(
    lines.map((l) => [l.key, { label: l.label, color: l.color }]),
  );
  return (
    <Card className="chart-card">
      <CardHeader className="chart-header">
        <CardTitle>{network ? "网络吞吐" : "资源使用趋势"}</CardTitle>
        <div className="chart-legend">
          {lines.map((l) => (
            <span key={l.key}>
              <i style={{ background: l.color }} />
              {l.label}
            </span>
          ))}
        </div>
      </CardHeader>
      <CardContent className="chart-content">
        <ChartContainer config={config} className="metric-chart">
          <AreaChart
            data={data}
            margin={{ top: 15, right: 28, left: 0, bottom: 0 }}
            accessibilityLayer
          >
            <defs>
              {lines.map((l) => (
                <linearGradient
                  key={l.key}
                  id={`${id}-${l.key}`}
                  x1="0"
                  y1="0"
                  x2="0"
                  y2="1"
                >
                  <stop offset="0%" stopColor={l.color} stopOpacity={0.2} />
                  <stop offset="95%" stopColor={l.color} stopOpacity={0.01} />
                </linearGradient>
              ))}
            </defs>
            <CartesianGrid
              vertical={false}
              strokeDasharray="3 5"
              stroke="var(--border)"
            />
            <XAxis
              dataKey="time"
              type="number"
              scale="time"
              domain={
                data.length > 1
                  ? ["dataMin", "dataMax"]
                  : [Date.now() - 300000, Date.now()]
              }
              tickFormatter={(t) => clock(t).slice(0, 5)}
              axisLine={false}
              tickLine={false}
              tickMargin={12}
              minTickGap={40}
            />
            <YAxis
              domain={network ? [0, "auto"] : [0, 100]}
              tickFormatter={(v) => (network ? bytes(v, 0) : `${v}%`)}
              axisLine={false}
              tickLine={false}
              tickMargin={8}
              width={network ? 88 : 64}
            />
            <ChartTooltip
              content={
                <ChartTooltipContent
                  labelFormatter={(_label, payload) =>
                    clock(payload[0]?.payload?.time ?? Date.now())
                  }
                  formatter={(value, name) => (
                    <>
                      <span className="text-muted-foreground">
                        {config[String(name)]?.label ?? name}
                      </span>
                      <span className="ml-auto font-mono">
                        {network
                          ? `${bytes(Number(value))}/s`
                          : `${Number(value).toFixed(1)}%`}
                      </span>
                    </>
                  )}
                />
              }
            />
            {lines.map((l) => (
              <Area
                key={l.key}
                dataKey={l.key}
                type="monotone"
                stroke={l.color}
                strokeWidth={1.8}
                fill={`url(#${id}-${l.key})`}
                dot={data.length === 1}
                isAnimationActive={false}
                connectNulls={false}
              />
            ))}
          </AreaChart>
        </ChartContainer>
        {samples.length < 2 && (
          <div className="chart-wait">等待采集更多数据…</div>
        )}
      </CardContent>
      <div className="chart-foot">
        <span>最近 5 分钟</span>
        <span>仅保存在当前页面</span>
      </div>
    </Card>
  );
}
