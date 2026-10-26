import { useEffect, useState } from "react";
import { Navigate, NavLink, Route, Routes } from "react-router-dom";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import {
  Activity,
  ArrowUpRight,
  ChevronRight,
  CircleHelp,
  Container,
  LayoutDashboard,
  HardDrive,
  Globe2,
  Loader2,
  LogOut,
  Menu,
  Moon,
  Pause,
  Play,
  Server,
  Settings2,
  ShieldCheck,
  Sun,
  X,
} from "lucide-react";
import { toast, Toaster } from "sonner";
import { Button } from "@/components/ui/button";
import { Login } from "@/pages/login";
import { Dashboard } from "@/pages/dashboard";
import { Services } from "@/pages/services";
import { System } from "@/pages/system";
import { Storage } from "@/pages/storage";
import { Vhosts } from "@/pages/vhosts";
import { Docker } from "@/pages/docker";
import { api, fetchSession, logout } from "@/lib/api";
import type { Host, Service, Session } from "@/lib/types";
import { useMonitor } from "@/lib/use-monitor";
import { cn } from "@/lib/utils";

export default function App() {
  const client = useQueryClient();
  const session = useQuery({
    queryKey: ["session"],
    queryFn: fetchSession,
    retry: false,
    staleTime: 60000,
    refetchOnWindowFocus: true,
  });
  useEffect(() => {
    const expired = () => {
      client.setQueryData(["session"], null);
      client.removeQueries({ predicate: (q) => q.queryKey[0] !== "session" });
    };
    window.addEventListener("session-expired", expired);
    return () => window.removeEventListener("session-expired", expired);
  }, [client]);
  if (session.isPending)
    return (
      <div className="app-loading">
        <Activity size={28} />
        <p>正在连接管理面板…</p>
      </div>
    );
  if (session.error)
    return (
      <div className="app-loading">
        <Server size={28} />
        <p>无法连接服务器</p>
        <span>{session.error.message}</span>
        <Button variant="outline" onClick={() => void session.refetch()}>
          重新连接
        </Button>
      </div>
    );
  return (
    <>
      {session.data ? <Workspace session={session.data} /> : <Login />}
      <Toaster richColors theme="system" position="bottom-right" />
    </>
  );
}
function Workspace({ session }: { session: Session }) {
  const monitor = useMonitor(),
    client = useQueryClient();
  const [mobileOpen, setMobileOpen] = useState(false),
    [theme, setTheme] = useState(() => localStorage.getItem("theme") || "dark"),
    [loggingOut, setLoggingOut] = useState(false);
  useEffect(() => {
    document.documentElement.classList.toggle("dark", theme === "dark");
    localStorage.setItem("theme", theme);
  }, [theme]);
  const host = useQuery({
    queryKey: ["host"],
    queryFn: () => api<Host>("/host"),
    staleTime: Infinity,
  });
  const services = useQuery({
    queryKey: ["services"],
    queryFn: () => api<Service[]>("/services"),
    refetchInterval: 5000,
    retry: 1,
  });
  const vhostCount = useQuery({
    queryKey: ["vhost-count"],
    queryFn: () => api<{ count: number }>("/vhosts/count"),
    refetchInterval: 30000,
    retry: 1,
  });
  const stateLabels = {
    connecting: "连接中",
    live: "实时连接",
    offline: "连接中断",
    paused: "已暂停",
  };
  async function signOut() {
    setLoggingOut(true);
    try {
      await logout();
      client.setQueryData(["session"], null);
      client.removeQueries({ predicate: (q) => q.queryKey[0] !== "session" });
    } catch (e) {
      toast.error((e as Error).message);
    } finally {
      setLoggingOut(false);
    }
  }
  return (
    <div className="workspace">
      {mobileOpen && (
        <div className="mobile-overlay" onClick={() => setMobileOpen(false)} />
      )}
      <aside className={cn("sidebar", mobileOpen && "open")}>
        <NavLink to="/" className="brand" onClick={() => setMobileOpen(false)}>
          <div className="brand-logo">
            <Activity size={21} />
          </div>
          <span>
            LNMP<span className="brand-secondary"> GoPanel</span>
          </span>
          <span className="brand-tag">BETA</span>
        </NavLink>
        <button
          className="mobile-close"
          aria-label="关闭导航"
          onClick={() => setMobileOpen(false)}
        >
          <X size={20} />
        </button>
        <div className="sidebar-server">
          <div className="server-glyph">
            <Server size={18} />
          </div>
          <div>
            <strong>{host.data?.hostname || "本地服务器"}</strong>
            <span>
              <span
                className={cn(
                  "tiny-dot",
                  monitor.connection !== "live" && "dim",
                )}
              />
              {host.data
                ? `${host.data.platform} ${host.data.version}`
                : "本机 · Linux"}
            </span>
          </div>
        </div>
        <div className="nav-group-title">工作空间</div>
        <nav>
          {[
            { to: "/", label: "系统概览", icon: LayoutDashboard },
            { to: "/vhosts", label: "虚拟主机", icon: Globe2 },
            { to: "/services", label: "LNMP 服务", icon: Settings2 },
            { to: "/docker", label: "Docker 管理", icon: Container },
            { to: "/storage", label: "硬盘空间", icon: HardDrive },
            { to: "/system", label: "系统信息", icon: Server },
          ].map((item) => (
            <NavLink
              key={item.to}
              to={item.to}
              end
              aria-label={item.label}
              onClick={() => setMobileOpen(false)}
              className={({ isActive }) => cn("nav-item", isActive && "active")}
            >
              <item.icon size={17} />
              <span>{item.label}</span>
              {item.to === "/services" && (
                <span className="nav-count">
                  {services.data?.length ?? "—"}
                </span>
              )}
              {item.to === "/vhosts" && (
                <span className="nav-count" title={vhostCount.error?.message}>
                  {vhostCount.data?.count ?? "—"}
                </span>
              )}
            </NavLink>
          ))}
        </nav>
        <div className="sidebar-bottom">
          <a
            className="nav-item help-link"
            href="https://lnmp.org/faq.html"
            target="_blank"
            rel="noreferrer"
          >
            <CircleHelp size={17} />
            <span>LNMP 文档</span>
            <ArrowUpRight size={14} />
          </a>
          <div className="sidebar-profile">
            <div className="avatar">
              {session.username.slice(0, 1).toUpperCase()}
            </div>
            <div>
              <strong>{session.username}</strong>
              <span>系统管理员</span>
            </div>
            <Button
              variant="ghost"
              size="icon"
              aria-label="退出登录"
              onClick={() => void signOut()}
              disabled={loggingOut}
            >
              {loggingOut ? (
                <Loader2 className="animate-spin" />
              ) : (
                <LogOut size={16} />
              )}
            </Button>
          </div>
        </div>
      </aside>
      <div className="main-shell">
        <header className="topbar">
          <div className="breadcrumb">
            <Button
              variant="ghost"
              size="icon"
              className="mobile-menu"
              aria-label="打开导航"
              onClick={() => setMobileOpen(true)}
            >
              <Menu />
            </Button>
            <span>工作空间</span>
            <ChevronRight size={12} />
            <strong>{host.data?.hostname || "本地服务器"}</strong>
          </div>
          <div className="topbar-actions">
            <span
              className={cn(
                "connection-indicator",
                monitor.connection === "live" && "connected",
              )}
            >
              <span className="status-dot" />
              {stateLabels[monitor.connection]}
            </span>
            <span className="topbar-divider" />
            <Button
              variant="ghost"
              size="icon"
              aria-label={monitor.paused ? "继续实时更新" : "暂停实时更新"}
              onClick={() => monitor.setPaused(!monitor.paused)}
            >
              {monitor.paused ? <Play size={16} /> : <Pause size={16} />}
            </Button>
            <Button
              variant="ghost"
              size="icon"
              aria-label={theme === "dark" ? "切换浅色主题" : "切换深色主题"}
              onClick={() => setTheme(theme === "dark" ? "light" : "dark")}
            >
              {theme === "dark" ? <Sun size={17} /> : <Moon size={17} />}
            </Button>
          </div>
        </header>
        <main className="main-content">
          <Routes>
            <Route
              path="/"
              element={
                <Dashboard
                  monitor={monitor}
                  host={host.data}
                  services={services.data}
                  serviceError={services.error?.message}
                />
              }
            />
            <Route
              path="/services"
              element={
                <Services
                  services={services.data}
                  error={services.error?.message}
                  loading={services.isFetching}
                />
              }
            />
            <Route
              path="/system"
              element={
                <System
                  host={host.data}
                  snapshot={monitor.latest}
                  error={host.error?.message}
                />
              }
            />
            <Route path="/storage" element={<Storage />} />
            <Route path="/vhosts" element={<Vhosts />} />
            <Route path="/docker" element={<Docker />} />
            <Route path="*" element={<Navigate to="/" replace />} />
          </Routes>
        </main>
        <footer className="main-footer">
          <span>
            LNMP GoPanel <span className="version">v0.1.0</span>
          </span>
          <span>
            <ShieldCheck size={13} />
            本机管理 · 会话已保护
          </span>
        </footer>
      </div>
    </div>
  );
}
