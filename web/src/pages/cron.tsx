import { useQuery, useQueryClient } from "@tanstack/react-query";
import { useSessionMutation } from "@/lib/use-session-mutation";
import { lazy, Suspense, useState } from "react";
import { Clock3, RefreshCw } from "lucide-react";
import { Button } from "@/components/ui/button";
import { Switch } from "@/components/ui/switch";
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from "@/components/ui/table";
import { api } from "@/lib/api";
import type { CronSnapshot } from "@/lib/types";
import { CronSchedule } from "@/components/cron-schedule";

const CronSource = lazy(() =>
  import("@/components/cron-source").then((module) => ({
    default: module.CronSource,
  })),
);

export function Cron() {
  const client = useQueryClient();
  const [sourceOpen, setSourceOpen] = useState(false);
  const query = useQuery({
    queryKey: ["cron"],
    queryFn: ({ signal }) => api<CronSnapshot>("/cron", { signal }),
    retry: false,
    refetchOnWindowFocus: false,
  });
  const data = query.data;
  const toggle = useSessionMutation({
    mutationFn: (input: { line: number; enabled: boolean; revision: string }) =>
      api<CronSnapshot>("/cron/toggle", {
        method: "POST",
        body: JSON.stringify(input),
      }),
    onMutate: async () => {
      await client.cancelQueries({ queryKey: ["cron"] });
    },
    onSuccess: (snapshot) => {
      client.setQueryData(["cron"], snapshot);
    },
  });
  return (
    <>
      <div className="page-heading">
        <div>
          <div className="eyebrow muted">SCHEDULED TASKS</div>
          <h1>定时任务</h1>
          <p>查看和开关 root 用户的定时任务。</p>
        </div>
        <Button
          variant="outline"
          size="sm"
          disabled={query.isFetching || toggle.isPending}
          onClick={() => {
            toggle.reset();
            void query.refetch();
          }}
        >
          <RefreshCw
            size={14}
            className={query.isFetching ? "animate-spin" : ""}
          />
          {query.isFetching ? "读取中…" : query.error ? "重试" : "刷新"}
        </Button>
      </div>
      {query.error && (
        <div className="notice" role="alert">
          定时任务读取失败：{query.error.message}
          {data ? "。下方保留上次读取的数据，可能已过期。" : ""}
        </div>
      )}
      {toggle.error && (
        <div className="notice" role="alert">
          定时任务开关失败：{toggle.error.message}
        </div>
      )}
      {data?.warnings.map((warning, index) => (
        <div className="notice" role="status" key={index}>
          {warning}
        </div>
      ))}
      <Card>
        <CardHeader className="storage-section-header">
          <CardTitle className="card-title-icon">
            <Clock3 size={17} />
            root crontab
          </CardTitle>
          <span className="small-note" aria-live="polite">
            {data
              ? `${data.tasks.length} 项任务 · 最近读取 ${new Date(data.updated_at).toLocaleString("zh-CN")}`
              : query.isPending
                ? "正在读取…"
                : "尚未读取成功"}
          </span>
        </CardHeader>
        <CardContent className="table-content">
          <Table aria-label="定时任务列表" className="cron-table">
            <TableHeader>
              <TableRow>
                <TableHead>行号</TableHead>
                <TableHead>执行周期</TableHead>
                <TableHead>执行命令</TableHead>
                <TableHead>启用</TableHead>
              </TableRow>
            </TableHeader>
            <TableBody>
              {data?.tasks.map((task) => (
                <TableRow key={`${data.updated_at}:${task.line}`}>
                  <TableCell className="font-mono text-muted-foreground">
                    {task.line}
                  </TableCell>
                  <TableCell className="font-mono">
                    {task.enabled ? (
                      <CronSchedule task={task} revision={data.updated_at} />
                    ) : (
                      <span title="任务已关闭，不会执行">{task.schedule}</span>
                    )}
                  </TableCell>
                  <TableCell>
                    <code className="cron-command">{task.command}</code>
                  </TableCell>
                  <TableCell>
                    <Switch
                      aria-label={`启用第 ${task.line} 行定时任务`}
                      checked={task.enabled}
                      disabled={
                        toggle.isPending || query.isFetching || !!query.error
                      }
                      onCheckedChange={(enabled) =>
                        toggle.mutate({
                          line: task.line,
                          enabled,
                          revision: data.revision,
                        })
                      }
                    />
                  </TableCell>
                </TableRow>
              ))}
              {!data?.tasks.length && (
                <TableRow>
                  <TableCell colSpan={4} className="empty-cell">
                    {query.isPending
                      ? "正在读取定时任务…"
                      : !data
                        ? "无法获取定时任务，请重试。"
                        : data.warnings.length
                          ? "未识别到定时任务，请查看提示与 crontab 原文。"
                          : "root 用户尚未配置定时任务。"}
                  </TableCell>
                </TableRow>
              )}
            </TableBody>
          </Table>
        </CardContent>
      </Card>
      {data && (
        <details
          className="cron-source"
          onToggle={(event) => setSourceOpen(event.currentTarget.open)}
        >
          <summary>查看 crontab 原文</summary>
          {sourceOpen && (
            <Suspense fallback={<pre>{data.raw || "（空）"}</pre>}>
              <CronSource raw={data.raw} />
            </Suspense>
          )}
        </details>
      )}
    </>
  );
}
