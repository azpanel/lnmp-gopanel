import { useEffect, useState } from "react";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { api } from "./api";
import { sessionStream } from "./session-stream";
import type { VhostJob, VhostJobLine } from "./vhost-create";

export function useVhostJob(id: string | null, open: boolean) {
  const client = useQueryClient();
  const [live, setLive] = useState(false);
  const task = useQuery({
    queryKey: ["vhost-job", id],
    queryFn: () => api<VhostJob>(`/vhosts/jobs/${encodeURIComponent(id!)}`),
    enabled: open && Boolean(id),
    refetchInterval: (query) =>
      !live && query.state.data?.status === "running" ? 2000 : false,
  });
  const running = task.data?.status === "running";
  useEffect(() => {
    if (!open || !id || !running) return;
    const key = ["vhost-job", id];
    const stream = sessionStream({
      url: () => `/api/v1/vhosts/jobs/${encodeURIComponent(id)}/events`,
      onState: (state) => setLive(state === "live"),
      beforeReconnect: async () => {
        const job = await client.fetchQuery({
          queryKey: key,
          queryFn: () =>
            api<VhostJob>(`/vhosts/jobs/${encodeURIComponent(id)}`),
          staleTime: 0,
        });
        return job.status === "running";
      },
      events: {
        snapshot: (event) =>
          client.setQueryData(key, JSON.parse(event.data) as VhostJob),
        status: (event) => {
          const update = JSON.parse(event.data) as VhostJob;
          client.setQueryData<VhostJob>(key, (old) => ({
            ...update,
            lines: old?.lines || [],
          }));
        },
        log: (event) => {
          const line = JSON.parse(event.data) as VhostJobLine;
          client.setQueryData<VhostJob>(key, (old) =>
            old
              ? {
                  ...old,
                  lines: [
                    ...old.lines.filter((previous) => previous.id !== line.id),
                    line,
                  ]
                    .sort((a, b) => a.id - b.id)
                    .slice(-1000),
                }
              : old,
          );
        },
        complete: () => {
          stream.stop();
          setLive(false);
          void client.invalidateQueries({ queryKey: key });
        },
      },
    });
    return () => {
      stream.stop();
      setLive(false);
    };
  }, [open, id, running, client]);
  // Refresh metadata after every completed site, including polling fallback.
  useEffect(() => {
    if (!task.data) return;
    for (const key of ["vhosts", "services", "vhost-config", "vhost-jobs"])
      void client.invalidateQueries({ queryKey: [key] });
    // Only progress transitions should invalidate queries.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [id, task.data?.completed, task.data?.status]);
  return { ...task, live };
}
