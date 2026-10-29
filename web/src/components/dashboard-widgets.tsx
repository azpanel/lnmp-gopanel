import { useState, type ReactNode } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { Plus } from "lucide-react";
import { toast } from "sonner";
import {
  Bar,
  BarChart,
  CartesianGrid,
  ComposedChart,
  Line,
  Area,
  ReferenceLine,
  XAxis,
  YAxis,
} from "recharts";
import { api } from "@/lib/api";
import { bytes, clock, percent } from "@/lib/format";
import { chartPoints } from "@/lib/history";
import type {
  DashboardWidgetSelection,
  Host,
  Snapshot,
  WidgetID,
} from "@/lib/types";
import type { Monitor } from "@/lib/use-monitor";
import { Button } from "@/components/ui/button";
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
import { Checkbox } from "@/components/ui/checkbox";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog";
import {
  ChartContainer,
  ChartTooltip,
  ChartTooltipContent,
} from "@/components/ui/chart";

export const widgetOptions: {
  id: WidgetID;
  title: string;
  description: string;
  service?: boolean;
}[] = [
  { id: "disk_io", title: "磁盘读写速率", description: "磁盘读取与写入速率" },
  {
    id: "load",
    title: "系统负载趋势",
    description: "1、5、15 分钟负载及 CPU 核数参考线",
  },
  { id: "swap", title: "Swap 使用趋势", description: "交换空间容量与使用率" },
  {
    id: "memory_breakdown",
    title: "内存构成",
    description: "占用、缓存、缓冲区与空闲内存",
  },
  {
    id: "processes",
    title: "进程资源排行",
    description: "CPU／内存占用前 10 名",
  },
  {
    id: "nginx",
    title: "Nginx 活跃连接数",
    description: "活跃连接数量及趋势",
    service: true,
  },
  {
    id: "php_fpm",
    title: "PHP-FPM 进程与队列",
    description: "活跃、空闲进程和等待队列",
    service: true,
  },
  {
    id: "database",
    title: "数据库状态",
    description: "连接数趋势与慢查询累计数量",
    service: true,
  },
];
const queryKey = ["dashboard-widgets"];
export function useDashboardWidgets() {
  const client = useQueryClient();
  const query = useQuery({
    queryKey,
    queryFn: () => api<DashboardWidgetSelection>("/dashboard/widgets"),
    refetchInterval: 30000,
    refetchOnWindowFocus: "always",
    retry: false,
  });
  const mutation = useMutation({
    mutationFn: (enabled: WidgetID[]) =>
      api<DashboardWidgetSelection>("/dashboard/widgets", {
        method: "PUT",
        body: JSON.stringify({ enabled }),
      }),
    onMutate: async () => {
      await client.cancelQueries({ queryKey });
    },
    onSuccess: (data) => {
      client.setQueryData(queryKey, data);
    },
    onError: (error) => {
      toast.error(error.message);
    },
  });
  return {
    query,
    mutation,
    enabled: query.data?.enabled ?? [],
    save: mutation.mutateAsync,
  };
}
type WidgetControl = ReturnType<typeof useDashboardWidgets>;
export function AddWidgetButton({ control }: { control: WidgetControl }) {
  const [open, setOpen] = useState(false);
  const [draft, setDraft] = useState<WidgetID[]>([]);
  const unavailable = control.query.isPending || control.query.isError;
  return (
    <>
      <Button
        variant="outline"
        disabled={control.query.isPending || control.mutation.isPending}
        onClick={() => {
          setDraft([...control.enabled]);
          control.mutation.reset();
          setOpen(true);
        }}
      >
        <Plus size={16} />
        添加状态指标
      </Button>
      <Dialog
        open={open}
        onOpenChange={(value) => {
          if (!control.mutation.isPending) setOpen(value);
        }}
      >
        <DialogContent
          className="widget-dialog"
          showCloseButton={!control.mutation.isPending}
        >
          <DialogHeader>
            <DialogTitle>添加状态指标</DialogTitle>
            <DialogDescription>
              选择要显示的组件，保存在服务器并在各浏览器共享。
            </DialogDescription>
          </DialogHeader>
          {control.query.isPending && <p role="status">正在读取组件选择…</p>}
          {control.query.isError && (
            <div role="alert">
              {control.query.error.message}
              <Button
                variant="ghost"
                onClick={() =>
                  void control.query.refetch().then((result) => {
                    if (result.data) setDraft([...result.data.enabled]);
                  })
                }
              >
                重试
              </Button>
            </div>
          )}
          {[false, true].map((service) => (
            <fieldset
              key={String(service)}
              disabled={unavailable || control.mutation.isPending}
              className="widget-options"
            >
              <legend>{service ? "服务指标" : "系统指标"}</legend>
              {widgetOptions
                .filter((option) => !!option.service === service)
                .map((option) => (
                  <label className="widget-option" key={option.id}>
                    <Checkbox
                      checked={draft.includes(option.id)}
                      onCheckedChange={(checked) =>
                        setDraft((previous) =>
                          checked
                            ? [...previous, option.id]
                            : previous.filter((id) => id !== option.id),
                        )
                      }
                    />
                    <span>
                      <strong>{option.title}</strong>
                      <small>{option.description}</small>
                    </span>
                  </label>
                ))}
            </fieldset>
          ))}
          {control.mutation.isError && (
            <p role="alert" className="text-destructive">
              {control.mutation.error.message}
            </p>
          )}
          <DialogFooter>
            <Button
              variant="outline"
              disabled={control.mutation.isPending}
              onClick={() => setOpen(false)}
            >
              取消
            </Button>
            <Button
              disabled={unavailable || control.mutation.isPending}
              onClick={() =>
                void control
                  .save(draft)
                  .then(() => setOpen(false))
                  .catch(() => {})
              }
            >
              {control.mutation.isPending ? "保存中…" : "保存"}
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>
    </>
  );
}

const colors = ["#55bca5", "#9690dc", "#d69d55", "#7a9fd4"];
type Series = { key: string; label: string; color: string };
const trendSeries: Partial<Record<WidgetID, Series[]>> = {
  disk_io: [
    { key: "diskRead", label: "读取", color: colors[0] },
    { key: "diskWrite", label: "写入", color: colors[1] },
  ],
  load: [
    { key: "loadOne", label: "1 分钟", color: colors[0] },
    { key: "loadFive", label: "5 分钟", color: colors[1] },
    { key: "loadFifteen", label: "15 分钟", color: colors[2] },
  ],
  swap: [{ key: "swapUsed", label: "已用容量", color: colors[0] }],
  nginx: [{ key: "nginxActive", label: "活跃连接", color: colors[0] }],
  php_fpm: [
    { key: "phpActive", label: "活跃", color: colors[0] },
    { key: "phpIdle", label: "空闲", color: colors[1] },
    { key: "phpQueue", label: "等待队列", color: colors[2] },
  ],
  database: [{ key: "dbConnections", label: "连接数", color: colors[0] }],
};
function Legend({ series }: { series: Series[] }) {
  return (
    <div className="chart-legend widget-legend">
      {series.map((line) => (
        <span key={line.key}>
          <i style={{ background: line.color }} />
          {line.label}
        </span>
      ))}
    </div>
  );
}
function Trend({
  id,
  samples,
  cores,
}: {
  id: WidgetID;
  samples: Snapshot[];
  cores?: number;
}) {
  const lines = trendSeries[id] ?? [];
  const data = chartPoints(samples);
  const config = Object.fromEntries(
    lines.map((line) => [line.key, { label: line.label, color: line.color }]),
  );
  const format = (value: number) =>
    id === "disk_io"
      ? `${bytes(value)}/s`
      : id === "swap"
        ? bytes(value)
        : id === "load"
          ? value.toFixed(2)
          : String(value);
  const latest = samples.at(-1);
  const knownCores =
    id === "load" && cores != null && cores > 0 ? cores : undefined;
  const available = data.filter((point) =>
    lines.some((line) => point[line.key as keyof typeof point] != null),
  ).length;
  return (
    <>
      <Legend series={lines} />
      <div className="chart-content">
        <ChartContainer config={config} className="metric-chart">
          <ComposedChart
            data={data}
            margin={{ top: 25, right: 25, left: 0, bottom: 0 }}
            accessibilityLayer
          >
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
              tickFormatter={(time) => clock(time).slice(0, 5)}
              axisLine={false}
              tickLine={false}
              tickMargin={12}
              minTickGap={40}
            />
            <YAxis
              domain={[0, (max: number) => Math.max(max, knownCores ?? 0, 1)]}
              tickFormatter={format}
              allowDecimals={id === "load" || id === "disk_io" || id === "swap"}
              axisLine={false}
              tickLine={false}
              width={id === "disk_io" ? 100 : id === "swap" ? 88 : 55}
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
                        {format(Number(value))}
                      </span>
                    </>
                  )}
                />
              }
            />
            {knownCores != null && (
              <ReferenceLine
                y={knownCores}
                stroke={colors[3]}
                strokeDasharray="5 4"
                label={{
                  value: `${knownCores} 核`,
                  position: "insideTopRight",
                  fill: "var(--muted-foreground)",
                  fontSize: 12,
                }}
              />
            )}
            {lines.map((line) =>
              id === "swap" ? (
                <Area
                  key={line.key}
                  dataKey={line.key}
                  stroke={line.color}
                  fill={line.color}
                  fillOpacity={0.15}
                  dot={available === 1}
                  connectNulls={false}
                  isAnimationActive={false}
                />
              ) : (
                <Line
                  key={line.key}
                  dataKey={line.key}
                  stroke={line.color}
                  strokeWidth={1.8}
                  dot={available === 1}
                  connectNulls={false}
                  isAnimationActive={false}
                />
              ),
            )}
          </ComposedChart>
        </ChartContainer>
        {available < 2 && (
          <div className="chart-wait">
            {available === 0 ? "等待首次采集…" : "等待采集更多数据…"}
          </div>
        )}
      </div>
      {id === "load" && knownCores == null && (
        <p className="widget-note">CPU 核数暂不可用，参考线将在读取后显示。</p>
      )}
      {id === "swap" && latest?.swap && (
        <p className="widget-note">
          已用 {bytes(latest.swap.used)} / {bytes(latest.swap.total)} ·{" "}
          {percent(latest.swap.percent)}%
        </p>
      )}
      {id === "disk_io" && <p className="widget-note">所有整盘合计</p>}
      {id === "nginx" && latest?.nginx && (
        <p className="widget-note">
          当前活跃连接：{latest.nginx.active}（含等待连接）
        </p>
      )}
      {id === "php_fpm" && latest?.php_fpm && (
        <p className="widget-note">
          活跃 {latest.php_fpm.active} · 空闲 {latest.php_fpm.idle} · 等待{" "}
          {latest.php_fpm.queue}
        </p>
      )}
      {id === "database" && latest?.database && (
        <p className="widget-note">
          当前连接 {latest.database.connections} · 慢查询累计{" "}
          {latest.database.slow_queries}
          <br />
          慢查询自数据库启动或计数器重置起累计
        </p>
      )}
      <div className="chart-foot">
        <span>最近 5 分钟</span>
        <span>仅保存在当前页面</span>
      </div>
    </>
  );
}
function MemoryComposition({ snapshot }: { snapshot?: Snapshot }) {
  const memory = snapshot?.memory;
  if (
    !memory ||
    memory.other == null ||
    memory.free == null ||
    memory.buffers == null ||
    memory.cached == null
  )
    return <div className="widget-empty">等待内存构成数据…</div>;
  const parts = [
    { key: "other", label: "其他占用", value: memory.other, color: colors[0] },
    { key: "cached", label: "缓存", value: memory.cached, color: colors[1] },
    {
      key: "buffers",
      label: "缓冲区",
      value: memory.buffers,
      color: colors[2],
    },
    { key: "free", label: "空闲", value: memory.free, color: colors[3] },
  ];
  const config = Object.fromEntries(
    parts.map((part) => [part.key, { label: part.label, color: part.color }]),
  );
  return (
    <CardContent className="widget-memory">
      <p>
        总容量 <strong>{bytes(memory.total)}</strong>
        <br />
        可用内存 <strong>{bytes(memory.available)}</strong>
      </p>
      <ChartContainer config={config} className="memory-breakdown-chart">
        <BarChart
          layout="vertical"
          data={[memory]}
          margin={{ top: 10, right: 0, bottom: 0, left: 0 }}
          accessibilityLayer
        >
          <XAxis type="number" domain={[0, memory.total]} hide />
          <YAxis type="category" hide />
          <ChartTooltip
            content={
              <ChartTooltipContent
                hideLabel
                formatter={(value, name) => (
                  <>
                    <span>{config[String(name)]?.label}</span>
                    <span className="ml-auto font-mono">
                      {bytes(Number(value))}
                    </span>
                  </>
                )}
              />
            }
          />
          {parts.map((part) => (
            <Bar
              key={part.key}
              dataKey={part.key}
              stackId="memory"
              fill={part.color}
              isAnimationActive={false}
            />
          ))}
        </BarChart>
      </ChartContainer>
      <div className="memory-parts">
        {parts.map((part) => (
          <div key={part.key}>
            <span>
              <i style={{ background: part.color }} />
              {part.label}
            </span>
            <strong>{bytes(part.value)}</strong>
          </div>
        ))}
      </div>
      <p className="small-note">可用内存包含可回收缓存，与上方分段存在重叠。</p>
    </CardContent>
  );
}
function ProcessRanking({ snapshot }: { snapshot?: Snapshot }) {
  const [mode, setMode] = useState<"cpu" | "memory">("cpu");
  const processes = snapshot?.processes;
  const items = processes?.[mode] ?? [];
  const data = items.map((item) => ({
    ...item,
    label: `${item.name} (${item.pid})`,
    value: mode === "cpu" ? item.cpu : item.rss,
  }));
  return (
    <CardContent className="widget-processes">
      <div className="widget-toggle" role="group" aria-label="进程排行方式">
        <Button
          variant={mode === "cpu" ? "secondary" : "ghost"}
          size="sm"
          aria-pressed={mode === "cpu"}
          onClick={() => setMode("cpu")}
        >
          CPU
        </Button>
        <Button
          variant={mode === "memory" ? "secondary" : "ghost"}
          size="sm"
          aria-pressed={mode === "memory"}
          onClick={() => setMode("memory")}
        >
          内存
        </Button>
      </div>
      {!items.length ? (
        <div className="widget-empty">
          {!processes
            ? "等待进程数据…"
            : mode === "cpu"
              ? "正在计算 CPU 使用率…"
              : "暂无可用进程"}
        </div>
      ) : (
        <ChartContainer
          config={{
            value: {
              label: mode === "cpu" ? "CPU" : "内存 RSS",
              color: colors[0],
            },
          }}
          className="process-ranking-chart"
          style={{ height: Math.max(150, items.length * 32 + 30) }}
        >
          <BarChart
            data={data}
            layout="vertical"
            margin={{ top: 5, right: 25, left: 0, bottom: 0 }}
            accessibilityLayer
          >
            <XAxis
              type="number"
              domain={mode === "cpu" ? [0, 100] : [0, "auto"]}
              tickFormatter={(value) =>
                mode === "cpu"
                  ? `${Number(value).toFixed(0)}%`
                  : bytes(value, 0)
              }
              axisLine={false}
              tickLine={false}
            />
            <YAxis
              type="category"
              dataKey="label"
              width={180}
              tick={{ fontSize: 11 }}
              tickFormatter={(value) =>
                value.length > 28 ? `${value.slice(0, 27)}…` : value
              }
              axisLine={false}
              tickLine={false}
              interval={0}
            />
            <ChartTooltip
              content={
                <ChartTooltipContent
                  labelFormatter={(_label, payload) =>
                    payload[0]?.payload?.label ?? ""
                  }
                  formatter={(value, _name, item) => (
                    <>
                      <span>{mode === "cpu" ? "CPU" : "内存 RSS"}</span>
                      <span className="ml-auto font-mono">
                        {mode === "cpu"
                          ? `${Number(value).toFixed(1)}%`
                          : `${bytes(Number(value))} (${percent(item.payload.memory_percent)}%)`}
                      </span>
                    </>
                  )}
                />
              }
            />
            <Bar
              dataKey="value"
              fill={colors[0]}
              radius={[0, 3, 3, 0]}
              isAnimationActive={false}
            />
          </BarChart>
        </ChartContainer>
      )}
      {processes?.partial && (
        <p role="status" className="widget-note">
          部分进程不可读取或采集超时，排行可能不完整。
        </p>
      )}
      <p className="small-note">前 10 名 · CPU 按整机总算力计算 · 内存为 RSS</p>
    </CardContent>
  );
}
function WidgetCard({
  id,
  title,
  children,
}: {
  id: WidgetID;
  title: string;
  children: ReactNode;
}) {
  return (
    <Card className="chart-card optional-widget" data-widget={id}>
      <CardHeader className="chart-header">
        <CardTitle>{title}</CardTitle>
      </CardHeader>
      {children}
    </Card>
  );
}
export function DashboardWidgets({
  control,
  monitor,
  host,
}: {
  control: WidgetControl;
  monitor: Monitor;
  host?: Host;
}) {
  const snapshot = monitor.latest;
  return (
    <>
      {control.query.isError && (
        <div className="notice" role="alert">
          {control.query.error.message}
          <Button
            variant="ghost"
            size="sm"
            onClick={() => void control.query.refetch()}
          >
            重试
          </Button>
        </div>
      )}
      {control.enabled.length > 0 && (
        <div className="optional-widgets-grid">
          {widgetOptions
            .filter((option) => control.enabled.includes(option.id))
            .map((option) => {
              const error =
                snapshot?.errors[
                  option.id === "memory_breakdown" ? "memory" : option.id
                ];
              let content: ReactNode;
              if (error)
                content = (
                  <div className="widget-empty" role="status">
                    {error}
                  </div>
                );
              else if (option.id === "memory_breakdown")
                content = <MemoryComposition snapshot={snapshot} />;
              else if (option.id === "processes")
                content = <ProcessRanking snapshot={snapshot} />;
              else if (option.id === "swap" && snapshot?.swap?.total === 0)
                content = <div className="widget-empty">未启用 Swap</div>;
              else
                content = (
                  <Trend
                    id={option.id}
                    samples={monitor.samples}
                    cores={host?.cores}
                  />
                );
              return (
                <WidgetCard key={option.id} id={option.id} title={option.title}>
                  {content}
                </WidgetCard>
              );
            })}
        </div>
      )}
    </>
  );
}
