import { useSessionMutation } from "@/lib/use-session-mutation";
import { useState } from "react";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { Loader2, Wrench } from "lucide-react";
import { toast } from "sonner";
import { api } from "@/lib/api";
import { Button } from "@/components/ui/button";
import {
  Dialog,
  DialogClose,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
  DialogTrigger,
} from "@/components/ui/dialog";

interface RepairPlan {
  revision: string;
  current_manager: string;
  proposed_manager: string;
  master_pid: number;
  restore_pid: boolean;
  message: string;
}

export function ServiceManagementRepair({ disabled }: { disabled: boolean }) {
  const [open, setOpen] = useState(false);
  const client = useQueryClient();
  const preview = useQuery({
    queryKey: ["nginx-management-repair"],
    queryFn: () => api<RepairPlan>("/services/nginx/management-repair"),
    enabled: open,
    retry: false,
    refetchOnWindowFocus: false,
  });
  const repair = useSessionMutation({
    mutationFn: (revision: string) =>
      api<{ message: string }>("/services/nginx/management-repair", {
        method: "POST",
        body: JSON.stringify({ revision }),
      }),
    onSuccess: (result) => {
      toast.success(result.message);
      setOpen(false);
    },
    onSettled: () => {
      void client.invalidateQueries({ queryKey: ["services"] });
      void client.invalidateQueries({ queryKey: ["nginx-management-repair"] });
    },
  });
  return (
    <Dialog
      open={open}
      onOpenChange={(next) => {
        if (!repair.isPending) {
          repair.reset();
          setOpen(next);
        }
      }}
    >
      <DialogTrigger asChild>
        <Button
          variant="outline"
          size="sm"
          className="mt-3"
          disabled={disabled}
        >
          <Wrench size={13} />
          修复管理方式
        </Button>
      </DialogTrigger>
      <DialogContent
        showCloseButton={!repair.isPending}
        onEscapeKeyDown={(event) => {
          if (repair.isPending) event.preventDefault();
        }}
        onInteractOutside={(event) => {
          if (repair.isPending) event.preventDefault();
        }}
      >
        <DialogHeader>
          <DialogTitle>修复 Nginx 管理方式</DialogTitle>
          <DialogDescription>
            先验证当前进程和管理配置，再切换为 LNMP init
            脚本管理。现有网站保持运行，修复失败时自动恢复原配置和 PID 记录。
          </DialogDescription>
        </DialogHeader>
        {preview.isFetching && <p role="status">正在检查可用的修复方案…</p>}
        {preview.error && (
          <p role="alert" className="notice">
            {preview.error.message}
          </p>
        )}
        {preview.data && !preview.isFetching && !preview.error && (
          <div className="small-note space-y-2">
            <p>管理方式：systemd → LNMP init 脚本</p>
            <p>当前主进程：PID {preview.data.master_pid}</p>
            <p>
              {preview.data.restore_pid
                ? "将根据已验证的主进程恢复 PID 记录。"
                : "当前 PID 记录已与主进程匹配。"}
            </p>
            <p>执行前保存备份，完成后重新验证进程与管理配置。</p>
          </div>
        )}
        {repair.error && (
          <p role="alert" className="notice">
            {repair.error.message}
          </p>
        )}
        <DialogFooter>
          <DialogClose asChild>
            <Button variant="outline" disabled={repair.isPending}>
              取消
            </Button>
          </DialogClose>
          <Button
            disabled={
              disabled ||
              !preview.data ||
              preview.isFetching ||
              Boolean(preview.error) ||
              repair.isPending
            }
            onClick={() => preview.data && repair.mutate(preview.data.revision)}
          >
            {repair.isPending && <Loader2 className="animate-spin" />}
            {repair.isPending ? "修复中…" : "确认修复"}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}
