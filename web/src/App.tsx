import { useEffect, useState } from "react";
import {
  Navigate,
  NavLink,
  Route,
  Routes,
  useLocation,
} from "react-router-dom";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import {
  Activity,
  ChevronRight,
  Clock3,
  Container,
  LayoutDashboard,
  HardDrive,
  Globe2,
  Loader2,
  LogOut,
  FileText,
  Menu,
  Moon,
  Pause,
  PanelLeftClose,
  PanelLeftOpen,
  Play,
  Server,
  Settings2,
  ShieldCheck,
  Sun,
  Tag,
  X,
} from "lucide-react";
import { Tooltip } from "radix-ui";
import { toast, Toaster } from "sonner";
import { Button } from "@/components/ui/button";
import { Login } from "@/pages/login";
import { Dashboard } from "@/pages/dashboard";
import { Services } from "@/pages/services";
import { LNMPInstall } from "@/pages/lnmp-install";
import { System } from "@/pages/system";
import { Storage } from "@/pages/storage";
import { Vhosts } from "@/pages/vhosts";
import { Docker } from "@/pages/docker";
import { Cron } from "@/pages/cron";
import { Logs } from "@/pages/logs";
import { api, fetchSession, logout } from "@/lib/api";
import type { BuildInfo, Host, Service, Session } from "@/lib/types";
import { formatBuildTime } from "@/lib/build-info";
import { useMonitor } from "@/lib/use-monitor";
import { cn } from "@/lib/utils";
import { AccountSecurity } from "@/components/account-security";

type Theme = "light" | "dark";

export default function App() {
  const [theme, setTheme] = useState<Theme>(() =>
    document.documentElement.classList.contains("dark") ? "dark" : "light",
  );
  useEffect(() => {
    document.documentElement.classList.toggle("dark", theme === "dark");
    document
      .querySelector('meta[name="theme-color"]')
      ?.setAttribute("content", theme === "dark" ? "#101114" : "#f6f7f8");
    try {
      localStorage.setItem("theme", theme);
    } catch {
      // The theme still works for this visit when storage is unavailable.
    }
  }, [theme]);
  const client = useQueryClient();
  const session = useQuery({
    queryKey: ["session"],
    queryFn: () => fetchSession(),
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
      {session.data ? (
        <Workspace session={session.data} theme={theme} setTheme={setTheme} />
      ) : (
        <Login />
      )}
      <Toaster richColors theme={theme} position="bottom-right" />
    </>
  );
}
function Workspace({
  session,
  theme,
  setTheme,
}: {
  session: Session;
  theme: Theme;
  setTheme: (theme: Theme) => void;
}) {
  const monitor = useMonitor(session.csrf_token),
    client = useQueryClient();
  const { pathname } = useLocation();
  const [mobileOpen, setMobileOpen] = useState(false),
    [loggingOut, setLoggingOut] = useState(false);
  const [sidebarHidden, setSidebarHidden] = useState(() => {
    try {
      return localStorage.getItem("sidebar-hidden") === "true";
    } catch {
      return false;
    }
  });
  const [mobileViewport, setMobileViewport] = useState(
    () => window.matchMedia("(max-width: 720px)").matches,
  );
  useEffect(() => {
    try {
      localStorage.setItem("sidebar-hidden", String(sidebarHidden));
    } catch {
      // Navigation still works when browser storage is unavailable.
    }
  }, [sidebarHidden]);
  useEffect(() => {
    const media = window.matchMedia("(max-width: 720px)");
    const change = () => {
      setMobileViewport(media.matches);
      setMobileOpen(false);
    };
    media.addEventListener("change", change);
    return () => media.removeEventListener("change", change);
  }, []);
  const sidebarVisible = mobileViewport ? mobileOpen : !sidebarHidden;
  const [buildOpen, setBuildOpen] = useState(false);
  useEffect(() => {
    setBuildOpen(false);
  }, [sidebarVisible, pathname]);
  const build = useQuery({
    queryKey: ["build"],
    queryFn: () => api<BuildInfo>("/build"),
    staleTime: Infinity,
    retry: 1,
  });
  const buildVersion =
    build.data?.version || (build.isError ? "暂不可用" : "读取中…");
  const buildTime = build.data
    ? formatBuildTime(build.data.built_at)
    : build.isError
      ? "暂不可用"
      : "读取中…";
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
    <div className={cn("workspace", sidebarHidden && "sidebar-hidden")}>
      <div
        className={cn("mobile-overlay", mobileOpen && "open")}
        aria-hidden="true"
        onClick={() => setMobileOpen(false)}
      />
      <aside
        id="workspace-navigation"
        className={cn("sidebar", mobileOpen && "open")}
        aria-hidden={!sidebarVisible}
        inert={!sidebarVisible}
      >
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
        <nav>
          {[
            { to: "/", label: "系统概览", icon: LayoutDashboard },
            { to: "/vhosts", label: "虚拟主机", icon: Globe2 },
            { to: "/services", label: "LNMP 服务", icon: Settings2 },
            { to: "/docker", label: "Docker 管理", icon: Container },
            { to: "/storage", label: "硬盘空间", icon: HardDrive },
            { to: "/cron", label: "定时任务", icon: Clock3 },
            { to: "/logs", label: "日志读取", icon: FileText },
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
          <div className="sidebar-profile">
            <Tooltip.Provider delayDuration={150}>
              <Tooltip.Root
                open={buildOpen && sidebarVisible}
                onOpenChange={setBuildOpen}
              >
                <Tooltip.Trigger asChild>
                  <button
                    type="button"
                    className="avatar"
                    aria-label="查看版本和构建时间"
                    onClick={(event) => {
                      event.preventDefault();
                      setBuildOpen(true);
                    }}
                  >
                    {session.username.slice(0, 1).toUpperCase()}
                  </button>
                </Tooltip.Trigger>
                <Tooltip.Portal>
                  <Tooltip.Content
                    className="profile-build-tooltip"
                    aria-hidden={!buildOpen || !sidebarVisible}
                    side="top"
                    align="start"
                    sideOffset={10}
                    collisionPadding={16}
                  >
                    <dl className="profile-build-info" aria-label="构建信息">
                      <div>
                        <dt>
                          <Tag size={14} aria-hidden="true" />
                          版本
                        </dt>
                        <dd className="build-version">{buildVersion}</dd>
                      </div>
                      <div>
                        <dt>
                          <Clock3 size={14} aria-hidden="true" />
                          构建时间
                        </dt>
                        <dd className="build-time">{buildTime}</dd>
                      </div>
                    </dl>
                    <Tooltip.Arrow className="profile-build-arrow" />
                  </Tooltip.Content>
                </Tooltip.Portal>
              </Tooltip.Root>
            </Tooltip.Provider>
            <div>
              <strong>{session.username}</strong>
              <span>系统管理员</span>
            </div>
            <AccountSecurity username={session.username} />
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
              className="sidebar-toggle"
              aria-label={sidebarHidden ? "显示导航" : "隐藏导航"}
              aria-controls="workspace-navigation"
              aria-expanded={!sidebarHidden}
              onClick={() => setSidebarHidden((hidden) => !hidden)}
            >
              {sidebarHidden ? <PanelLeftOpen /> : <PanelLeftClose />}
            </Button>
            <Button
              variant="ghost"
              size="icon"
              className="mobile-menu"
              aria-label="打开导航"
              aria-controls="workspace-navigation"
              aria-expanded={mobileOpen}
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
          <div className="page-transition" key={pathname}>
            <Routes>
              <Route path="/lnmp/install" element={<LNMPInstall />} />
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
              <Route path="/cron" element={<Cron />} />
              <Route path="/logs" element={<Logs />} />
              <Route path="*" element={<Navigate to="/" replace />} />
            </Routes>
          </div>
        </main>
        <footer className="main-footer">
          <span>LNMP GoPanel</span>
          <span>
            <ShieldCheck size={13} />
            本机管理 · 会话已保护
          </span>
        </footer>
      </div>
    </div>
  );
}
