import { useEffect, useState } from "react";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { FolderTree } from "lucide-react";
import { api } from "@/lib/api";
import { useSessionMutation } from "@/lib/use-session-mutation";
import type { VhostSnapshot } from "@/lib/types";
import { Button } from "@/components/ui/button";
import { Checkbox } from "@/components/ui/checkbox";
import { Input } from "@/components/ui/input";
import { PolicySelect } from "@/components/backup-policy-fields";

export function BackupPolicySiteSources({
  onAdd,
}: {
  onAdd: (paths: string[]) => void;
}) {
  const [open, setOpen] = useState(false);
  const [search, setSearch] = useState("");
  const [selected, setSelected] = useState<Record<string, boolean>>({});
  const [ranges, setRanges] = useState<Record<string, string>>({});
  const client = useQueryClient();
  const query = useQuery({
    queryKey: ["backup-sites"],
    enabled: open,
    queryFn: ({ signal }) => api<VhostSnapshot>("/vhosts", { signal }),
    refetchInterval: (q) => (q.state.data?.status === "loading" ? 1000 : false),
  });
  const refresh = useSessionMutation({
    mutationFn: () => api<VhostSnapshot>("/vhosts/refresh", { method: "POST" }),
    onSuccess: (result) => {
      client.setQueryData(["backup-sites"], result);
      void query.refetch();
    },
  });
  const trigger = refresh.mutate;
  useEffect(() => {
    if (open && query.data?.status === "idle") trigger();
  }, [open, query.data?.status, trigger]);
  const sites = query.data?.sites ?? [];
  const choices = sites.flatMap((site) =>
    site.roots.map((root, i) => {
      const key = `${site.name}:${i}`;
      const path =
        root.public_parent && ranges[key] === "public"
          ? `${root.path.replace(/\/$/, "")}/public`
          : root.path;
      return { site, root, key, path };
    }),
  );
  return (
    <div className="policy-site-picker">
      <Button
        variant="outline"
        aria-expanded={open}
        onClick={() => setOpen(!open)}
      >
        从网站添加
      </Button>
      {open && (
        <section aria-label="选择网站目录" className="policy-site-choices">
          <div className="policy-inline">
            <Input
              aria-label="搜索网站目录"
              placeholder="搜索站点名称或域名"
              value={search}
              onChange={(e) => setSearch(e.target.value)}
            />
            <Button
              variant="ghost"
              disabled={refresh.isPending || query.data?.status === "loading"}
              onClick={() => refresh.mutate()}
            >
              刷新网站
            </Button>
          </div>
          {(query.isPending || query.data?.status === "loading") && (
            <p role="status">正在读取网站目录…</p>
          )}
          {(query.error || refresh.error || query.data?.error) && (
            <p role="alert" className="form-error">
              网站目录读取失败：
              {query.error?.message ||
                refresh.error?.message ||
                query.data?.error}
            </p>
          )}
          {sites
            .filter((s) =>
              `${s.name} ${s.domains.join(" ")}`
                .toLowerCase()
                .includes(search.toLowerCase()),
            )
            .map((site) => (
              <div key={site.name} className="policy-site-choice">
                <strong>
                  {site.domains.join(", ") || site.name}
                  {!site.enabled ? "（已停用）" : ""}
                </strong>
                {(!site.roots.length || site.error) && (
                  <p className="small-note">
                    {site.error || "该站点没有可用的本地目录。"}
                  </p>
                )}
                {choices
                  .filter((c) => c.site === site)
                  .map((c) => (
                    <div key={c.key}>
                      <label className="policy-site-path">
                        <Checkbox
                          aria-label={`选择网站目录 ${c.site.name} ${c.root.path || c.root.configured}`}
                          checked={!!selected[c.key]}
                          disabled={
                            !!c.root.error ||
                            !!site.error ||
                            !c.path.startsWith("/")
                          }
                          onCheckedChange={(checked) =>
                            setSelected((old) => ({
                              ...old,
                              [c.key]: checked === true,
                            }))
                          }
                        />
                        <span>{c.root.error || c.path || "目录无法解析"}</span>
                      </label>
                      {c.root.public_parent && (
                        <PolicySelect
                          label={`目录范围 ${site.name}`}
                          icon={FolderTree}
                          value={ranges[c.key] ?? "project"}
                          options={[
                            ["project", "项目完整目录"],
                            ["public", "仅 public 目录"],
                          ]}
                          onChange={(v) =>
                            setRanges((old) => ({ ...old, [c.key]: v }))
                          }
                        />
                      )}
                    </div>
                  ))}
              </div>
            ))}
          {!query.isPending &&
            query.data?.status !== "loading" &&
            !sites.length && (
              <p className="small-note">
                没有可添加的网站目录，可继续手动填写路径。
              </p>
            )}
          <Button
            size="sm"
            disabled={
              !choices.some(
                (c) => selected[c.key] && !c.root.error && !c.site.error,
              )
            }
            onClick={() => {
              onAdd([
                ...new Set(
                  choices
                    .filter(
                      (c) =>
                        selected[c.key] &&
                        !c.root.error &&
                        !c.site.error &&
                        c.path.startsWith("/"),
                    )
                    .map((c) => c.path),
                ),
              ]);
              setSelected({});
              setOpen(false);
            }}
          >
            添加所选目录
          </Button>
        </section>
      )}
    </div>
  );
}
