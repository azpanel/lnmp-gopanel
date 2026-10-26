import { useState } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { Camera, ImageIcon, Loader2, RefreshCw, Settings2 } from "lucide-react";
import { toast } from "sonner";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Switch } from "@/components/ui/switch";
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
import type { ScreenshotSnapshot, Vhost, WebsiteScreenshot } from "@/lib/types";

function imageURL(item: WebsiteScreenshot) {
  return `/api/v1/vhosts/screenshot/image?name=${encodeURIComponent(item.name)}&v=${encodeURIComponent(item.image_version || "")}`;
}
function timestamp(value: string | null) {
  return value
    ? new Date(value).toLocaleString("zh-CN", { hour12: false })
    : "";
}
function Preview({
  site,
  item,
  onSettings,
  onEnlarge,
  onRefresh,
  pending,
  compact = false,
}: {
  site: Vhost;
  item?: WebsiteScreenshot;
  onSettings: () => void;
  onEnlarge: () => void;
  onRefresh: () => void;
  pending: boolean;
  compact?: boolean;
}) {
  const [failedImage, setFailedImage] = useState("");
  const busy = item?.status === "queued" || item?.status === "running";
  const hasImage = Boolean(
    item?.image_version && failedImage !== item.image_version,
  );
  return (
    <div
      className={`website-preview${compact ? " website-preview-compact" : ""}`}
    >
      <button
        type="button"
        className="website-preview-image"
        title={item?.captured_at ? timestamp(item.captured_at) : undefined}
        onClick={hasImage ? onEnlarge : onSettings}
        aria-label={
          hasImage
            ? `放大网站截图 ${site.domains[0] || site.name}`
            : `配置网站截图 ${site.domains[0] || site.name}`
        }
      >
        {hasImage && item ? (
          <img
            src={imageURL(item)}
            width={1440}
            height={900}
            loading="lazy"
            alt={`${site.domains[0] || site.name} 的网站截图`}
            onError={() => setFailedImage(item.image_version || "")}
          />
        ) : (
          <span className="website-preview-placeholder">
            {busy ? (
              <Loader2 size={23} className="animate-spin" />
            ) : (
              <ImageIcon size={23} />
            )}
            {!compact && (
              <span>
                {busy
                  ? item?.status === "queued"
                    ? "等待截图…"
                    : "正在截图…"
                  : item
                    ? "暂无可用截图"
                    : "配置网站截图"}
              </span>
            )}
          </span>
        )}
        {hasImage && busy && (
          <span className="website-preview-progress">
            <Loader2 size={12} className="animate-spin" />
            {item?.status === "queued" ? "排队中" : "更新中"}
          </span>
        )}
      </button>
      <div className="website-preview-footer">
        {!compact && (
          <span>
            {item?.captured_at
              ? timestamp(item.captured_at)
              : "桌面首屏 · 1440 × 900"}
          </span>
        )}
        {item && (
          <Button
            variant="ghost"
            size="xs"
            aria-label={`重新截图 ${site.domains[0] || site.name}`}
            title="重新截图"
            disabled={busy || pending}
            onClick={onRefresh}
          >
            <RefreshCw size={12} />
            {!compact && "重新截图"}
          </Button>
        )}
      </div>
      {item?.error && (
        <p
          className="site-detail-error website-preview-error"
          title={item.error}
        >
          {item.error}
          {item.image_version && "（保留上次成功截图）"}
        </p>
      )}
    </div>
  );
}

export function useWebsiteScreenshots(enabled: boolean) {
  const client = useQueryClient();
  const [site, setSite] = useState<Vhost | null>(null);
  const [enlarged, setEnlarged] = useState<WebsiteScreenshot | null>(null);
  const [url, setURL] = useState("");
  const [language, setLanguage] = useState("zh-CN");
  const [loadTimeoutSeconds, setLoadTimeoutSeconds] = useState("30");
  const [authEnabled, setAuthEnabled] = useState(false);
  const [username, setUsername] = useState("");
  const [password, setPassword] = useState("");
  const query = useQuery({
    queryKey: ["vhost-screenshots"],
    queryFn: () => api<ScreenshotSnapshot>("/vhosts/screenshots"),
    enabled,
    refetchInterval: (q) =>
      Object.values(q.state.data?.items ?? {}).some(
        (item) => item.status === "queued" || item.status === "running",
      )
        ? 1000
        : false,
  });
  const update = (item: WebsiteScreenshot) => {
    client.setQueryData<ScreenshotSnapshot>(["vhost-screenshots"], (old) =>
      old ? { ...old, items: { ...old.items, [item.name]: item } } : old,
    );
    void client.invalidateQueries({ queryKey: ["vhost-screenshots"] });
  };
  const save = useMutation({
    mutationFn: () =>
      api<WebsiteScreenshot>(
        `/vhosts/screenshot?name=${encodeURIComponent(site!.name)}`,
        {
          method: "PUT",
          body: JSON.stringify({
            url,
            language,
            load_timeout_seconds: Number(loadTimeoutSeconds),
            auth_enabled: authEnabled,
            username,
            ...(password ? { password } : {}),
          }),
        },
      ),
    onSuccess: (item) => {
      update(item);
      setSite(null);
      setPassword("");
      toast.success("截图设置已保存，正在生成画面");
    },
  });
  const clear = useMutation({
    mutationFn: () =>
      api<void>(`/vhosts/screenshot?name=${encodeURIComponent(site!.name)}`, {
        method: "DELETE",
      }),
    onSuccess: () => {
      client.setQueryData<ScreenshotSnapshot>(["vhost-screenshots"], (old) => {
        if (!old || !site) return old;
        const items = { ...old.items };
        delete items[site.name];
        return { ...old, items };
      });
      setSite(null);
      setPassword("");
      toast.success("已清除截图设置、认证信息和图片");
    },
  });
  const refresh = useMutation({
    mutationFn: (name: string) =>
      api<WebsiteScreenshot>(
        `/vhosts/screenshot/refresh?name=${encodeURIComponent(name)}`,
        { method: "POST" },
      ),
    onSuccess: update,
    onError: (error) => toast.error(error.message),
  });
  const openSettings = (target: Vhost) => {
    if (!query.data) {
      if (query.error) toast.error(query.error.message);
      return;
    }
    const item = query.data?.items[target.name];
    setURL(item?.url || "");
    setLanguage(item?.language || "zh-CN");
    setLoadTimeoutSeconds(String(item?.load_timeout_seconds || 30));
    setAuthEnabled(item?.auth_enabled || false);
    setUsername(item?.username || "");
    setPassword("");
    save.reset();
    clear.reset();
    setSite(target);
  };
  const busy = save.isPending || clear.isPending;
  const saved = site ? query.data?.items[site.name] : undefined;
  return {
    notice: query.error?.message || query.data?.browser_error,
    settings: (target: Vhost) => (
      <Button
        variant="outline"
        size="sm"
        disabled={query.isPending || query.isError}
        onClick={() => openSettings(target)}
      >
        <Settings2 size={13} />
        截图设置
      </Button>
    ),
    preview: (target: Vhost, compact = false) => (
      <Preview
        key={target.name}
        site={target}
        compact={compact}
        item={query.data?.items[target.name]}
        pending={refresh.isPending && refresh.variables === target.name}
        onSettings={() => openSettings(target)}
        onEnlarge={() => setEnlarged(query.data!.items[target.name])}
        onRefresh={() => refresh.mutate(target.name)}
      />
    ),
    dialogs: (
      <>
        <Dialog
          open={site !== null}
          onOpenChange={(open) => {
            if (!open && !busy) {
              setSite(null);
              setPassword("");
            }
          }}
        >
          <DialogContent
            showCloseButton={!busy}
            className="screenshot-settings-dialog"
            onEscapeKeyDown={(e) => {
              if (busy) e.preventDefault();
            }}
            onInteractOutside={(e) => {
              if (busy) e.preventDefault();
            }}
          >
            <DialogHeader>
              <DialogTitle>
                截图设置 · {site?.domains[0] || site?.name}
              </DialogTitle>
              <DialogDescription>
                保存后截取网站桌面首屏，设置与认证信息仅保存在本机。
              </DialogDescription>
            </DialogHeader>
            <form
              onSubmit={(e) => {
                e.preventDefault();
                save.mutate();
              }}
            >
              <fieldset disabled={busy} className="screenshot-settings-fields">
                <div className="form-field">
                  <Label htmlFor="screenshot-url">截图地址</Label>
                  <Input
                    id="screenshot-url"
                    type="url"
                    required
                    maxLength={2048}
                    placeholder="https://example.com/"
                    value={url}
                    onChange={(e) => setURL(e.target.value)}
                  />
                </div>
                <div className="form-field">
                  <Label htmlFor="screenshot-language">浏览器语言</Label>
                  <Input
                    id="screenshot-language"
                    required
                    maxLength={64}
                    pattern="[A-Za-z]{2,8}(-[A-Za-z0-9]{1,8})*"
                    placeholder="zh-CN"
                    value={language}
                    onChange={(e) => setLanguage(e.target.value)}
                    aria-describedby="screenshot-language-help"
                  />
                  <p id="screenshot-language-help" className="small-note">
                    例如
                    zh-CN（简体中文）、en-US（英语），用于网站语言及日期、数字格式。
                  </p>
                </div>
                <div className="form-field">
                  <Label htmlFor="screenshot-timeout">最长加载时间（秒）</Label>
                  <Input
                    id="screenshot-timeout"
                    type="number"
                    required
                    min={5}
                    max={300}
                    step={1}
                    value={loadTimeoutSeconds}
                    onChange={(e) => setLoadTimeoutSeconds(e.target.value)}
                    aria-describedby="screenshot-timeout-help"
                  />
                  <p id="screenshot-timeout-help" className="small-note">
                    默认 30 秒，可设为 5–300 秒。等待
                    CSS、图片和字体等资源加载稳定后截图；加载较慢时可适当增加，超时保留上次成功截图。
                  </p>
                </div>
                <div className="screenshot-auth-toggle">
                  <Switch
                    id="screenshot-auth"
                    checked={authEnabled}
                    onCheckedChange={setAuthEnabled}
                  />
                  <Label htmlFor="screenshot-auth">使用 Basic Auth</Label>
                </div>
                {authEnabled && (
                  <>
                    <div className="form-field">
                      <Label htmlFor="screenshot-username">
                        Basic Auth 用户名
                      </Label>
                      <Input
                        id="screenshot-username"
                        required
                        maxLength={256}
                        autoComplete="off"
                        value={username}
                        onChange={(e) => setUsername(e.target.value)}
                      />
                    </div>
                    <div className="form-field">
                      <Label htmlFor="screenshot-password">
                        Basic Auth 密码
                      </Label>
                      <Input
                        id="screenshot-password"
                        type="password"
                        maxLength={1024}
                        autoComplete="new-password"
                        required={!saved?.password_saved}
                        placeholder={
                          saved?.password_saved
                            ? "密码已保存，留空保留"
                            : "请输入密码"
                        }
                        value={password}
                        onChange={(e) => setPassword(e.target.value)}
                      />
                      <p className="small-note">
                        更换用户名或目标来源时，需要重新填写密码。关闭认证会清除已保存的凭据。
                      </p>
                    </div>
                  </>
                )}
                {query.data?.browser_error && (
                  <div className="notice">{query.data.browser_error}</div>
                )}
                {(save.error || clear.error) && (
                  <div className="notice" role="alert">
                    {save.error?.message || clear.error?.message}
                  </div>
                )}
                <DialogFooter>
                  {saved && (
                    <Button
                      type="button"
                      variant="ghost"
                      className="screenshot-clear"
                      onClick={() => clear.mutate()}
                    >
                      清除设置
                    </Button>
                  )}
                  <DialogClose asChild>
                    <Button type="button" variant="outline">
                      取消
                    </Button>
                  </DialogClose>
                  <Button type="submit">
                    {busy ? (
                      <Loader2 size={14} className="animate-spin" />
                    ) : (
                      <Camera size={14} />
                    )}
                    保存并截图
                  </Button>
                </DialogFooter>
              </fieldset>
            </form>
          </DialogContent>
        </Dialog>
        <Dialog
          open={enlarged !== null}
          onOpenChange={(open) => {
            if (!open) setEnlarged(null);
          }}
        >
          <DialogContent className="screenshot-image-dialog">
            <DialogHeader>
              <DialogTitle>网站截图 · {enlarged?.name}</DialogTitle>
              <DialogDescription>
                {enlarged?.captured_url || enlarged?.url} ·{" "}
                {timestamp(enlarged?.captured_at || null)}
              </DialogDescription>
            </DialogHeader>
            {enlarged && (
              <img
                src={imageURL(enlarged)}
                width={1440}
                height={900}
                alt={`${enlarged.name} 的完整网站截图`}
              />
            )}
          </DialogContent>
        </Dialog>
      </>
    ),
  };
}
