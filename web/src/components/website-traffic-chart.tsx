import { useEffect, useState } from "react";
import { useQuery } from "@tanstack/react-query";
import {
  Bar,
  CartesianGrid,
  ComposedChart,
  Line,
  XAxis,
  YAxis,
} from "recharts";
import { api } from "@/lib/api";
import { bytes } from "@/lib/format";
import type { WebsiteTraffic } from "@/lib/types";
import { readTrafficView, saveTrafficView } from "@/lib/website-traffic";
import { Button } from "@/components/ui/button";
import {
  ChartContainer,
  ChartTooltip,
  ChartTooltipContent,
} from "@/components/ui/chart";
import { Switch } from "@/components/ui/switch";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select";

const config = {
  rx: { label: "入站", color: "#77d9c2" },
  tx: { label: "出站", color: "#a4a0e8" },
};

export function WebsiteTrafficChart() {
  const [view, setView] = useState(readTrafficView);
  useEffect(() => saveTrafficView(view), [view]);
  const query = useQuery({
    queryKey: ["dashboard-traffic", view.period],
    queryFn: ({ signal }) =>
      api<WebsiteTraffic>(`/dashboard/traffic?period=${view.period}`, {
        signal,
      }),
    refetchInterval: 60000,
    staleTime: 60000,
    retry: false,
  });
  const data = query.data;
  const available = data?.points.some(
    (point) => point.rx != null || point.tx != null,
  );
  return (
    <>
      <div className="traffic-controls">
        <label className="traffic-period">
          <span data-active={view.period === "day"}>日</span>
          <Switch
            aria-label="按小时显示流量"
            checked={view.period === "hour"}
            onCheckedChange={(checked) =>
              setView((previous) => ({
                ...previous,
                period: checked ? "hour" : "day",
              }))
            }
          />
          <span data-active={view.period === "hour"}>小时</span>
        </label>
        <Select
          value={view.chart}
          onValueChange={(chart) => {
            if (chart === "line" || chart === "bar")
              setView((previous) => ({ ...previous, chart }));
          }}
        >
          <SelectTrigger
            className="w-[116px] bg-white text-[14px] text-slate-900"
            aria-label="流量图表类型"
          >
            <SelectValue />
          </SelectTrigger>
          <SelectContent>
            <SelectItem className="text-[14px]" value="line">
              折线图
            </SelectItem>
            <SelectItem className="text-[14px]" value="bar">
              柱状图
            </SelectItem>
          </SelectContent>
        </Select>
      </div>
      <div className="chart-legend traffic-legend">
        {Object.entries(config).map(([key, item]) => (
          <span key={key}>
            <i style={{ background: item.color }} />
            {item.label}
          </span>
        ))}
      </div>
      {query.isPending && (
        <div className="widget-empty" role="status">
          正在读取流量数据…
        </div>
      )}
      {query.isError && (
        <div className="widget-note" role="status">
          {data ? "更新失败，当前显示上次数据。" : ""}
          {query.error.message}
          <Button
            variant="ghost"
            size="sm"
            disabled={query.isFetching}
            onClick={() => void query.refetch()}
          >
            重试
          </Button>
        </div>
      )}
      {data &&
        (available ? (
          <div className="chart-content">
            <ChartContainer config={config} className="metric-chart">
              <ComposedChart
                data={data.points}
                margin={{ top: 15, right: 25, left: 0, bottom: 0 }}
                accessibilityLayer
              >
                <CartesianGrid
                  vertical={false}
                  strokeDasharray="3 5"
                  stroke="var(--border)"
                />
                <XAxis
                  dataKey="label"
                  tickFormatter={(label: string) =>
                    view.period === "day" ? label.slice(5) : label.slice(11, 16)
                  }
                  axisLine={false}
                  tickLine={false}
                  tickMargin={12}
                  minTickGap={35}
                />
                <YAxis
                  domain={[0, "auto"]}
                  tickFormatter={(value) => bytes(value, 0)}
                  axisLine={false}
                  tickLine={false}
                  tickMargin={8}
                  width={88}
                />
                <ChartTooltip
                  content={
                    <ChartTooltipContent
                      labelFormatter={(_label, payload) =>
                        payload[0]?.payload?.label ?? ""
                      }
                      formatter={(value, name) => (
                        <>
                          <span className="text-muted-foreground">
                            {config[String(name) as keyof typeof config]
                              ?.label ?? name}
                          </span>
                          <span className="ml-auto font-mono">
                            {bytes(Number(value))}
                          </span>
                        </>
                      )}
                    />
                  }
                />
                {Object.entries(config).map(([key, item]) =>
                  view.chart === "line" ? (
                    <Line
                      key={key}
                      dataKey={key}
                      type="linear"
                      stroke={item.color}
                      strokeWidth={1.8}
                      dot={{ r: 2 }}
                      connectNulls={false}
                      isAnimationActive={false}
                    />
                  ) : (
                    <Bar
                      key={key}
                      dataKey={key}
                      fill={item.color}
                      radius={[3, 3, 0, 0]}
                      maxBarSize={24}
                      isAnimationActive={false}
                    />
                  ),
                )}
              </ComposedChart>
            </ChartContainer>
          </div>
        ) : (
          <div className="widget-empty" role="status">
            等待 vnstat 积累流量数据…
          </div>
        ))}
      <div className="chart-foot traffic-foot">
        <span>
          {view.period === "day" ? "最近 30 天" : "最近 24 小时"} · 服务器时区
        </span>
        {data && (
          <span>
            网卡 {data.interface} · 更新于 {data.updated_at.replace("T", " ")}
          </span>
        )}
      </div>
      <p className="widget-note">
        服务器出口网卡流量 · vnstat 每 60 秒查询 · 当前日/小时数据尚未结束
      </p>
    </>
  );
}
