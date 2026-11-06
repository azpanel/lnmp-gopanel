import { useCallback, useEffect, useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { Tabs } from "radix-ui";
import { RefreshCw } from "lucide-react";
import { Button } from "@/components/ui/button";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog";
import { api, APIError } from "@/lib/api";
import {
  destinationLabel,
  readableBytes,
  type Policy,
  type PolicySummary,
} from "@/lib/backup-policies";
import { backupRunTime } from "@/lib/backup-runs";
import { artifactPage, type BackupArtifactList } from "@/lib/backup-artifacts";

function BackupArtifactLocation({
  policy,
  index,
  onConflict,
}: {
  policy: Policy;
  index: number;
  onConflict: () => void;
}) {
  const [page, setPage] = useState(1);
  const query = useQuery({
    queryKey: ["backup-artifacts", policy.id, policy.revision, index],
    queryFn: ({ signal }) =>
      api<BackupArtifactList>(
        `/backups/policies/${policy.id}/artifacts?revision=${policy.revision}&destination_index=${index}`,
        { signal },
      ),
    retry: false,
    refetchOnWindowFocus: false,
    staleTime: 0,
  });
  const conflict =
    query.error instanceof APIError && query.error.status === 409;
  useEffect(() => {
    if (conflict) onConflict();
  }, [conflict, onConflict]);
  const result = artifactPage(query.data?.items ?? [], page);
  const refresh = () => {
    setPage(1);
    void query.refetch();
  };
  return (
    <div className="backup-artifact-location">
      <div className="backup-artifact-toolbar">
        <p className="small-note backup-artifact-destination">
          {destinationLabel(policy.destinations[index])}
        </p>
        <Button
          size="sm"
          variant="outline"
          disabled={query.isFetching}
          onClick={refresh}
        >
          <RefreshCw
            size={14}
            className={query.isFetching ? "animate-spin" : ""}
            aria-hidden="true"
          />
          刷新备份
        </Button>
      </div>
      {query.error && (
        <div className="notice" role="alert">
          <p>
            {conflict
              ? "存储配置已变化，正在重新读取政策…"
              : `备份文件查询失败：${query.error.message}`}
          </p>
          {!conflict && (
            <Button
              size="sm"
              variant="outline"
              onClick={refresh}
              disabled={query.isFetching}
            >
              重试
            </Button>
          )}
          {query.data && <p className="small-note">以下为上次查询结果。</p>}
        </div>
      )}
      {query.isFetching && (
        <p role="status" className="small-note">
          正在读取备份文件…
        </p>
      )}
      {query.data && (
        <>
          <p className="small-note">
            {query.data.truncated ? "已展示" : "共"} {query.data.items.length}{" "}
            个备份文件 · 查询时间：{backupRunTime(query.data.checked_at)}
          </p>
          {(query.data.warning || query.data.truncated) && (
            <p className="notice" role="status">
              {query.data.warning || "仅展示部分匹配的备份文件。"}
            </p>
          )}
          {!query.data.items.length ? (
            <p className="backup-artifact-empty">
              此存储位置暂无当前政策的备份文件。
            </p>
          ) : (
            <>
              <table className="backup-artifact-table" aria-label="备份文件">
                <thead>
                  <tr>
                    <th>文件名</th>
                    <th>类别</th>
                    <th>大小</th>
                    <th>备份时间</th>
                  </tr>
                </thead>
                <tbody>
                  {result.items.map((file) => (
                    <tr key={file.object}>
                      <td className="backup-artifact-name">{file.name}</td>
                      <td>
                        <span className="backup-artifact-mobile-label">
                          类别：
                        </span>
                        {file.category === "databases" ? "数据库" : "文件"}
                      </td>
                      <td>
                        <span className="backup-artifact-mobile-label">
                          大小：
                        </span>
                        {file.bytes != null
                          ? readableBytes(file.bytes)
                          : file.size_display
                            ? `约 ${file.size_display}`
                            : "—"}
                      </td>
                      <td>
                        <span className="backup-artifact-mobile-label">
                          备份时间：
                        </span>
                        {backupRunTime(file.backup_at)}
                      </td>
                    </tr>
                  ))}
                </tbody>
              </table>
              {result.pages > 1 && (
                <div className="backup-artifact-pagination">
                  <Button
                    size="sm"
                    variant="outline"
                    disabled={result.current <= 1}
                    onClick={() => setPage(result.current - 1)}
                  >
                    上一页
                  </Button>
                  <span>
                    第 {result.current} / {result.pages} 页
                  </span>
                  <Button
                    size="sm"
                    variant="outline"
                    disabled={result.current >= result.pages}
                    onClick={() => setPage(result.current + 1)}
                  >
                    下一页
                  </Button>
                </div>
              )}
            </>
          )}
        </>
      )}
    </div>
  );
}

function BackupArtifactTabs({
  policy,
  onConflict,
}: {
  policy: Policy;
  onConflict: () => void;
}) {
  const [selected, setSelected] = useState("0");
  if (!policy.destinations.length) return <p>尚未配置存储位置。</p>;
  return (
    <Tabs.Root
      value={selected}
      onValueChange={setSelected}
      className="backup-artifact-tabs"
    >
      <Tabs.List
        aria-label="备份存储位置"
        className="policy-tabs-list backup-artifact-tabs-list"
      >
        {policy.destinations.map((destination, index) => (
          <Tabs.Trigger
            key={index}
            value={String(index)}
            title={destinationLabel(destination)}
          >
            {destination.type === "local" ? "本地" : destination.type}{" "}
            {index + 1}
          </Tabs.Trigger>
        ))}
      </Tabs.List>
      {policy.destinations.map((_, index) => (
        <Tabs.Content
          key={index}
          value={String(index)}
          className="backup-artifact-tab-content"
        >
          {selected === String(index) && (
            <BackupArtifactLocation
              policy={policy}
              index={index}
              onConflict={onConflict}
            />
          )}
        </Tabs.Content>
      ))}
    </Tabs.Root>
  );
}

export function BackupArtifactsDialog({
  policy,
  onClose,
}: {
  policy: PolicySummary;
  onClose: () => void;
}) {
  const query = useQuery({
    queryKey: ["backup-artifact-policy", policy.id],
    queryFn: ({ signal }) =>
      api<Policy>(`/backups/policies/${policy.id}`, { signal }),
    refetchOnWindowFocus: false,
    gcTime: 0,
    retry: 1,
  });
  const reload = useCallback(() => {
    void query.refetch();
  }, [query.refetch]);
  return (
    <Dialog
      open
      onOpenChange={(open) => {
        if (!open) onClose();
      }}
    >
      <DialogContent className="backup-artifact-dialog">
        <DialogHeader>
          <DialogTitle>
            查看备份 · {query.data?.name ?? policy.name}
          </DialogTitle>
          <DialogDescription>
            按存储位置查看当前政策的备份文件。
          </DialogDescription>
        </DialogHeader>
        {query.isPending ? (
          <p role="status">正在读取存储配置…</p>
        ) : (
          <>
            {query.error && (
              <div className="notice" role="alert">
                存储配置读取失败：{query.error.message}
                <Button size="sm" variant="outline" onClick={reload}>
                  重试
                </Button>
              </div>
            )}
            {query.data && (
              <BackupArtifactTabs
                key={`${query.data.id}-${query.data.revision}`}
                policy={query.data}
                onConflict={reload}
              />
            )}
          </>
        )}
      </DialogContent>
    </Dialog>
  );
}
