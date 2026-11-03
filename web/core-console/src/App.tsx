import { lazy, Suspense, useCallback, useEffect, useState } from "react";
import {
  Navigate,
  Route,
  Routes,
  useLocation,
  useSearchParams,
} from "react-router-dom";
import { RefreshCw } from "lucide-react";
import { AppSidebar, navigation } from "./components/layout/app-sidebar";
import {
  SidebarProvider,
  SidebarInset,
  SidebarTrigger,
} from "./components/ui/sidebar";
import { Separator } from "./components/ui/separator";
import { Button } from "./components/ui/button";
import { api, errorText } from "./api";
import type { NetworkState, RouteDocument } from "./api";
import { Mark, Loading } from "./components";
import Login from "./pages/Login";
import ThemeToggle from "./ThemeToggle";
import PageBoundary from "./PageBoundary";
const Overview = lazy(() => import("./pages/Overview"));
const Agents = lazy(() => import("./pages/Agents"));
const Nodes = lazy(() => import("./pages/Nodes"));
const System = lazy(() => import("./pages/System"));
const Box = lazy(() => import("./pages/Box"));
const MqttUsers = lazy(() => import("./pages/MqttUsers"));
const Audio = lazy(() => import("./pages/Audio"));
const Reviews = lazy(() => import("./pages/Reviews"));
export default function App() {
  const [auth, setAuth] = useState<"checking" | "in" | "out" | "error">(
      "checking",
    ),
    [state, setState] = useState<NetworkState | null>(null),
    [routes, setRoutes] = useState<RouteDocument | null>(null),
    [error, setError] = useState(""),
    [busy, setBusy] = useState(false),
    [lastSync, setLastSync] = useState<Date | null>(null);
  const location = useLocation();
  const independent = [
    "/box",
    "/box/",
    "/messages",
    "/apps",
    "/inbox",
    "/inbox/",
    "/mqtt",
    "/reviews",
  ].includes(location.pathname);
  const session = useCallback(() => {
    setAuth("checking");
    api("/auth/session")
      .then(() => setAuth("in"))
      .catch((e) => setAuth(e.status === 401 ? "out" : "error"));
  }, []);
  useEffect(() => {
    session();
    const expired = () => {
      setAuth("out");
      setState(null);
      setRoutes(null);
    };
    window.addEventListener("orbit:unauthorized", expired);
    return () => window.removeEventListener("orbit:unauthorized", expired);
  }, [session]);
  const refresh = useCallback(async () => {
    setBusy(true);
    try {
      const [s, r] = await Promise.all([
        api<NetworkState>("/state"),
        api<RouteDocument>("/routes"),
      ]);
      setState(s);
      setRoutes(r);
      setError("");
      setLastSync(new Date());
    } catch (e) {
      setError(errorText(e));
    } finally {
      setBusy(false);
    }
  }, []);
  useEffect(() => {
    if (auth !== "in" || independent) return;
    let active = true;
    let timer: ReturnType<typeof setTimeout>;
    async function poll() {
      try {
        const s = await api<NetworkState>("/state");
        if (active) {
          setState(s);
          setError("");
          setLastSync(new Date());
        }
      } catch (e) {
        if (active) setError(errorText(e));
      } finally {
        if (active) timer = setTimeout(poll, document.hidden ? 30000 : 5000);
      }
    }
    void refresh();
    timer = setTimeout(poll, 5000);
    return () => {
      active = false;
      clearTimeout(timer);
    };
  }, [auth, refresh, independent]);
  const updateNodeLabel = useCallback((id: string, label: string) => {
    setState((current) =>
      current
        ? { ...current, node_labels: { ...current.node_labels, [id]: label } }
        : current,
    );
  }, []);
  const updateNodeArchive = useCallback((id: string, archived: boolean) => {
    setState((current) =>
      current
        ? {
            ...current,
            archived_nodes: archived
              ? [...new Set([...(current.archived_nodes ?? []), id])]
              : (current.archived_nodes?.filter((item) => item !== id) ?? []),
          }
        : current,
    );
  }, []);
  async function logout() {
    try {
      if (
        !window.dispatchEvent(
          new Event("orbit:beforelogout", { cancelable: true }),
        )
      )
        return;
      const cache = await import("./box/controller.js");
      if (!(await cache.confirmBoxLogout())) return;
      await api("/auth/logout", { method: "POST" });
      window.dispatchEvent(new Event("orbit:logout"));
      setState(null);
      setRoutes(null);
      setAuth("out");
      await cache.clearBoxCache();
    } catch (e) {
      setError(errorText(e));
    }
  }
  if (auth === "checking") return <Loading />;
  if (auth === "error")
    return (
      <div className="connection-error">
        <Mark />
        <h1>暂时无法连接 Core</h1>
        <p>请检查服务是否正在运行。</p>
        <button className="button" onClick={session}>
          重新连接
        </button>
      </div>
    );
  if (auth === "out") return <Login onLogin={() => setAuth("in")} />;
  const current =
    navigation.find(
      (item) =>
        item.path === location.pathname ||
        (item.path !== "/" && location.pathname.startsWith(item.path + "/")),
    ) ?? navigation[0];
  return (
    <SidebarProvider
      defaultOpen={!document.cookie.split("; ").includes("sidebar_state=false")}
    >
      <a
        className="sr-only focus:not-sr-only focus:fixed focus:top-2 focus:left-2 focus:z-50 focus:rounded-md focus:bg-background focus:p-3"
        href="#main-content"
      >
        跳到主要内容
      </a>
      <AppSidebar onLogout={logout} />
      <SidebarInset className="min-w-0">
        <header className="sticky top-0 z-20 flex h-16 shrink-0 items-center gap-3 border-b bg-background/95 px-4 backdrop-blur-sm md:px-6">
          <SidebarTrigger aria-label="切换导航" />
          <Separator orientation="vertical" className="mr-1 h-4" />
          <span className="hidden text-sm text-muted-foreground sm:inline">
            工作台
          </span>
          <span className="hidden text-muted-foreground/50 sm:inline">/</span>
          <span className="text-sm font-medium">{current.label}</span>
          <div className="ml-auto flex items-center gap-2">
            <ThemeToggle />
            {!independent && (
              <div className="flex items-center gap-2 text-xs text-muted-foreground">
                <span className={"status-dot " + (error ? "warn" : "")} />
                <span>
                  {error
                    ? "连接异常"
                    : lastSync
                      ? "已同步 " +
                        lastSync.toLocaleTimeString("zh-CN", {
                          hour: "2-digit",
                          minute: "2-digit",
                        })
                      : "同步中"}
                </span>
                <Button
                  variant="ghost"
                  size="icon"
                  onClick={refresh}
                  disabled={busy}
                  aria-label="刷新数据"
                >
                  <RefreshCw size={15} className={busy ? "spin" : ""} />
                </Button>
              </div>
            )}
          </div>
        </header>
        <main
          id="main-content"
          tabIndex={-1}
          className={
            "w-full min-w-0 max-w-none px-4 py-6 md:px-6 " +
            (location.pathname.startsWith("/nodes/") ? "device-page" : "")
          }
        >
          {error && (
            <div className="banner error" role="alert">
              {error}
              <button onClick={refresh}>重试</button>
            </div>
          )}
          <PageBoundary key={location.pathname}>
            <Suspense fallback={<Loading />}>
              {independent ? (
                <Routes>
                  <Route path="/box" element={<Box />} />
                  <Route path="/mqtt" element={<MqttUsers />} />
                  <Route path="/reviews" element={<Reviews />} />
                  <Route path="*" element={<Navigate to="/box" replace />} />
                </Routes>
              ) : state && routes ? (
                <Routes>
                  <Route
                    path="/"
                    element={<Overview state={state} document={routes} />}
                  />
                  <Route
                    path="/agents"
                    element={<Agents state={state} document={routes} />}
                  />
                  <Route
                    path="/nodes"
                    element={
                      <Nodes
                        state={state}
                        document={routes}
                        onChange={setRoutes}
                        onLabelChange={updateNodeLabel}
                        onArchiveChange={updateNodeArchive}
                      />
                    }
                  />
                  <Route
                    path="/nodes/:nodeId"
                    element={
                      <Nodes
                        state={state}
                        document={routes}
                        onChange={setRoutes}
                        onLabelChange={updateNodeLabel}
                        onArchiveChange={updateNodeArchive}
                      />
                    }
                  />
                  <Route path="/routes" element={<LegacyRoutes />} />
                  <Route
                    path="/system"
                    element={<System state={state} document={routes} />}
                  />
                  <Route path="/audio" element={<Audio state={state} />} />
                  <Route path="*" element={<Navigate to="/" replace />} />
                </Routes>
              ) : (
                <Loading />
              )}
            </Suspense>
          </PageBoundary>
        </main>
      </SidebarInset>
    </SidebarProvider>
  );
}

function LegacyRoutes() {
  const [params] = useSearchParams();
  const id = params.get("node");
  return (
    <Navigate to={id ? `/nodes/${encodeURIComponent(id)}` : "/nodes"} replace />
  );
}
