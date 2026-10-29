import { useEffect, useRef, useState, type FormEvent } from "react";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import {
  Copy,
  Download,
  Eye,
  EyeOff,
  KeyRound,
  Loader2,
  ShieldCheck,
  Shuffle,
} from "lucide-react";
import { toast } from "sonner";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogHeader,
  DialogTitle,
  DialogTrigger,
} from "@/components/ui/dialog";
import { acceptSession, api } from "@/lib/api";
import { assessPassword, generateStrongPassword } from "@/lib/password";
import type {
  AccountSecurity as Security,
  Session,
  TOTPSetup,
} from "@/lib/types";

export function AccountSecurity({ username }: { username: string }) {
  const [open, setOpen] = useState(false);
  const [busy, setBusy] = useState(false);
  return (
    <Dialog
      open={open}
      onOpenChange={(value) => {
        if (!busy) setOpen(value);
      }}
    >
      <DialogTrigger asChild>
        <Button variant="ghost" size="icon" aria-label="账户安全">
          <ShieldCheck size={18} />
        </Button>
      </DialogTrigger>
      <DialogContent
        className="account-security-dialog"
        showCloseButton={!busy}
        onEscapeKeyDown={(event) => {
          if (busy) event.preventDefault();
        }}
        onInteractOutside={(event) => {
          if (busy) event.preventDefault();
        }}
      >
        <DialogHeader>
          <DialogTitle>账户安全</DialogTitle>
          <DialogDescription>管理登录密码和 TOTP 双重验证。</DialogDescription>
        </DialogHeader>
        {open && (
          <SecurityContents username={username} busy={busy} setBusy={setBusy} />
        )}
      </DialogContent>
    </Dialog>
  );
}

type SecurityResult = { session: Session; recovery_codes?: string[] };
type Strength = { score: number; valid: boolean; message: string };

function SecurityContents({
  username,
  busy,
  setBusy,
}: {
  username: string;
  busy: boolean;
  setBusy: (value: boolean) => void;
}) {
  const client = useQueryClient();
  const security = useQuery({
    queryKey: ["account-security"],
    queryFn: () => api<Security>("/auth/security"),
    staleTime: 0,
    retry: false,
  });
  const [current, setCurrent] = useState("");
  const [password, setPassword] = useState("");
  const [confirmation, setConfirmation] = useState("");
  const [showPassword, setShowPassword] = useState(false);
  const [factor, setFactor] = useState("");
  const [recovery, setRecovery] = useState(false);
  const [setup, setSetup] = useState<TOTPSetup | null>(null);
  const [setupCode, setSetupCode] = useState("");
  const [codes, setCodes] = useState<string[] | null>(null);
  const [strength, setStrength] = useState<Strength | null>(null);
  const [error, setError] = useState("");
  const [now, setNow] = useState(Date.now());
  const draft = useRef(false);
  const errorRef = useRef<HTMLParagraphElement>(null);
  const enabled = security.data?.totp_enabled ?? false;
  useEffect(() => {
    if (error) errorRef.current?.scrollIntoView({ block: "nearest" });
  }, [error]);
  useEffect(() => {
    let active = true;
    setStrength(null);
    if (!password) return;
    // Debounce assessment; the estimator is loaded only with this dialog.
    const timer = setTimeout(() => {
      void assessPassword(password, username)
        .then((result) => {
          if (active) setStrength(result);
        })
        .catch(() => {
          if (active)
            setStrength({
              score: 0,
              valid: false,
              message: "强度检测加载失败，请重新打开账户安全设置",
            });
        });
    }, 150);
    return () => {
      active = false;
      clearTimeout(timer);
    };
  }, [password, username]);
  useEffect(() => {
    if (!setup) return;
    const timer = setInterval(() => setNow(Date.now()), 1000);
    return () => clearInterval(timer);
  }, [setup]);
  useEffect(
    () => () => {
      if (draft.current)
        void api("/auth/totp/setup", { method: "DELETE" }).catch(() => {});
      client.removeQueries({ queryKey: ["account-security"] });
    },
    [client],
  );

  async function run(action: () => Promise<void>) {
    setError("");
    setBusy(true);
    try {
      await action();
    } catch (error) {
      setError((error as Error).message);
    } finally {
      setBusy(false);
    }
  }
  function proof() {
    return {
      current_password: current,
      ...(enabled
        ? recovery
          ? { recovery_code: factor }
          : { code: factor }
        : {}),
    };
  }
  function updateSession(result: SecurityResult) {
    client.setQueryData(["session"], acceptSession(result.session));
    setCurrent("");
    setFactor("");
    setRecovery(false);
    void security.refetch();
  }
  async function copy(value: string) {
    try {
      await navigator.clipboard.writeText(value);
      toast.success("已复制");
    } catch {
      setError("无法复制，请手动选择并保存");
    }
  }
  function generate() {
    try {
      const value = generateStrongPassword();
      setPassword(value);
      setConfirmation(value);
      setShowPassword(true);
      setError("");
    } catch {
      setError("无法生成安全随机密码，请手动输入");
    }
  }
  function changePassword(event: FormEvent) {
    event.preventDefault();
    if (password !== confirmation) {
      setError("两次输入的新密码不一致");
      return;
    }
    if (!strength?.valid) {
      setError("请使用通过强度检测的新密码");
      return;
    }
    void run(async () => {
      await api("/auth/password", {
        method: "POST",
        body: JSON.stringify({ ...proof(), new_password: password }),
      });
      setPassword("");
      setConfirmation("");
      setCurrent("");
      setFactor("");
      toast.success("密码已修改，请使用新密码重新登录");
      window.dispatchEvent(new Event("session-expired"));
    });
  }
  function startSetup() {
    void run(async () => {
      const value = await api<TOTPSetup>("/auth/totp/setup", {
        method: "POST",
        body: JSON.stringify({ current_password: current }),
      });
      draft.current = true;
      setSetup(value);
      setSetupCode("");
      setNow(Date.now());
      setCurrent("");
    });
  }
  function confirmSetup(event: FormEvent) {
    event.preventDefault();
    void run(async () => {
      const result = await api<SecurityResult>("/auth/totp/confirm", {
        method: "POST",
        body: JSON.stringify({ setup_id: setup!.setup_id, code: setupCode }),
      });
      draft.current = false;
      setSetup(null);
      setSetupCode("");
      updateSession(result);
      setCodes(result.recovery_codes!);
    });
  }
  function manageTOTP(disable: boolean) {
    void run(async () => {
      const result = await api<SecurityResult>(
        disable ? "/auth/totp" : "/auth/totp/recovery-codes",
        { method: disable ? "DELETE" : "POST", body: JSON.stringify(proof()) },
      );
      updateSession(result);
      if (disable) toast.success("TOTP 已关闭");
      else setCodes(result.recovery_codes!);
    });
  }
  function downloadCodes() {
    const blob = new Blob(
      [
        `LNMP GoPanel · ${username}\n每个恢复码仅可使用一次，请妥善离线保存。\n\n${codes!.join("\n")}\n`,
      ],
      { type: "text/plain;charset=utf-8" },
    );
    const url = URL.createObjectURL(blob);
    const link = document.createElement("a");
    link.href = url;
    link.download = "gopanel-recovery-codes.txt";
    link.click();
    setTimeout(() => URL.revokeObjectURL(url), 1000);
  }
  const expired = setup ? now >= new Date(setup.expires_at).getTime() : false;
  const canAuthenticate = !!current && (!enabled || !!factor);
  return (
    <div className="account-security-content">
      {error && (
        <p ref={errorRef} className="form-error" role="alert">
          {error}
        </p>
      )}
      {security.isPending ? (
        <p role="status">正在读取账户安全设置…</p>
      ) : security.error ? (
        <div className="notice" role="alert">
          {security.error.message}
          <Button variant="outline" onClick={() => void security.refetch()}>
            重试
          </Button>
        </div>
      ) : codes ? (
        <section className="account-security-section">
          <h3>
            <KeyRound size={17} />
            保存一次性恢复码
          </h3>
          <p className="small-note">
            以下恢复码仅展示这一次。每个只能使用一次，重新生成后旧码全部失效。请离线保存。
          </p>
          <pre className="recovery-codes" aria-label="一次性恢复码">
            {codes.join("\n")}
          </pre>
          <div className="account-security-actions">
            <Button
              variant="outline"
              onClick={() => void copy(codes.join("\n"))}
            >
              <Copy size={15} />
              复制恢复码
            </Button>
            <Button variant="outline" onClick={downloadCodes}>
              <Download size={15} />
              下载恢复码
            </Button>
            <Button onClick={() => setCodes(null)}>我已保存</Button>
          </div>
        </section>
      ) : setup ? (
        <section className="account-security-section">
          <h3>
            <ShieldCheck size={17} />
            绑定 TOTP 验证器
          </h3>
          <p className="small-note">
            使用验证器扫码或手动输入密钥，再输入 6 位验证码完成绑定。绑定在 5
            分钟后过期。
          </p>
          <img
            className="totp-qr"
            src={setup.qr_code}
            alt="TOTP 绑定二维码"
            width={240}
            height={240}
          />
          <Label htmlFor="totp-secret">手动输入密钥</Label>
          <div className="account-password-input">
            <Input
              id="totp-secret"
              value={setup.secret}
              readOnly
              spellCheck={false}
              autoComplete="off"
            />
            <Button
              type="button"
              variant="outline"
              size="icon"
              aria-label="复制 TOTP 密钥"
              onClick={() => void copy(setup.secret)}
            >
              <Copy size={15} />
            </Button>
          </div>
          <form onSubmit={confirmSetup} className="account-security-section">
            <div className="form-field">
              <Label htmlFor="totp-confirm-code">动态验证码</Label>
              <Input
                id="totp-confirm-code"
                value={setupCode}
                onChange={(e) => setSetupCode(e.target.value)}
                autoComplete="one-time-code"
                inputMode="numeric"
                pattern="[0-9]{6}"
                maxLength={6}
                required
                disabled={busy || expired}
              />
            </div>
            <p className="small-note">
              验证码依赖服务器时间。已用过的验证码不能再次使用，请等待下一组数字。
            </p>
            {expired && (
              <p className="form-error" role="alert">
                绑定已过期，请返回后重新开始。
              </p>
            )}
            <div className="account-security-actions">
              <Button
                type="submit"
                disabled={busy || expired || setupCode.length !== 6}
              >
                {busy && <Loader2 className="animate-spin" />}确认启用 TOTP
              </Button>
              <Button
                type="button"
                variant="outline"
                disabled={busy}
                onClick={() =>
                  void run(async () => {
                    await api("/auth/totp/setup", { method: "DELETE" });
                    draft.current = false;
                    setSetup(null);
                    setSetupCode("");
                  })
                }
              >
                返回
              </Button>
            </div>
          </form>
        </section>
      ) : (
        <>
          <section className="account-security-section">
            <h3>
              <ShieldCheck size={17} />
              身份确认
            </h3>
            <div className="form-field">
              <Label htmlFor="account-current-password">当前登录密码</Label>
              <Input
                id="account-current-password"
                type="password"
                autoComplete="current-password"
                value={current}
                onChange={(e) => setCurrent(e.target.value)}
                disabled={busy}
              />
            </div>
            {enabled && (
              <div className="form-field">
                <Label htmlFor="account-factor">
                  {recovery ? "一次性恢复码" : "动态验证码"}
                </Label>
                <Input
                  id="account-factor"
                  value={factor}
                  onChange={(e) => setFactor(e.target.value)}
                  autoComplete="one-time-code"
                  inputMode={recovery ? "text" : "numeric"}
                  maxLength={recovery ? 35 : 6}
                  disabled={busy}
                />
                <Button
                  type="button"
                  variant="ghost"
                  size="sm"
                  disabled={busy}
                  onClick={() => {
                    setRecovery(!recovery);
                    setFactor("");
                  }}
                >
                  {recovery ? "使用动态验证码" : "使用恢复码"}
                </Button>
                <p className="small-note">
                  每次操作都需要新的验证码或未使用的恢复码。
                </p>
              </div>
            )}
          </section>
          <form className="account-security-section" onSubmit={changePassword}>
            <h3>
              <KeyRound size={17} />
              修改登录密码
            </h3>
            <div className="form-field">
              <div className="account-password-heading">
                <Label htmlFor="account-new-password">新密码</Label>
                <Button
                  type="button"
                  variant="outline"
                  size="sm"
                  disabled={busy}
                  onClick={generate}
                >
                  <Shuffle size={14} />
                  随机生成强密码
                </Button>
              </div>
              <div className="account-password-input">
                <Input
                  id="account-new-password"
                  type={showPassword ? "text" : "password"}
                  autoComplete="new-password"
                  value={password}
                  onChange={(e) => setPassword(e.target.value)}
                  required
                  disabled={busy}
                  aria-describedby="account-password-strength"
                />
                <Button
                  type="button"
                  variant="outline"
                  size="icon"
                  disabled={busy}
                  aria-label={showPassword ? "隐藏新密码" : "显示新密码"}
                  onClick={() => setShowPassword(!showPassword)}
                >
                  {showPassword ? <EyeOff size={15} /> : <Eye size={15} />}
                </Button>
                <Button
                  type="button"
                  variant="outline"
                  size="icon"
                  disabled={busy || !password}
                  aria-label="复制新密码"
                  onClick={() => void copy(password)}
                >
                  <Copy size={15} />
                </Button>
              </div>
              <div
                id="account-password-strength"
                className="account-password-strength"
                data-score={strength?.score ?? 0}
              >
                <div className="account-strength-bars" aria-hidden="true">
                  {[1, 2, 3, 4].map((level) => (
                    <span
                      key={level}
                      data-filled={!!strength && strength.score >= level}
                    />
                  ))}
                </div>
                <span role="status">
                  {!password
                    ? "至少 15 个字符；支持空格和长口令，最多 72 个 UTF-8 字节。"
                    : strength
                      ? strength.message
                      : "正在检测密码强度…"}
                </span>
              </div>
            </div>
            <div className="form-field">
              <Label htmlFor="account-confirm-password">确认新密码</Label>
              <Input
                id="account-confirm-password"
                type={showPassword ? "text" : "password"}
                value={confirmation}
                onChange={(e) => setConfirmation(e.target.value)}
                autoComplete="new-password"
                required
                disabled={busy}
              />
            </div>
            <p className="small-note">
              修改成功后，所有浏览器都会退出登录，请先保存好新密码。
            </p>
            <Button
              type="submit"
              disabled={
                busy || !canAuthenticate || !strength?.valid || !confirmation
              }
            >
              {busy && <Loader2 className="animate-spin" />}修改密码并退出登录
            </Button>
          </form>
          <section className="account-security-section">
            <h3>
              <ShieldCheck size={17} />
              TOTP 双重验证
            </h3>
            <p className="small-note">
              {enabled
                ? `已启用 · 剩余 ${security.data?.recovery_remaining} 个恢复码。登录时需要密码和动态验证码，也可使用恢复码。`
                : "尚未启用。绑定验证器后，登录时需要额外输入动态验证码。"}
            </p>
            <div className="account-security-actions">
              {enabled ? (
                <>
                  <Button
                    variant="outline"
                    disabled={busy || !canAuthenticate}
                    onClick={() => manageTOTP(false)}
                  >
                    重新生成恢复码
                  </Button>
                  <Button
                    variant="destructive"
                    disabled={busy || !canAuthenticate}
                    onClick={() => manageTOTP(true)}
                  >
                    关闭 TOTP
                  </Button>
                </>
              ) : (
                <Button
                  variant="outline"
                  disabled={busy || !current}
                  onClick={startSetup}
                >
                  绑定 TOTP
                </Button>
              )}
            </div>
          </section>
        </>
      )}
    </div>
  );
}
