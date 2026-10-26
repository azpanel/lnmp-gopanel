import { useEffect, useId, useRef, useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { Popover } from "radix-ui";
import { api } from "@/lib/api";
import type { CronPreview, CronTask } from "@/lib/types";

export function CronSchedule({
  task,
  revision,
}: {
  task: CronTask;
  revision: string;
}) {
  const [open, setOpen] = useState(false);
  const id = useId();
  const closeTimer = useRef<ReturnType<typeof setTimeout> | undefined>(
    undefined,
  );
  useEffect(() => () => clearTimeout(closeTimer.current), []);
  const show = () => {
    clearTimeout(closeTimer.current);
    setOpen(true);
  };
  const hide = () => {
    clearTimeout(closeTimer.current);
    closeTimer.current = setTimeout(() => setOpen(false), 150);
  };
  const query = useQuery({
    queryKey: ["cron-preview", revision, task.line, task.schedule],
    queryFn: () => api<CronPreview>(`/cron/preview?line=${task.line}`),
    enabled: open,
    retry: false,
    staleTime: 0,
    refetchOnWindowFocus: false,
  });
  const preview = query.data;
  const changed = preview && preview.schedule !== task.schedule;
  return (
    <Popover.Root open={open} onOpenChange={setOpen}>
      <Popover.Anchor asChild>
        <button
          type="button"
          className="cron-schedule"
          aria-label={`查看 ${task.schedule} 的执行时间`}
          aria-describedby={open ? id : undefined}
          onMouseEnter={show}
          onMouseLeave={hide}
          onFocus={show}
          onBlur={hide}
          onClick={show}
        >
          {task.schedule}
        </button>
      </Popover.Anchor>
      <Popover.Portal>
        <Popover.Content
          id={id}
          role="tooltip"
          className="cron-preview"
          side="top"
          align="start"
          sideOffset={8}
          collisionPadding={16}
          onOpenAutoFocus={(event) => event.preventDefault()}
          onCloseAutoFocus={(event) => event.preventDefault()}
          onMouseEnter={show}
          onMouseLeave={hide}
        >
          <strong>接下来 5 次预计执行时间</strong>
          {query.isFetching || query.isPending ? (
            <p role="status">正在计算…</p>
          ) : query.error ? (
            <p role="alert">{query.error.message}，请再次悬停重试。</p>
          ) : changed ? (
            <p>任务配置已变化，请刷新列表。</p>
          ) : preview ? (
            <>
              {preview.timezone && (
                <p className="cron-preview-zone">时区：{preview.timezone}</p>
              )}
              {preview.times.length > 0 && (
                <ol>
                  {preview.times.map((time) => (
                    <li key={time}>
                      <time dateTime={time}>
                        {`${time.slice(0, 19).replace("T", " ")} ${time.endsWith("Z") ? "UTC" : time.slice(19)}`}
                      </time>
                    </li>
                  ))}
                </ol>
              )}
              {preview.message && <p>{preview.message}</p>}
            </>
          ) : null}
        </Popover.Content>
      </Popover.Portal>
    </Popover.Root>
  );
}
