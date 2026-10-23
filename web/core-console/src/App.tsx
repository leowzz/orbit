import { useCallback, useEffect, useState } from "react";
import {
  NavLink,
  Navigate,
  Route,
  Routes,
  useLocation,
  useSearchParams,
} from "react-router-dom";
import {
  Smartphone,
  Boxes,
  LayoutDashboard,
  KeyRound,
  LogOut,
  Menu,
  Mic,
  RefreshCw,
  Server,
  Settings2,
  X,
} from "lucide-react";
import { api, errorText } from "./api";
import type { NetworkState, RouteDocument } from "./api";
import { Mark, Loading } from "./components";
import Login from "./pages/Login";
import Overview from "./pages/Overview";
import Agents from "./pages/Agents";
import Nodes from "./pages/Nodes";
import System from "./pages/System";
import Apps from "./pages/Apps";
import MqttUsers from "./pages/MqttUsers";
import Audio from "./pages/Audio";
const navigation = [
  { path: "/", label: "总览", icon: LayoutDashboard },
  { path: "/agents", label: "电脑与来源", icon: Server },
  { path: "/nodes", label: "设备", icon: Boxes },
  { path: "/mqtt", label: "MQTT 账号", icon: KeyRound },
  { path: "/messages", label: "收件箱", icon: Smartphone },
  { path: "/audio", label: "音频记录", icon: Mic },
  { path: "/system", label: "系统信息", icon: Settings2 },
];
export default function App() {
  const [auth, setAuth] = useState<"checking" | "in" | "out" | "error">(
      "checking",
    ),
    [state, setState] = useState<NetworkState | null>(null),
    [routes, setRoutes] = useState<RouteDocument | null>(null),
    [error, setError] = useState(""),
    [busy, setBusy] = useState(false),
    [lastSync, setLastSync] = useState<Date | null>(null),
    [menu, setMenu] = useState(false);
  const [narrow, setNarrow] = useState(
    () => window.matchMedia("(max-width: 760px)").matches,
  );
  useEffect(() => {
    const media = window.matchMedia("(max-width: 760px)");
    const update = () => setNarrow(media.matches);
    const escape = (event: KeyboardEvent) => {
      if (event.key === "Escape") setMenu(false);
    };
    media.addEventListener("change", update);
    window.addEventListener("keydown", escape);
    return () => {
      media.removeEventListener("change", update);
      window.removeEventListener("keydown", escape);
    };
  }, []);
  const location = useLocation();
  useEffect(() => {
    setMenu(false);
  }, [location.pathname]);
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
    if (auth !== "in") return;
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
        if (active) timer = setTimeout(poll, 5000);
      }
    }
    void refresh();
    timer = setTimeout(poll, 5000);
    return () => {
      active = false;
      clearTimeout(timer);
    };
  }, [auth, refresh]);
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
      await api("/auth/logout", { method: "POST" });
      setState(null);
      setRoutes(null);
      setAuth("out");
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
    <div className="app-shell">
      <button
        className={"mobile-scrim " + (menu ? "visible" : "")}
        onClick={() => setMenu(false)}
        aria-label="关闭导航"
      />
      <aside
        id="main-navigation"
        inert={narrow && !menu}
        className={"sidebar " + (menu ? "open" : "")}
      >
        <div className="wordmark">
          <Mark small />
          Orbit<span>控制台</span>
          <button
            className="mobile-close icon-button"
            onClick={() => setMenu(false)}
            aria-label="关闭导航"
          >
            <X size={18} />
          </button>
        </div>
        <div className="workspace">
          <span className="workspace-dot" />
          <div>
            <strong>{state?.core_id ?? "Orbit"}</strong>
            <small>个人工作空间</small>
          </div>
        </div>
        <div className="nav-label">工作空间</div>
        <nav>
          {navigation.map((item) => (
            <NavLink key={item.path} to={item.path} end={item.path === "/"}>
              <item.icon size={17} strokeWidth={1.6} />
              <span>{item.label}</span>
            </NavLink>
          ))}
        </nav>
        <div className="sidebar-bottom">
          <div className="sidebar-note">
            <span className="status-dot" />
            Core 控制台<span>私有网络</span>
          </div>
          <button onClick={logout}>
            <LogOut size={16} />
            退出登录
          </button>
        </div>
      </aside>
      <div className="main-shell">
        <header className="topbar">
          <div>
            <button
              className="mobile-menu icon-button"
              onClick={() => setMenu(true)}
              aria-label="打开导航"
              aria-expanded={menu}
              aria-controls="main-navigation"
            >
              <Menu size={20} />
            </button>
            <span>工作空间</span>
            <span className="slash">/</span>
            <strong>{current.label}</strong>
          </div>
          <div className="sync">
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
            <button
              className="icon-button"
              onClick={refresh}
              disabled={busy}
              aria-label="刷新数据"
            >
              <RefreshCw size={15} className={busy ? "spin" : ""} />
            </button>
          </div>
        </header>
        <main
          className={
            location.pathname.startsWith("/nodes/") ? "device-page" : undefined
          }
        >
          {error && (
            <div className="banner error" role="alert">
              {error}
              <button onClick={refresh}>重试</button>
            </div>
          )}
          {state && routes ? (
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
              <Route path="/messages" element={<Apps />} />
              <Route path="/mqtt" element={<MqttUsers />} />
              <Route path="/audio" element={<Audio state={state} />} />
              <Route
                path="/apps"
                element={<Navigate to="/messages" replace />}
              />
              <Route path="*" element={<Navigate to="/" replace />} />
            </Routes>
          ) : (
            <Loading />
          )}
        </main>
        <footer className="app-footer">
          <span>Orbit 控制台</span>
        </footer>
      </div>
    </div>
  );
}

function LegacyRoutes() {
  const [params] = useSearchParams();
  const id = params.get("node");
  return (
    <Navigate to={id ? `/nodes/${encodeURIComponent(id)}` : "/nodes"} replace />
  );
}
