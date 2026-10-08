import { useCallback, useEffect, useState } from "react";
import {
  NavLink,
  Navigate,
  Route,
  Routes,
  useLocation,
} from "react-router-dom";
import {
  ArrowLeftRight,
  Boxes,
  LayoutDashboard,
  LogOut,
  Menu,
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
import Routing from "./pages/Routing";
import System from "./pages/System";
const navigation = [
  { path: "/", label: "总览", sub: "Overview", icon: LayoutDashboard },
  { path: "/agents", label: "Agents", sub: "数据来源", icon: Server },
  { path: "/nodes", label: "Nodes", sub: "接收设备", icon: Boxes },
  { path: "/routes", label: "转发规则", sub: "Routing", icon: ArrowLeftRight },
  { path: "/system", label: "系统信息", sub: "System", icon: Settings2 },
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
    navigation.find((item) => item.path === location.pathname) ?? navigation[0];
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
          orbit<span> / core</span>
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
            <strong>{state?.core_id ?? "Orbit Network"}</strong>
            <small>PERSONAL NETWORK</small>
          </div>
        </div>
        <div className="nav-label">工作空间</div>
        <nav>
          {navigation.map((item) => (
            <NavLink key={item.path} to={item.path} end={item.path === "/"}>
              <item.icon size={17} strokeWidth={1.6} />
              <span>{item.label}</span>
              {item.path === "/nodes" && state && <em>{state.nodes.length}</em>}
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
        <main>
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
                element={<Nodes state={state} document={routes} />}
              />
              <Route
                path="/routes"
                element={
                  <Routing
                    state={state}
                    document={routes}
                    onChange={setRoutes}
                  />
                }
              />
              <Route
                path="/system"
                element={<System state={state} document={routes} />}
              />
              <Route path="*" element={<Navigate to="/" replace />} />
            </Routes>
          ) : (
            <Loading />
          )}
        </main>
        <footer className="app-footer">
          <span>ORBIT / CORE CONSOLE</span>
          <span>观察 · 连接 · 投影</span>
        </footer>
      </div>
    </div>
  );
}
