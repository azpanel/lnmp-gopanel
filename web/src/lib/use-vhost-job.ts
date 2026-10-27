import { useEffect, useState } from "react";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { api, fetchSession } from "./api";
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
    const source = new EventSource(
      `/api/v1/vhosts/jobs/${encodeURIComponent(id)}/events`,
    );
    source.onopen = () => setLive(true);
    source.addEventListener("snapshot", (event) =>
      client.setQueryData(
        key,
        JSON.parse((event as MessageEvent).data) as VhostJob,
      ),
    );
    source.addEventListener("status", (event) => {
      const update = JSON.parse((event as MessageEvent).data) as VhostJob;
      client.setQueryData<VhostJob>(key, (old) => ({
        ...update,
        lines: old?.lines || [],
      }));
    });
    source.addEventListener("log", (event) => {
      const line = JSON.parse((event as MessageEvent).data) as VhostJobLine;
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
    });
    source.addEventListener("complete", () => {
      source.close();
      setLive(false);
      void client.invalidateQueries({ queryKey: key });
    });
    source.addEventListener("auth_expired", () => {
      source.close();
      window.dispatchEvent(new Event("session-expired"));
    });
    source.onerror = () => {
      setLive(false);
      void fetchSession().catch(() => {});
    };
    return () => {
      source.close();
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
