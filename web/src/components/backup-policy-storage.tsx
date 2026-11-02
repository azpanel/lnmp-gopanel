import { Cloud, FolderOutput, UserRound, HardDrive } from "lucide-react";
import { useQuery } from "@tanstack/react-query";
import {
  PolicyFieldError,
  PolicySelect,
  PolicyLabel,
} from "@/components/backup-policy-fields";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { api } from "@/lib/api";
import type { Destination, PathDiagnostic } from "@/lib/backup-policies";
import type { BackupAccounts } from "@/components/backup-component-accounts";

export function BackupPolicyStorage({
  value,
  onChange,
  readOnly,
  active,
  enabled,
  diagnostics = [],
  submitted = false,
  onBlur,
}: {
  value: Destination[];
  onChange: (v: Destination[]) => void;
  readOnly: boolean;
  active: boolean;
  enabled: boolean;
  diagnostics?: PathDiagnostic[];
  submitted?: boolean;
  onBlur?: (index: number, control: string) => void;
}) {
  const accounts = useQuery({
    queryKey: ["backup-components"],
    queryFn: ({ signal }) =>
      api<{ items: { name: string; accounts?: BackupAccounts }[] }>(
        "/backups/components",
        { signal },
      ),
    enabled: active,
    retry: 1,
  });
  function update(index: number, next: Destination) {
    onChange(value.map((d, i) => (i === index ? next : d)));
  }
  return (
    <div className="policy-section policy-storage">
      <div className="policy-inline">
        {!readOnly && (
          <Button
            variant="outline"
            onClick={() => onChange([...value, { type: "local", path: "" }])}
            disabled={value.length >= 100}
          >
            添加存储位置
          </Button>
        )}
        <Button
          variant="ghost"
          disabled={accounts.isFetching}
          onClick={() => void accounts.refetch()}
        >
          刷新账户
        </Button>
      </div>
      <p className="small-note">
        可配置多个目的地。本轮仅保存位置，不创建目录、不连接云端。
      </p>
      {accounts.error && (
        <p role="alert" className="form-error">
          账户读取失败：{accounts.error.message}。已有选择保留。
        </p>
      )}
      {!value.length && (
        <p
          className={enabled && submitted ? "form-error" : "small-note"}
          role={enabled && submitted ? "alert" : undefined}
        >
          尚未配置存储位置
          {enabled ? "，启用政策前至少添加一个。" : "，可保存为停用草稿。"}
        </p>
      )}
      {value.map((d, index) => {
        const snapshot = accounts.data?.items.find(
          (i) => i.name === d.type,
        )?.accounts;
        const names = snapshot?.items.map((i) => i.name) ?? [];
        const selected =
          d.type === "gdrive" ? d.account : d.type === "rclone" ? d.remote : "";
        const options: [string, string][] = [
          ...new Set([...names, ...(selected ? [selected] : [])]),
        ].map((name) => [
          name,
          names.includes(name) ? name : `${name}（当前不可用）`,
        ]);
        const issues = diagnostics.filter((item) => item.index === index);
        const fieldError = (control: string) =>
          issues.find((item) => item.control === control)?.message;
        const inputProps = (control: string) => ({
          id: `policy-destination-${index}-${control}`,
          "aria-invalid": !!fieldError(control),
          "aria-describedby": fieldError(control)
            ? `policy-destination-${index}-${control}-error`
            : undefined,
          onBlur: () => onBlur?.(index, control),
        });
        return (
          <fieldset
            className="policy-destination"
            id={`policy-destination-${index}`}
            tabIndex={-1}
            key={index}
          >
            <legend>存储位置 {index + 1}</legend>
            <div className="policy-grid">
              <PolicySelect
                label="存储类型"
                icon={HardDrive}
                value={d.type}
                disabled={readOnly}
                options={[
                  ["local", "本地目录"],
                  ["gdrive", "gdrive"],
                  ["rclone", "rclone"],
                ]}
                onChange={(type) =>
                  update(
                    index,
                    type === "local"
                      ? { type, path: "" }
                      : type === "gdrive"
                        ? { type, account: "", folder_id: "" }
                        : { type: "rclone", remote: "", path: "" },
                  )
                }
              />
              {d.type === "local" ? (
                <label>
                  <PolicyLabel icon={FolderOutput}>本地目录</PolicyLabel>
                  <Input
                    {...inputProps("path")}
                    aria-label="本地目录"
                    placeholder="例如 /var/backups/websites"
                    value={d.path ?? ""}
                    disabled={readOnly}
                    onChange={(e) =>
                      update(index, { ...d, path: e.target.value })
                    }
                  />
                  <PolicyFieldError
                    id={`policy-destination-${index}-path`}
                    message={fieldError("path")}
                  />
                </label>
              ) : (
                <PolicySelect
                  label={d.type === "gdrive" ? "gdrive 账户" : "rclone 远程"}
                  icon={UserRound}
                  id={`policy-destination-${index}-${d.type === "gdrive" ? "account" : "remote"}`}
                  error={fieldError(d.type === "gdrive" ? "account" : "remote")}
                  onBlur={() =>
                    onBlur?.(index, d.type === "gdrive" ? "account" : "remote")
                  }
                  value={selected}
                  disabled={readOnly}
                  options={options}
                  onChange={(name) =>
                    update(
                      index,
                      d.type === "gdrive"
                        ? { ...d, account: name }
                        : { ...d, remote: name },
                    )
                  }
                />
              )}
              {d.type === "gdrive" && (
                <label>
                  <PolicyLabel icon={Cloud}>文件夹 ID</PolicyLabel>
                  <Input
                    {...inputProps("folder_id")}
                    aria-label="文件夹 ID"
                    placeholder="填写文件夹 ID，或 root 使用根目录"
                    value={d.folder_id}
                    disabled={readOnly}
                    onChange={(e) =>
                      update(index, { ...d, folder_id: e.target.value })
                    }
                  />
                  <PolicyFieldError
                    id={`policy-destination-${index}-folder_id`}
                    message={fieldError("folder_id")}
                  />
                </label>
              )}
              {d.type === "rclone" && (
                <label>
                  <PolicyLabel icon={FolderOutput}>远程内路径</PolicyLabel>
                  <Input
                    {...inputProps("path")}
                    aria-label="远程内路径"
                    placeholder="例如 backups/web，留空使用远程根目录"
                    value={d.path ?? ""}
                    disabled={readOnly}
                    onChange={(e) =>
                      update(index, { ...d, path: e.target.value })
                    }
                  />
                  <PolicyFieldError
                    id={`policy-destination-${index}-path`}
                    message={fieldError("path")}
                  />
                </label>
              )}
            </div>
            <div className="policy-card-feedback" aria-live="polite">
              {issues
                .filter((item) => !item.control)
                .map((item, i) => (
                  <p className="form-error" role="alert" key={i}>
                    {item.message}
                  </p>
                ))}
            </div>
            {d.type !== "local" &&
              !accounts.isPending &&
              (!snapshot ||
                snapshot.status !== "ready" ||
                !names.length ||
                (selected && !names.includes(selected))) && (
                <p className="small-note">
                  {snapshot?.status === "encrypted"
                    ? "账户配置已加密，无法识别远程。"
                    : "未识别到可用账户，请检查本机配置。"}
                  启用政策时需要有效账户，停用时保留已有选择。
                </p>
              )}
            {!readOnly && (
              <Button
                size="sm"
                variant="ghost"
                onClick={() => onChange(value.filter((_, i) => i !== index))}
                aria-label={`移除存储位置 ${index + 1}`}
              >
                移除位置
              </Button>
            )}
          </fieldset>
        );
      })}
    </div>
  );
}
