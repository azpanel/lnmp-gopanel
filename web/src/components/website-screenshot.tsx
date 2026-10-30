import { useSessionMutation } from "@/lib/use-session-mutation";
import { useState, type ReactNode } from "react";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import {
  Camera,
  CircleHelp,
  ImageIcon,
  Loader2,
  RefreshCw,
  Settings2,
} from "lucide-react";
import { toast } from "sonner";
import { Tooltip } from "radix-ui";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Switch } from "@/components/ui/switch";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select";
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
import { ConsequenceConfirmation } from "@/components/consequence-confirmation";
import type { ScreenshotSnapshot, Vhost, WebsiteScreenshot } from "@/lib/types";

function ScreenshotFieldLabel({
  htmlFor,
  children,
  help,
}: {
  htmlFor: string;
  children: ReactNode;
  help: string;
}) {
  const [open, setOpen] = useState(false);
  return (
    <div className="screenshot-field-label">
      <Label htmlFor={htmlFor}>{children}</Label>
      <Tooltip.Provider delayDuration={150}>
        <Tooltip.Root open={open} onOpenChange={setOpen}>
          <Tooltip.Trigger asChild>
            <Button
              type="button"
              variant="ghost"
              size="icon"
              className="screenshot-help-button"
              aria-label={`${children}说明`}
              onClick={(event) => {
                event.preventDefault();
                setOpen(true);
              }}
            >
              <CircleHelp size={15} aria-hidden="true" />
            </Button>
          </Tooltip.Trigger>
          <Tooltip.Portal>
            <Tooltip.Content
              className="screenshot-help-tooltip"
              style={{ pointerEvents: "none" }}
              side="top"
              sideOffset={6}
              collisionPadding={16}
            >
              {help}
              <Tooltip.Arrow className="screenshot-help-arrow" />
            </Tooltip.Content>
          </Tooltip.Portal>
        </Tooltip.Root>
      </Tooltip.Provider>
      <span id={`${htmlFor}-help`} className="sr-only">
        {help}
      </span>
    </div>
  );
}

const resolutions = [
  ...[
    [3840, 2400],
    [3200, 2000],
    [3072, 1920],
    [2880, 1800],
    [2560, 1600],
    [1920, 1200],
  ].map(([width, height]) => ({
    value: `${width}x${height}`,
    label: `桌面 · ${width} × ${height}（16:10）`,
    width,
    height,
  })),
  {
    value: "1920x1080",
    label: "桌面 · 1920 × 1080",
    width: 1920,
    height: 1080,
  },
  {
    value: "1440x900",
    label: "桌面 · 1440 × 900（默认）",
    width: 1440,
    height: 900,
  },
  { value: "1366x768", label: "桌面 · 1366 × 768", width: 1366, height: 768 },
  {
    value: "768x1024",
    label: "平板尺寸 · 768 × 1024",
    width: 768,
    height: 1024,
  },
  { value: "390x844", label: "手机尺寸 · 390 × 844", width: 390, height: 844 },
];

const zoomPresets = [25, 50, 67, 75, 80, 90, 100, 110, 125, 150, 175, 200];

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
            width={item.viewport_width}
            height={item.viewport_height}
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
              : `网站首屏 · ${item?.viewport_width || 1440} × ${item?.viewport_height || 900}`}
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
  const [resolution, setResolution] = useState("1440x900");
  const [viewportWidth, setViewportWidth] = useState("1440");
  const [viewportHeight, setViewportHeight] = useState("900");
  const [zoomPreset, setZoomPreset] = useState("100");
  const [zoomPercent, setZoomPercent] = useState("100");
  const [javascript, setJavaScript] = useState("");
  const [language, setLanguage] = useState("zh-CN");
  const [loadTimeoutSeconds, setLoadTimeoutSeconds] = useState("30");
  const [authEnabled, setAuthEnabled] = useState(false);
  const [username, setUsername] = useState("");
  const [password, setPassword] = useState("");
  const [confirmClear, setConfirmClear] = useState(false);
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
  const save = useSessionMutation({
    mutationFn: () =>
      api<WebsiteScreenshot>(
        `/vhosts/screenshot?name=${encodeURIComponent(site!.name)}`,
        {
          method: "PUT",
          body: JSON.stringify({
            url,
            viewport_width: Number(viewportWidth),
            viewport_height: Number(viewportHeight),
            zoom_percent: Number(zoomPercent),
            javascript,
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
  const clear = useSessionMutation({
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
      setConfirmClear(false);
    },
    onError: () => setConfirmClear(false),
  });
  const refresh = useSessionMutation({
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
    const domain = target.domains[0];
    const scheme = target.listens.some((listen) =>
      listen.split(/\s+/).includes("ssl"),
    )
      ? "https"
      : "http";
    setURL(item?.url || (domain ? `${scheme}://${domain}/` : ""));
    const width = item?.viewport_width || 1440;
    const height = item?.viewport_height || 900;
    setViewportWidth(String(width));
    setViewportHeight(String(height));
    setResolution(
      resolutions.find((r) => r.width === width && r.height === height)
        ?.value || "custom",
    );
    const zoom = item?.zoom_percent || 100;
    setZoomPercent(String(zoom));
    setZoomPreset(zoomPresets.includes(zoom) ? String(zoom) : "custom");
    setJavaScript(item?.javascript || "");
    setLanguage(item?.language || "zh-CN");
    setLoadTimeoutSeconds(String(item?.load_timeout_seconds || 30));
    setAuthEnabled(item?.auth_enabled || false);
    setUsername(item?.username || "");
    setPassword("");
    setConfirmClear(false);
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
              setConfirmClear(false);
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
                保存后按所选分辨率截取网站首屏，设置与认证信息仅保存在本机。
              </DialogDescription>
            </DialogHeader>
            <form
              onSubmit={(e) => {
                e.preventDefault();
                if (busy || confirmClear) return;
                if (new TextEncoder().encode(javascript).length > 16 * 1024) {
                  toast.error("截图前 JavaScript 不能超过 16 KiB");
                  return;
                }
                save.mutate();
              }}
            >
              <fieldset
                disabled={busy || confirmClear}
                className="screenshot-settings-fields"
              >
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
                <div className="screenshot-settings-row">
                  <div className="form-field">
                    <ScreenshotFieldLabel
                      htmlFor="screenshot-resolution"
                      help="设置浏览器视口大小，手机和平板尺寸不切换浏览器身份。"
                    >
                      截图分辨率
                    </ScreenshotFieldLabel>
                    <Select
                      value={resolution}
                      disabled={busy}
                      onValueChange={(value) => {
                        setResolution(value);
                        const preset = resolutions.find(
                          (r) => r.value === value,
                        );
                        if (preset) {
                          setViewportWidth(String(preset.width));
                          setViewportHeight(String(preset.height));
                        }
                      }}
                    >
                      <SelectTrigger
                        id="screenshot-resolution"
                        className="w-full"
                        aria-describedby="screenshot-resolution-help"
                      >
                        <SelectValue />
                      </SelectTrigger>
                      <SelectContent>
                        {resolutions.map((preset) => (
                          <SelectItem key={preset.value} value={preset.value}>
                            {preset.label}
                          </SelectItem>
                        ))}
                        <SelectItem value="custom">自定义</SelectItem>
                      </SelectContent>
                    </Select>
                  </div>
                  <div className="form-field">
                    <ScreenshotFieldLabel
                      htmlFor="screenshot-zoom"
                      help="缩小比例可显示更多内容，输出图片尺寸保持所选分辨率。"
                    >
                      网页缩放
                    </ScreenshotFieldLabel>
                    <Select
                      value={zoomPreset}
                      disabled={busy}
                      onValueChange={(value) => {
                        setZoomPreset(value);
                        if (value !== "custom") setZoomPercent(value);
                      }}
                    >
                      <SelectTrigger
                        id="screenshot-zoom"
                        className="w-full"
                        aria-describedby="screenshot-zoom-help"
                      >
                        <SelectValue />
                      </SelectTrigger>
                      <SelectContent>
                        {zoomPresets.map((percent) => (
                          <SelectItem key={percent} value={String(percent)}>
                            {percent}%{percent === 100 ? "（默认）" : ""}
                          </SelectItem>
                        ))}
                        <SelectItem value="custom">自定义</SelectItem>
                      </SelectContent>
                    </Select>
                  </div>
                </div>
                {resolution === "custom" && (
                  <div className="screenshot-dimensions">
                    <div className="form-field">
                      <Label htmlFor="screenshot-width">截图宽度（像素）</Label>
                      <Input
                        id="screenshot-width"
                        type="number"
                        required
                        min={320}
                        max={3840}
                        step={1}
                        value={viewportWidth}
                        onChange={(e) => setViewportWidth(e.target.value)}
                      />
                    </div>
                    <div className="form-field">
                      <Label htmlFor="screenshot-height">
                        截图高度（像素）
                      </Label>
                      <Input
                        id="screenshot-height"
                        type="number"
                        required
                        min={320}
                        max={3840}
                        step={1}
                        value={viewportHeight}
                        onChange={(e) => setViewportHeight(e.target.value)}
                      />
                    </div>
                  </div>
                )}
                {zoomPreset === "custom" && (
                  <div className="form-field">
                    <Label htmlFor="screenshot-zoom-percent">
                      自定义缩放比例（%）
                    </Label>
                    <Input
                      id="screenshot-zoom-percent"
                      type="number"
                      required
                      min={25}
                      max={200}
                      step={1}
                      value={zoomPercent}
                      onChange={(e) => setZoomPercent(e.target.value)}
                      aria-describedby="screenshot-zoom-help"
                    />
                  </div>
                )}
                <div className="screenshot-settings-row">
                  <div className="form-field">
                    <ScreenshotFieldLabel
                      htmlFor="screenshot-language"
                      help="例如 zh-CN（简体中文）、en-US（英语），用于网站语言及日期、数字格式。"
                    >
                      浏览器语言
                    </ScreenshotFieldLabel>
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
                  </div>
                  <div className="form-field">
                    <ScreenshotFieldLabel
                      htmlFor="screenshot-timeout"
                      help="默认 30 秒，可设为 5–300 秒。等待 CSS、图片和字体等资源加载稳定后截图；加载较慢时可适当增加，超时保留上次成功截图。"
                    >
                      最长加载时间（秒）
                    </ScreenshotFieldLabel>
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
                  </div>
                </div>
                <div className="form-field">
                  <ScreenshotFieldLabel
                    htmlFor="screenshot-javascript"
                    help="可选，最多 16 KiB。页面加载后在目标网站中执行一次，支持 await。执行完成并等待资源稳定后截图；执行出错或超时会保留上次成功截图。"
                  >
                    截图前 JavaScript
                  </ScreenshotFieldLabel>
                  <textarea
                    id="screenshot-javascript"
                    className="screenshot-script"
                    rows={3}
                    maxLength={16384}
                    spellCheck={false}
                    autoCapitalize="off"
                    autoCorrect="off"
                    value={javascript}
                    onChange={(e) => setJavaScript(e.target.value)}
                    placeholder={
                      'document.querySelector(".popup")?.remove();\nawait new Promise(resolve => setTimeout(resolve, 500));'
                    }
                    aria-describedby="screenshot-javascript-help"
                  />
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
                      <ScreenshotFieldLabel
                        htmlFor="screenshot-password"
                        help="更换用户名或目标来源时，需要重新填写密码。关闭认证会清除已保存的凭据。"
                      >
                        Basic Auth 密码
                      </ScreenshotFieldLabel>
                      <Input
                        id="screenshot-password"
                        aria-describedby="screenshot-password-help"
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
                {!confirmClear && (
                  <DialogFooter>
                    {saved && (
                      <Button
                        type="button"
                        variant="ghost"
                        className="screenshot-clear"
                        onClick={() => {
                          clear.reset();
                          setConfirmClear(true);
                        }}
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
                )}
              </fieldset>
              {confirmClear && (
                <ConsequenceConfirmation
                  title={`确认清除 ${site?.domains[0] || site?.name} 的截图设置？`}
                  description="将删除该站点的截图设置、已保存的认证信息和图片。此操作无法撤销，需要重新配置后才能再次截图。"
                  confirmLabel="确认清除设置"
                  busy={busy}
                  onCancel={() => setConfirmClear(false)}
                  onConfirm={() => {
                    if (!busy) clear.mutate();
                  }}
                />
              )}
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
                width={enlarged.viewport_width}
                height={enlarged.viewport_height}
                alt={`${enlarged.name} 的完整网站截图`}
              />
            )}
          </DialogContent>
        </Dialog>
      </>
    ),
  };
}
