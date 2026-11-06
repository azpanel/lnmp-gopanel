import { useState } from "react";
import { Bot, MessageCircle, UserRound, KeySquare, Send } from "lucide-react";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Switch } from "@/components/ui/switch";
import {
  PolicyFieldError,
  PolicyLabel,
  PolicySelect,
} from "@/components/backup-policy-fields";
import { api } from "@/lib/api";
import { useSessionMutation } from "@/lib/use-session-mutation";
import {
  emptyNotification,
  notificationValidation,
  type PolicySave,
} from "@/lib/backup-policies";
import { policyMessage } from "@/lib/backup-policy-feedback";

export function BackupPolicyNotifications({
  value,
  readOnly,
  fieldError,
  onChange,
}: {
  value: PolicySave;
  readOnly: boolean;
  fieldError: (field: string) => string | undefined;
  onChange: (patch: Partial<PolicySave>) => void;
}) {
  const n = value.notification ?? emptyNotification();
  const [testError, setTestError] = useState("");
  const mutation = useSessionMutation({
    mutationFn: () => {
      const identity =
        value.copy_from ??
        (value.id ? { id: value.id, revision: value.revision } : {});
      return api("/backups/notifications/test", {
        method: "POST",
        body: JSON.stringify({
          ...identity,
          notification: n,
          token_action: value.notification_token_action ?? "keep",
          token: value.notification_token ?? "",
        }),
      });
    },
  });
  const change = (patch: Partial<PolicySave>) => {
    setTestError("");
    mutation.reset();
    onChange(patch);
  };
  const disabled = readOnly || mutation.isPending;
  const action = value.notification_token_action ?? "keep";
  const tokenInput =
    action === "replace" || (!n.has_token && action !== "clear");
  const test = () => {
    const issue = notificationValidation(value, true);
    if (issue) {
      setTestError(issue.message);
      document.getElementById(issue.field)?.focus();
      return;
    }
    setTestError("");
    mutation.mutate();
  };
  return (
    <div className="policy-basic-form">
      <section className="policy-form-group" aria-label="通知设置">
        <h3>通知设置</h3>
        <label className="policy-switch" htmlFor="policy-notification-enabled">
          <span>启用通知</span>
          <Switch
            id="policy-notification-enabled"
            checked={n.enabled}
            disabled={disabled}
            onCheckedChange={(enabled) =>
              change({ notification: { ...n, enabled } })
            }
          />
        </label>
        <p className="small-note">
          每次备份完成后发送一条结果通知，包含成功、部分成功或失败。
        </p>
        <div className="policy-grid">
          <PolicySelect
            label="通知渠道"
            icon={MessageCircle}
            value={n.channel}
            options={[["telegram", "Telegram 机器人"]]}
            disabled={disabled}
            onChange={() =>
              change({ notification: { ...n, channel: "telegram" } })
            }
          />
          <label htmlFor="policy-notification-uid">
            <PolicyLabel icon={UserRound}>接收用户 UID</PolicyLabel>
            <Input
              id="policy-notification-uid"
              aria-label="接收用户 UID"
              inputMode="numeric"
              maxLength={19}
              placeholder="例如 123456789"
              value={n.uid}
              disabled={disabled}
              aria-invalid={!!fieldError("policy-notification-uid")}
              aria-describedby={
                fieldError("policy-notification-uid")
                  ? "policy-notification-uid-error"
                  : undefined
              }
              onChange={(e) =>
                change({ notification: { ...n, uid: e.target.value.trim() } })
              }
            />
            <PolicyFieldError
              id="policy-notification-uid"
              message={fieldError("policy-notification-uid")}
            />
          </label>
          {n.has_token && (
            <PolicySelect
              label="机器人 Token 设置"
              id="policy-notification-token-action"
              error={fieldError("policy-notification-token-action")}
              icon={KeySquare}
              value={action}
              options={[
                ["keep", "保留已配置 Token"],
                ["replace", "更换 Token"],
                ["clear", "清除 Token"],
              ]}
              disabled={disabled}
              onChange={(action) =>
                change({
                  notification_token_action: action as
                    "keep" | "replace" | "clear",
                  notification_token: "",
                })
              }
            />
          )}
          <label htmlFor="policy-notification-token">
            <PolicyLabel icon={Bot}>机器人 Token</PolicyLabel>
            <Input
              id="policy-notification-token"
              aria-label="机器人 Token"
              type="password"
              autoComplete="new-password"
              maxLength={256}
              placeholder={
                n.has_token && action === "keep"
                  ? "已配置，原 Token 不回显"
                  : action === "clear"
                    ? "保存后清除 Token"
                    : "填写 BotFather 提供的 Token"
              }
              value={value.notification_token ?? ""}
              disabled={disabled || !tokenInput}
              aria-invalid={!!fieldError("policy-notification-token")}
              aria-describedby={
                fieldError("policy-notification-token")
                  ? "policy-notification-token-error"
                  : undefined
              }
              onChange={(e) =>
                change({
                  notification_token: e.target.value.trim(),
                  notification_token_action:
                    !e.target.value.trim() && !n.has_token ? "keep" : "replace",
                })
              }
            />
            <PolicyFieldError
              id="policy-notification-token"
              message={fieldError("policy-notification-token")}
            />
          </label>
        </div>
        <p className="small-note">
          接收用户须先与机器人私聊并发送 /start。仅支持一个用户的数字
          UID，不支持用户名或群组。
        </p>
        <p className="small-note">
          通知失败会记录在执行历史，不影响备份结果，也不会自动重发。服务器需要能够访问
          Telegram。
        </p>
        {!readOnly && (
          <Button
            type="button"
            variant="outline"
            disabled={mutation.isPending}
            onClick={test}
          >
            <Send size={15} aria-hidden="true" />
            {mutation.isPending ? "正在发送…" : "发送测试消息"}
          </Button>
        )}
        {(testError || mutation.error) && (
          <p className="form-error" role="alert">
            {testError || policyMessage(mutation.error!.message)}
          </p>
        )}
        {mutation.isSuccess && (
          <p className="small-note" role="status">
            测试消息已发送，请在 Telegram 查看。
          </p>
        )}
      </section>
    </div>
  );
}
