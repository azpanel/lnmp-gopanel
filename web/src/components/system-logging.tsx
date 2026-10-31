import { useRef, useState } from "react";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { FileText, Loader2 } from "lucide-react";
import { toast } from "sonner";
import { api } from "@/lib/api";
import { useSessionMutation } from "@/lib/use-session-mutation";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
import {
  Dialog,
  DialogClose,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog";

type Kind = "php" | "database";
type LoggingView = {
  kind: Kind;
  service: string;
  path: string;
  available: boolean;
  configured: boolean;
  message: string;
};
type LoggingSnapshot = Record<Kind, LoggingView>;
const labels: Record<Kind, string> = {
  php: "PHP错误日志",
  database: "DB慢日志",
};

export function SystemLogging() {
  const [selection, setSelection] = useState<Kind | null>(null);
  const trigger = useRef<HTMLButtonElement | null>(null);
  const client = useQueryClient();
  const status = useQuery({
    queryKey: ["system-logging"],
    queryFn: ({ signal }) =>
      api<LoggingSnapshot>("/system/logging", { signal }),
    refetchInterval: 30000,
  });
  const enable = useSessionMutation({
    mutationFn: (kind: Kind) =>
      api<{ message: string }>(`/system/logging/${kind}/enable`, {
        method: "POST",
      }),
    onSuccess: (result) => {
      toast.success(result.message);
      setSelection(null);
    },
    onError: (error) => toast.error(error.message),
    onSettled: async () => {
      await Promise.all([
        client.invalidateQueries({ queryKey: ["system-logging"] }),
        client.invalidateQueries({ queryKey: ["services"] }),
        client.invalidateQueries({ queryKey: ["logs"] }),
      ]);
    },
  });
  const service = selection && status.data?.[selection].service;
  return (
    <>
      <Card>
        <CardHeader>
          <div className="card-title-icon">
            <FileText size={17} />
            <CardTitle>日志设置</CardTitle>
          </div>
        </CardHeader>
        <CardContent>
          {status.isPending && (
            <p className="muted" role="status">
              正在读取日志配置…
            </p>
          )}
          {status.error && (
            <div
              className="rounded-md border border-destructive/30 p-3 text-sm"
              role="alert"
            >
              日志配置读取失败：{status.error.message}
              <Button
                variant="outline"
                size="sm"
                className="ml-2"
                disabled={status.isFetching}
                onClick={() => void status.refetch()}
              >
                重试
              </Button>
            </div>
          )}
          <div className="space-y-4">
            {(["php", "database"] as const).map((kind) => {
              const view = status.data?.[kind];
              return (
                <div
                  key={kind}
                  role="group"
                  aria-label={labels[kind]}
                  className="flex flex-wrap items-center justify-between gap-3"
                >
                  <div className="min-w-0 flex-1 basis-48">
                    <div className="flex flex-wrap items-center gap-2 font-medium">
                      <span className="text-[14px] leading-5">
                        {labels[kind]}
                      </span>
                      {view && !status.isError && (
                        <Badge
                          variant="outline"
                          className={
                            view.configured
                              ? "border-primary/20 bg-primary/10 text-[11px] leading-4 text-primary"
                              : "border-amber-500/25 bg-amber-500/10 text-[11px] leading-4 text-amber-700 dark:text-amber-300"
                          }
                        >
                          {view.configured ? "已配置" : "未配置"}
                        </Badge>
                      )}
                    </div>
                    <p className="muted mt-1 text-[12px] leading-5 break-all">
                      {view?.message}
                    </p>
                  </div>
                  <Button
                    variant="outline"
                    size="sm"
                    disabled={
                      !view?.available ||
                      view.configured ||
                      enable.isPending ||
                      status.isError
                    }
                    onClick={(event) => {
                      trigger.current = event.currentTarget;
                      setSelection(kind);
                    }}
                  >
                    打开{labels[kind]}
                  </Button>
                </div>
              );
            })}
          </div>
        </CardContent>
      </Card>
      <Dialog
        open={selection !== null}
        onOpenChange={(open) => {
          if (!open && !enable.isPending) setSelection(null);
        }}
      >
        <DialogContent
          onCloseAutoFocus={(event) => {
            if (trigger.current?.isConnected && !trigger.current.disabled) {
              event.preventDefault();
              trigger.current.focus();
            }
          }}
          onEscapeKeyDown={(event) => {
            if (enable.isPending) event.preventDefault();
          }}
          onInteractOutside={(event) => {
            if (enable.isPending) event.preventDefault();
          }}
        >
          <DialogHeader>
            <DialogTitle>
              确认打开{selection && labels[selection]}？
            </DialogTitle>
            <DialogDescription>
              将保存日志配置并重启{" "}
              {service || (selection === "php" ? "PHP-FPM" : "数据库")}
              ，可能短暂中断网站访问或数据库连接。
              {selection === "database" &&
                "慢查询阈值保留现值，未配置时设为 2 秒。"}
            </DialogDescription>
          </DialogHeader>
          <DialogFooter>
            <DialogClose asChild>
              <Button variant="outline" disabled={enable.isPending}>
                取消
              </Button>
            </DialogClose>
            <Button
              disabled={enable.isPending}
              onClick={() => selection && enable.mutate(selection)}
            >
              {enable.isPending ? (
                <>
                  <Loader2 className="animate-spin" />
                  执行中…
                </>
              ) : (
                "确认打开"
              )}
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>
    </>
  );
}
