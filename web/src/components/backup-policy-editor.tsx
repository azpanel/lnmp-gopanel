import { useEffect, useRef, useState } from "react";
import {
  Archive,
  Database,
  DatabaseBackup,
  Files,
  FolderOpen,
  Gauge,
  KeyRound,
  NotebookPen,
  Settings2,
  ShieldCheck,
  HardDrive,
  Eye,
  EyeOff,
  BadgeCheck,
  Tag,
} from "lucide-react";
import { Tabs } from "radix-ui";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { toast } from "sonner";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Switch } from "@/components/ui/switch";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog";
import { BackupPolicyStorage } from "@/components/backup-policy-storage";
import { BackupPolicyPathCheck } from "@/components/backup-policy-path-check";
import type { PathsResult } from "@/lib/backup-policies";
import { BackupPolicyCron } from "@/components/backup-policy-cron";
import { BackupPolicyDatabases } from "@/components/backup-policy-databases";
import { BackupPolicyFiles } from "@/components/backup-policy-files";
import {
  PolicyLabel,
  PolicyRetentionSlider,
  PolicySelect,
} from "@/components/backup-policy-fields";
import { api } from "@/lib/api";
import { useSessionMutation } from "@/lib/use-session-mutation";
import {
  newPolicy,
  policyValidation,
  type Policy,
  type PolicySave,
} from "@/lib/backup-policies";

type Tab = "basic" | "databases" | "files" | "storage";
export function BackupPolicyEditor({
  policyId,
  readOnly,
  copy = false,
  onClose,
}: {
  policyId?: string;
  readOnly: boolean;
  copy?: boolean;
  onClose: () => void;
}) {
  const [draft, setDraft] = useState<PolicySave>(newPolicy);
  const [tab, setTab] = useState<Tab>("basic");
  const [error, setError] = useState("");
  const [confirmation, setConfirmation] = useState("");
  const [showPassword, setShowPassword] = useState(false);
  const [passwordChosen, setPasswordChosen] = useState(false);
  const [paths, setPaths] = useState<PathsResult>();
  const [discard, setDiscard] = useState(false);
  const original = useRef(JSON.stringify(draft));
  const initialized = useRef(false);
  const panel = useRef<HTMLDivElement>(null);
  const notes = useRef<HTMLTextAreaElement>(null);
  useEffect(() => {
    if (!notes.current) return;
    notes.current.style.height = "auto";
    notes.current.style.height = `${Math.min(notes.current.scrollHeight, 84)}px`;
  }, [draft.notes]);
  const client = useQueryClient();
  const query = useQuery({
    queryKey: ["backup-policy", policyId],
    enabled: !!policyId,
    queryFn: ({ signal }) =>
      api<Policy>(`/backups/policies/${policyId}`, { signal }),
    refetchOnWindowFocus: false,
    staleTime: 0,
    refetchOnMount: "always",
  });
  useEffect(() => {
    if (query.data && query.isFetchedAfterMount && !initialized.current) {
      const value = {
        ...query.data,
        destinations: (query.data.destinations ?? []).map((d) =>
          d.type === "rclone" ? { ...d, path: d.path ?? "" } : d,
        ),
        ...(copy
          ? {
              id: "",
              revision: 0,
              name: `${[...query.data.name].slice(0, 76).join("")}（副本）`,
              enabled: false,
              created_at: "0001-01-01T00:00:00Z",
              updated_at: "0001-01-01T00:00:00Z",
              copy_from: { id: query.data.id, revision: query.data.revision },
            }
          : {}),
        password_action: "keep" as const,
        password: "",
      };
      setDraft(value);
      original.current = JSON.stringify(value);
      initialized.current = true;
    }
  }, [query.data, query.isFetchedAfterMount, copy]);
  useEffect(() => {
    if (readOnly) return;
    const before = (e: BeforeUnloadEvent) => {
      if (JSON.stringify(draft) !== original.current) {
        e.preventDefault();
      }
    };
    window.addEventListener("beforeunload", before);
    return () => window.removeEventListener("beforeunload", before);
  }, [draft, readOnly]);
  const mutation = useSessionMutation({
    mutationFn: () =>
      api<Policy>(
        policyId && !copy
          ? `/backups/policies/${policyId}`
          : "/backups/policies",
        {
          method: policyId && !copy ? "PUT" : "POST",
          body: JSON.stringify(draft),
        },
      ),
    onSuccess: (saved) => {
      client.setQueryData(["backup-policy", saved.id], saved);
      void client.invalidateQueries({ queryKey: ["backup-policies"] });
      void client.invalidateQueries({ queryKey: ["backup-policy", policyId] });
      toast.success(
        copy
          ? "备份政策已复制"
          : policyId
            ? "备份政策已更新"
            : "备份政策已添加",
      );
      onClose();
    },
    onError: (e) => {
      setError(e.message);
      if (
        e.message.includes("存储") ||
        e.message.includes("账户") ||
        e.message.includes("远程") ||
        e.message.includes("目的地")
      )
        setTab("storage");
      else if (
        e.message.includes("路径") ||
        e.message.includes("来源") ||
        e.message.includes("排除")
      )
        setTab("files");
      else if (e.message.includes("库") || e.message.includes("表规则"))
        setTab("databases");
      else setTab("basic");
    },
  });
  const update = <K extends keyof PolicySave>(
    field: K,
    value: PolicySave[K],
  ) => {
    setError("");
    if (field === "files" || field === "destinations") setPaths(undefined);
    setDraft((old) => ({ ...old, [field]: value }));
  };
  function close() {
    if (mutation.isPending) return;
    if (!readOnly && JSON.stringify(draft) !== original.current) {
      setDiscard(true);
      return;
    }
    onClose();
  }
  function save() {
    if (copy && draft.compression.has_password && !passwordChosen) {
      setError("请选择复制后的压缩密码处理方式");
      setTab("basic");
      return;
    }
    if (
      draft.password_action === "replace" &&
      draft.password !== confirmation
    ) {
      setError("两次输入的压缩密码不一致");
      setTab("basic");
      requestAnimationFrame(() =>
        document.getElementById("policy-password-confirm")?.focus(),
      );
      return;
    }
    if (paths?.errors.length) {
      setError(paths.errors[0].message);
      setTab(paths.errors[0].field === "destinations" ? "storage" : "files");
      return;
    }
    const failure = policyValidation(draft);
    if (failure) {
      setError(failure.message);
      setTab(failure.tab);
      requestAnimationFrame(() => {
        if (failure.field) document.getElementById(failure.field)?.focus();
        else
          panel.current?.querySelector<HTMLElement>('[role="alert"]')?.focus();
      });
      return;
    }
    setError("");
    mutation.mutate();
  }
  const disabled = readOnly || mutation.isPending;
  const loading = !!policyId && (query.isPending || !initialized.current);
  return (
    <Dialog
      open
      onOpenChange={(open) => {
        if (!open) close();
      }}
    >
      <DialogContent
        className="backup-policy-dialog"
        ref={panel}
        onEscapeKeyDown={(e) => {
          if (mutation.isPending || discard) e.preventDefault();
        }}
        onInteractOutside={(e) => {
          if (mutation.isPending || discard) e.preventDefault();
        }}
      >
        <DialogHeader className="policy-editor-header">
          <DialogTitle>
            {readOnly
              ? "查看备份政策"
              : copy
                ? "复制备份政策"
                : policyId
                  ? "编辑备份政策"
                  : "添加备份政策"}
          </DialogTitle>
          <DialogDescription>
            设置备份时间、压缩方式及数据库和文件范围。
          </DialogDescription>
        </DialogHeader>
        <div className="policy-editor-body">
          {query.error ? (
            <div className="notice" role="alert">
              {query.error.message}
              <Button variant="outline" onClick={() => void query.refetch()}>
                重试
              </Button>
            </div>
          ) : loading ? (
            <p role="status">正在读取政策…</p>
          ) : (
            <>
              <Tabs.Root
                value={tab}
                onValueChange={(v) => setTab(v as Tab)}
                className="policy-tabs"
              >
                <Tabs.List
                  className="policy-tabs-list"
                  aria-label="备份政策设置"
                >
                  <Tabs.Trigger value="basic">
                    <Settings2 size={16} aria-hidden="true" />
                    基本设置
                  </Tabs.Trigger>
                  <Tabs.Trigger value="databases">
                    <Database size={16} aria-hidden="true" />
                    数据库
                  </Tabs.Trigger>
                  <Tabs.Trigger value="storage">
                    <HardDrive size={16} aria-hidden="true" />
                    存储位置
                  </Tabs.Trigger>
                  <Tabs.Trigger value="files">
                    <FolderOpen size={16} aria-hidden="true" />
                    文件与目录
                  </Tabs.Trigger>
                </Tabs.List>
                <BackupPolicyPathCheck
                  files={draft.files}
                  destinations={draft.destinations}
                  onResult={setPaths}
                />
                {error && (
                  <p className="form-error" role="alert" tabIndex={-1}>
                    {error}
                  </p>
                )}
                <Tabs.Content
                  forceMount
                  value="basic"
                  className="policy-tab-panel"
                >
                  <div className="policy-basic-form">
                    <section
                      className="policy-form-group"
                      aria-label="政策信息"
                    >
                      <h3>政策信息</h3>
                      <div className="policy-identity-row">
                        <label htmlFor="policy-name">
                          <PolicyLabel icon={Tag}>名称</PolicyLabel>
                          <Input
                            id="policy-name"
                            maxLength={80}
                            value={draft.name}
                            disabled={disabled}
                            onChange={(e) => update("name", e.target.value)}
                          />
                        </label>
                        <label className="policy-switch">
                          <Switch
                            aria-label="启用政策"
                            checked={draft.enabled}
                            disabled={disabled}
                            onCheckedChange={(v) => update("enabled", v)}
                          />
                          <span>启用政策</span>
                        </label>
                      </div>
                      <label htmlFor="policy-notes">
                        <PolicyLabel icon={NotebookPen}>备注</PolicyLabel>
                        <textarea
                          id="policy-notes"
                          ref={notes}
                          aria-label="备注"
                          className="policy-textarea policy-notes"
                          maxLength={1000}
                          rows={1}
                          value={draft.notes}
                          disabled={disabled}
                          onChange={(e) => update("notes", e.target.value)}
                        />
                      </label>
                    </section>
                    <section
                      className="policy-form-group"
                      aria-label="备份频率"
                    >
                      <h3>备份频率</h3>
                      <BackupPolicyCron
                        value={draft.schedule}
                        readOnly={disabled}
                        onChange={(v) => update("schedule", v)}
                      />
                    </section>
                    <section
                      className="policy-form-group"
                      aria-label="压缩设置"
                    >
                      <h3>压缩设置</h3>
                      <div className="policy-grid">
                        <PolicySelect
                          label="压缩格式"
                          icon={Archive}
                          value={draft.compression.format}
                          disabled={disabled}
                          options={[
                            ["zip", "ZIP"],
                            ["7z", "7z"],
                          ]}
                          onChange={(format) =>
                            update("compression", {
                              ...draft.compression,
                              format: format as "zip" | "7z",
                              level: 5,
                            })
                          }
                        />
                        <PolicySelect
                          label="压缩等级"
                          icon={Gauge}
                          value={String(draft.compression.level)}
                          disabled={disabled}
                          options={(draft.compression.format === "7z"
                            ? [0, 1, 3, 5, 7, 9]
                            : [0, 1, 2, 3, 4, 5, 6, 7, 8, 9]
                          ).map((n) => [
                            String(n),
                            `${n}${n === 0 ? " · 不压缩" : n === 5 ? " · 标准" : n === 9 ? " · 最高" : ""}`,
                          ])}
                          onChange={(v) =>
                            update("compression", {
                              ...draft.compression,
                              level: Number(v),
                            })
                          }
                        />
                      </div>
                      <div className="policy-grid">
                        {" "}
                        <PolicySelect
                          label="压缩密码"
                          icon={ShieldCheck}
                          value={
                            copy &&
                            draft.compression.has_password &&
                            !passwordChosen
                              ? ""
                              : draft.password_action
                          }
                          disabled={disabled}
                          options={[
                            [
                              "keep",
                              draft.compression.has_password
                                ? copy
                                  ? "沿用原政策密码"
                                  : "保持已有密码（已设置）"
                                : "不加密",
                            ],
                            ["replace", "设置新密码"],
                            ["clear", "清除密码/不加密"],
                          ]}
                          onChange={(v) => {
                            setError("");
                            setPasswordChosen(true);
                            setConfirmation("");
                            setShowPassword(false);
                            setDraft((old) => ({
                              ...old,
                              password_action:
                                v as PolicySave["password_action"],
                              password: "",
                            }));
                          }}
                        />
                        {draft.password_action === "replace" && (
                          <div className="policy-password-fields">
                            <label htmlFor="policy-password">
                              <PolicyLabel icon={KeyRound}>
                                新压缩密码
                              </PolicyLabel>
                              <Input
                                id="policy-password"
                                type={showPassword ? "text" : "password"}
                                autoComplete="new-password"
                                maxLength={4096}
                                disabled={disabled}
                                value={draft.password}
                                onChange={(e) =>
                                  update("password", e.target.value)
                                }
                              />
                            </label>
                            <label htmlFor="policy-password-confirm">
                              <PolicyLabel icon={BadgeCheck}>
                                确认压缩密码
                              </PolicyLabel>
                              <Input
                                id="policy-password-confirm"
                                type={showPassword ? "text" : "password"}
                                autoComplete="new-password"
                                maxLength={4096}
                                value={confirmation}
                                disabled={disabled}
                                onChange={(e) => {
                                  setConfirmation(e.target.value);
                                  setError("");
                                }}
                              />
                            </label>
                            <Button
                              type="button"
                              variant="ghost"
                              size="sm"
                              aria-pressed={showPassword}
                              disabled={disabled}
                              onClick={() => setShowPassword(!showPassword)}
                            >
                              {showPassword ? (
                                <EyeOff size={15} />
                              ) : (
                                <Eye size={15} />
                              )}{" "}
                              {showPassword ? "隐藏密码" : "显示密码"}
                            </Button>
                          </div>
                        )}
                      </div>
                    </section>
                    <section
                      className="policy-form-group"
                      aria-label="保留规则"
                    >
                      <h3>保留规则</h3>{" "}
                      <div className="policy-grid">
                        <PolicyRetentionSlider
                          label="数据库备份保留份数"
                          icon={DatabaseBackup}
                          value={draft.retention.databases}
                          disabled={disabled}
                          onChange={(value) =>
                            update("retention", {
                              ...draft.retention,
                              databases: value,
                            })
                          }
                        />
                        <PolicyRetentionSlider
                          label="文件备份保留份数"
                          icon={Files}
                          value={draft.retention.files}
                          disabled={disabled}
                          onChange={(value) =>
                            update("retention", {
                              ...draft.retention,
                              files: value,
                            })
                          }
                        />
                      </div>
                    </section>
                  </div>
                </Tabs.Content>
                <Tabs.Content
                  forceMount
                  value="databases"
                  className="policy-tab-panel policy-databases-tab"
                >
                  <BackupPolicyDatabases
                    active={tab === "databases"}
                    value={draft.databases}
                    readOnly={disabled}
                    onChange={(v) => update("databases", v)}
                  />
                </Tabs.Content>
                <Tabs.Content
                  forceMount
                  value="storage"
                  className="policy-tab-panel"
                >
                  <BackupPolicyStorage
                    value={draft.destinations}
                    enabled={draft.enabled}
                    readOnly={disabled}
                    active={tab === "storage"}
                    onChange={(v) => update("destinations", v)}
                  />
                </Tabs.Content>
                <Tabs.Content
                  forceMount
                  value="files"
                  className="policy-tab-panel"
                >
                  <BackupPolicyFiles
                    active={tab === "files"}
                    value={draft.files}
                    readOnly={disabled}
                    onChange={(v) => update("files", v)}
                  />
                </Tabs.Content>
              </Tabs.Root>
            </>
          )}
        </div>
        <div className="policy-footer-zone">
          {discard && (
            <div
              className="policy-discard"
              role="alertdialog"
              aria-label="放弃未保存修改"
            >
              <p>放弃本次未保存的修改？</p>
              <div className="policy-inline">
                <Button variant="outline" onClick={() => setDiscard(false)}>
                  继续编辑
                </Button>
                <Button variant="destructive" onClick={onClose}>
                  放弃修改
                </Button>
              </div>
            </div>
          )}
          <DialogFooter className="policy-editor-footer">
            <span className="policy-footer-note">
              仅保存配置，暂不执行备份或自动清理。
            </span>
            <div className="policy-footer-actions">
              <Button
                variant="outline"
                disabled={mutation.isPending}
                onClick={close}
              >
                {readOnly ? "关闭" : "取消"}
              </Button>
              {!readOnly && (
                <Button
                  disabled={
                    loading || !!query.error || mutation.isPending || discard
                  }
                  onClick={save}
                >
                  {mutation.isPending ? "保存中…" : "保存政策"}
                </Button>
              )}
            </div>
          </DialogFooter>
        </div>
      </DialogContent>
    </Dialog>
  );
}
