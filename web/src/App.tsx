import { Button } from "@/components/ui/button";
import { LoadBoundary } from "@/components/load-boundary";
import { fetchSession } from "@/lib/api";
import { Login } from "@/pages/login";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { Activity, Server } from "lucide-react";
import { lazy, Suspense, useEffect, useState } from "react";
import { Toaster } from "sonner";
const Workspace = lazy(() => import("@/components/workspace"));
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
    queryFn: ({ signal }) => fetchSession(true, signal),
    retry: false,
    staleTime: 60000,
    refetchOnWindowFocus: true,
  });
  useEffect(() => {
    const expired = () => {
      void client.cancelQueries();
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
        <LoadBoundary message="工作空间加载失败，请重新加载页面。">
          <Suspense
            fallback={
              <div className="app-loading">
                <Activity size={28} />
                <p>正在加载工作空间…</p>
              </div>
            }
          >
            <Workspace
              session={session.data}
              theme={theme}
              setTheme={setTheme}
            />
          </Suspense>
        </LoadBoundary>
      ) : (
        <Login />
      )}
      <Toaster richColors theme={theme} position="bottom-right" />
    </>
  );
}
