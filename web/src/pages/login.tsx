import { useState, type FormEvent } from "react";
import {
  Activity,
  ArrowRight,
  Loader2,
  LockKeyhole,
  ShieldCheck,
} from "lucide-react";
import { useQueryClient } from "@tanstack/react-query";
import { Button } from "@/components/ui/button";
import { Card, CardContent } from "@/components/ui/card";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { LoginShowcase } from "@/components/login-showcase";
import { login, loginTOTP } from "@/lib/api";
import type { LoginChallenge } from "@/lib/types";

export function Login() {
  const [username, setUsername] = useState("admin"),
    [password, setPassword] = useState(""),
    [error, setError] = useState(""),
    [pending, setPending] = useState(false);
  const client = useQueryClient();
  const [challenge, setChallenge] = useState<LoginChallenge | null>(null);
  const [code, setCode] = useState("");
  const [recovery, setRecovery] = useState(false);
  async function submit(e: FormEvent) {
    e.preventDefault();
    setError("");
    setPending(true);
    try {
      const session = challenge
        ? await loginTOTP(challenge.challenge_token, code, recovery)
        : await login(username, password);
      setPassword("");
      if ("totp_required" in session) {
        setChallenge(session);
        return;
      }
      setCode("");
      client.setQueryData(["session"], session);
    } catch (e) {
      setError((e as Error).message);
    } finally {
      setPending(false);
    }
  }
  return (
    <main className="login-page">
      <div className="login-brand">
        <div className="brand-logo">
          <Activity size={23} />
        </div>
        <span>
          LNMP
          <span className="text-muted-foreground font-normal"> GoPanel</span>
        </span>
      </div>
      <div className="login-grid">
        <LoginShowcase />
        <Card className="login-card">
          <CardContent>
            <div className="login-lock">
              <LockKeyhole size={22} />
            </div>
            <h2>登录管理面板</h2>
            <p className="text-muted-foreground text-sm mb-8">
              {challenge
                ? "请输入验证器中的验证码，或使用一次性恢复码"
                : "使用管理员账号继续"}
            </p>
            <form onSubmit={submit}>
              {!challenge && (
                <>
                  <div className="form-field">
                    <Label htmlFor="username">用户名</Label>
                    <Input
                      id="username"
                      value={username}
                      onChange={(e) => setUsername(e.target.value)}
                      autoComplete="username"
                      required
                      maxLength={128}
                    />
                  </div>
                  <div className="form-field">
                    <Label htmlFor="password">密码</Label>
                    <Input
                      id="password"
                      type="password"
                      value={password}
                      onChange={(e) => setPassword(e.target.value)}
                      autoComplete="current-password"
                      required
                    />
                  </div>
                </>
              )}
              {challenge && (
                <>
                  <div className="form-field">
                    <Label htmlFor="login-code">
                      {recovery ? "恢复码" : "动态验证码"}
                    </Label>
                    <Input
                      id="login-code"
                      value={code}
                      onChange={(e) => setCode(e.target.value)}
                      autoComplete="one-time-code"
                      autoFocus
                      required
                      inputMode={recovery ? "text" : "numeric"}
                      pattern={recovery ? undefined : "[0-9]{6}"}
                      maxLength={recovery ? 35 : 6}
                    />
                  </div>
                  <Button
                    type="button"
                    variant="ghost"
                    size="sm"
                    disabled={pending}
                    onClick={() => {
                      setRecovery(!recovery);
                      setCode("");
                      setError("");
                    }}
                  >
                    {recovery ? "使用动态验证码" : "使用恢复码"}
                  </Button>
                  <Button
                    type="button"
                    variant="ghost"
                    size="sm"
                    disabled={pending}
                    onClick={() => {
                      setChallenge(null);
                      setCode("");
                      setError("");
                      setRecovery(false);
                    }}
                  >
                    返回密码登录
                  </Button>
                </>
              )}
              {error && (
                <p className="form-error" role="alert">
                  {error}
                </p>
              )}
              <Button type="submit" disabled={pending} className="w-full mt-3">
                {pending ? (
                  <Loader2 className="animate-spin" />
                ) : (
                  <>
                    {challenge ? "验证并登录" : "登录面板"}
                    <ArrowRight size={16} />
                  </>
                )}
              </Button>
            </form>
            <div className="login-hint">
              <ShieldCheck size={14} />
              仅授权管理员可访问
            </div>
          </CardContent>
        </Card>
      </div>
      <footer className="login-footer">
        LNMP GoPanel <span>轻量 · 实时 · 专注</span>
      </footer>
    </main>
  );
}
