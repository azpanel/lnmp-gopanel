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
import { login } from "@/lib/api";

export function Login() {
  const [username, setUsername] = useState("admin"),
    [password, setPassword] = useState(""),
    [error, setError] = useState(""),
    [pending, setPending] = useState(false);
  const client = useQueryClient();
  async function submit(e: FormEvent) {
    e.preventDefault();
    setError("");
    setPending(true);
    try {
      const session = await login(username, password);
      setPassword("");
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
        <section className="login-intro">
          <div className="eyebrow">
            <span className="live-dot" />
            为你的服务器而生
          </div>
          <h1>
            运行状态，
            <br />
            <span>一目了然。</span>
          </h1>
          <p>
            轻量的 LNMP 监控与管理面板。
            <br />
            实时了解系统状态，从容管理每一项服务。
          </p>
          <div className="login-features">
            <span>
              <Activity size={16} />
              实时指标
            </span>
            <span>
              <ShieldCheck size={16} />
              本机管理
            </span>
            <span>
              <LockKeyhole size={16} />
              安全访问
            </span>
          </div>
        </section>
        <Card className="login-card">
          <CardContent>
            <div className="login-lock">
              <LockKeyhole size={22} />
            </div>
            <h2>登录管理面板</h2>
            <p className="text-muted-foreground text-sm mb-8">
              使用安装时设置的管理员账号继续
            </p>
            <form onSubmit={submit}>
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
                    登录面板
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
