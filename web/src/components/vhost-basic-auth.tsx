import { useState } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { Loader2 } from "lucide-react";
import { toast } from "sonner";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import {
  Dialog,
  DialogClose,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog";
import { api } from "@/lib/api";
import type { Vhost, VhostBasicAuth } from "@/lib/types";

export function VhostBasicAuthDialog({
  site,
  onClose,
}: {
  site: Vhost;
  onClose: () => void;
}) {
  const client = useQueryClient();
  const [username, setUsername] = useState<string>();
  const [password, setPassword] = useState("");
  const [confirmDisable, setConfirmDisable] = useState(false);
  const queryKey = ["vhost-basic-auth", site.name];
  const endpoint = `/vhosts/basic-auth?name=${encodeURIComponent(site.name)}`;
  const query = useQuery({
    queryKey,
    queryFn: () => api<VhostBasicAuth>(endpoint),
    staleTime: 0,
    gcTime: 0,
  });
  const state = query.data;
  const name = username ?? state?.username ?? "";
  const save = useMutation({
    mutationFn: (disable: boolean) => {
      if (!disable) {
        const size = new TextEncoder().encode(password).length;
        if (size < 8 || size > 256 || /[\p{Cc}]/u.test(password))
          throw new Error("密码需为 8–256 字节，不能包含控制字符");
      }
      return api<VhostBasicAuth>(disable ? endpoint : "/vhosts/basic-auth", {
        method: disable ? "DELETE" : "PUT",
        ...(disable
          ? {}
          : {
              body: JSON.stringify({
                name: site.name,
                username: name,
                password,
              }),
            }),
      });
    },
    onSuccess: (result) => {
      client.setQueryData(queryKey, result);
      setUsername(undefined);
      setPassword("");
      setConfirmDisable(false);
      toast.success(result.message || "访问认证已更新");
      for (const key of ["vhosts", "vhost-config", "services"])
        void client.invalidateQueries({ queryKey: [key] });
    },
    onError: () => {
      void query.refetch();
    },
  });
  const writable =
    state?.site_enabled &&
    (state.status === "enabled" || state.status === "disabled");
  const busy = save.isPending;
  const statusLabels = {
    disabled: "未开启",
    enabled: "已开启",
    external: "外部配置",
    unknown: "无法确认",
  };
  return (
    <Dialog
      open
      onOpenChange={(open) => {
        if (!open && !busy) onClose();
      }}
    >
      <DialogContent
        showCloseButton={!busy}
        onEscapeKeyDown={(e) => {
          if (busy) e.preventDefault();
        }}
        onInteractOutside={(e) => {
          if (busy) e.preventDefault();
        }}
      >
        <DialogHeader>
          <DialogTitle>访问认证 · {site.domains[0] || site.name}</DialogTitle>
          <DialogDescription>
            使用一组用户名和密码保护本站，证书验证路径免认证。
          </DialogDescription>
        </DialogHeader>
        {query.isPending ? (
          <p className="small-note">
            <Loader2 className="inline animate-spin" size={14} />{" "}
            正在读取认证状态…
          </p>
        ) : query.error ? (
          <div className="notice" role="alert">
            {query.error.message}
            <Button
              variant="outline"
              size="sm"
              onClick={() => void query.refetch()}
            >
              重试读取
            </Button>
          </div>
        ) : (
          state && (
            <>
              <p>
                认证状态：<strong>{statusLabels[state.status]}</strong>
                {state.username && (
                  <span className="block small-note mt-2">
                    当前用户名：{state.username}
                  </span>
                )}
              </p>
              {!state.site_enabled && (
                <div className="notice">
                  站点已停用，当前显示保存的配置状态；请启用站点后修改认证。
                </div>
              )}
              {state.message &&
                (state.status === "external" || state.status === "unknown") && (
                  <div className="notice" role="alert">
                    {state.message}
                  </div>
                )}
              {writable && (
                <form
                  onSubmit={(e) => {
                    e.preventDefault();
                    if (!busy) save.mutate(false);
                  }}
                >
                  <fieldset
                    disabled={busy || query.isFetching}
                    className="screenshot-settings-fields"
                  >
                    <div className="form-field">
                      <Label htmlFor="vhost-auth-username">认证用户名</Label>
                      <Input
                        id="vhost-auth-username"
                        autoComplete="off"
                        required
                        maxLength={64}
                        pattern="[a-zA-Z0-9._\-]+"
                        value={name}
                        onChange={(e) => setUsername(e.target.value)}
                      />
                    </div>
                    <div className="form-field">
                      <Label htmlFor="vhost-auth-password">
                        {state.status === "enabled" ? "新认证密码" : "认证密码"}
                      </Label>
                      <Input
                        id="vhost-auth-password"
                        type="password"
                        autoComplete="new-password"
                        required
                        maxLength={256}
                        value={password}
                        onChange={(e) => setPassword(e.target.value)}
                      />
                      <p className="small-note">
                        密码为 8–256 字节，仅保存哈希，修改时需填写新密码。
                      </p>
                    </div>
                    {save.error && (
                      <div className="notice" role="alert">
                        {save.error.message}
                      </div>
                    )}
                    {confirmDisable && (
                      <div className="notice">
                        确认关闭访问认证？关闭后，访问本站将不再需要这组凭证。
                      </div>
                    )}
                    <DialogFooter>
                      <DialogClose asChild>
                        <Button type="button" variant="outline">
                          关闭窗口
                        </Button>
                      </DialogClose>
                      {state.status === "enabled" && (
                        <Button
                          type="button"
                          variant="destructive"
                          onClick={() => {
                            if (confirmDisable) save.mutate(true);
                            else setConfirmDisable(true);
                          }}
                        >
                          {confirmDisable ? "确认关闭认证" : "关闭认证"}
                        </Button>
                      )}
                      <Button type="submit">
                        {busy ? (
                          <>
                            <Loader2 className="animate-spin" />
                            正在检查并重载…
                          </>
                        ) : state.status === "enabled" ? (
                          "修改凭证"
                        ) : (
                          "开启认证"
                        )}
                      </Button>
                    </DialogFooter>
                  </fieldset>
                </form>
              )}
            </>
          )
        )}
      </DialogContent>
    </Dialog>
  );
}
