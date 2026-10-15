import { useCallback, useEffect, useState } from "react";
import { Link, useNavigate, useParams } from "react-router-dom";
import { ArrowLeft, ArrowUpRight, Plus, Search, Settings2 } from "lucide-react";
import type { AppDevice, NetworkState, RouteDocument } from "../api";
import {
  api,
  date,
  modelName,
  displayMode,
  displayModes,
  sessionsEnabled,
} from "../api";
import { Badge, DeviceIcon, Empty, PageHead } from "../components";
import { Editor, type Target } from "./Routing";
import Apps, { DeviceForm, TokenDialog } from "./Apps";
import DeviceShortcuts from "./DeviceShortcuts";

type Device = Target & { app?: AppDevice; model: string; discovered: boolean };
export default function Nodes({
  state,
  document,
  onChange,
}: {
  state: NetworkState;
  document: RouteDocument;
  onChange: (d: RouteDocument) => void;
}) {
  const { nodeId } = useParams<{ nodeId: string }>();
  const navigate = useNavigate();
  const [appDevices, setAppDevices] = useState<AppDevice[] | null>(null),
    [enabled, setEnabled] = useState(false);
  const [error, setError] = useState(""),
    [query, setQuery] = useState(""),
    [filter, setFilter] = useState("all");
  const [tab, setTab] = useState("content"),
    [editing, setEditing] = useState(false),
    [creating, setCreating] = useState(false),
    [preconfigure, setPreconfigure] = useState(false);
  const [secret, setSecret] = useState<{ id: string; token: string } | null>(
    null,
  );
  const load = useCallback(async () => {
    try {
      const result = await api<{ enabled: boolean; devices?: AppDevice[] }>(
        "/app/devices",
      );
      setAppDevices(result.devices ?? []);
      setEnabled(result.enabled);
      setError("");
    } catch {
      setError("未能加载 App 设备，请重试。已发现设备仍可查看。");
    }
  }, []);
  useEffect(() => {
    let active = true;
    let timer: ReturnType<typeof setTimeout>;
    async function poll() {
      await load();
      if (active) timer = setTimeout(poll, 5000);
    }
    void poll();
    return () => {
      active = false;
      clearTimeout(timer);
    };
  }, [load]);
  useEffect(() => {
    setTab("content");
    setEditing(false);
  }, [nodeId]);
  const devices = new Map<string, Device>();
  for (const node of state.nodes)
    devices.set(node.nodeId, {
      id: node.nodeId,
      label: node.nodeId,
      profile:
        (
          {
            web: "overview-web",
            android: "overview-android",
            "oled-128x32": "usage-oled-128x32",
          } as Record<string, string>
        )[node.modelId] ?? "overview-web",
      model: node.modelId,
      discovered: true,
    });
  for (const app of appDevices ?? [])
    devices.set(app.id, {
      id: app.id,
      label: app.label || app.id,
      profile: "overview-app",
      model: app.platform === "web" ? "web" : "android",
      revoked: app.revoked,
      app,
      discovered: true,
    });
  for (const [id, route] of Object.entries(document.routes))
    if (!devices.has(id))
      devices.set(id, {
        id,
        label: id,
        profile: route.profile,
        model: route.profile.endsWith("-oled-128x32")
          ? "oled-128x32"
          : route.profile === "overview-web"
            ? "web"
            : "android",
        discovered: false,
      });
  const current = nodeId ? devices.get(nodeId) : null;
  const targets = [...devices.values()];
  const route = nodeId ? document.routes[nodeId] : undefined;
  const node = state.nodes.find((n) => n.nodeId === nodeId);
  const filtered = targets.filter(
    (d) =>
      (filter === "all" ||
        (filter === "app" ? !!d.app : !d.app && d.model === filter)) &&
      `${d.label} ${d.id}`.toLowerCase().includes(query.toLowerCase()),
  );
  return (
    <>
      {error && (
        <div className="banner error" role="alert">
          {error}
          <button onClick={() => void load()}>重试</button>
        </div>
      )}
      {nodeId ? (
        current ? (
          <>
            <Link className="text-link" to="/nodes">
              <ArrowLeft size={15} />
              全部设备
            </Link>
            <PageHead
              title={current.label}
              description={
                current.app
                  ? `Orbit App · ${current.app.platform}`
                  : modelName(current.model)
              }
            />
            {current.revoked && (
              <div className="banner error">
                此设备的访问已撤销。可在「连接与访问」中重新授权。
              </div>
            )}
            <div className="toolbar">
              <div className="tabs" role="tablist" aria-label="设备设置">
                {[
                  ["content", "内容与显示"],
                  ["shortcuts", "快捷操作"],
                  ["access", "连接与访问"],
                ].map(([id, label]) => (
                  <button
                    key={id}
                    role="tab"
                    aria-selected={tab === id}
                    className={tab === id ? "selected" : ""}
                    onClick={() => setTab(id)}
                  >
                    {label}
                  </button>
                ))}
              </div>
            </div>
            {tab === "content" && (
              <div className="device-section panel">
                <div className="device-section-head">
                  <h2>数据内容与展示</h2>
                  <button
                    className="button secondary"
                    disabled={current.revoked || appDevices === null}
                    onClick={() => setEditing(true)}
                  >
                    <Settings2 size={16} />
                    {route ? "调整配置" : "配置数据内容"}
                  </button>
                </div>
                {route ? (
                  <>
                    <dl className="device-facts">
                      <div>
                        <dt>指标展示</dt>
                        <dd>
                          {displayModes.find(
                            (m) => m.value === displayMode(route),
                          )?.label ?? "综合展示"}
                          {displayMode(route) === "rotating"
                            ? ` · 每 ${route.rotation_seconds || 3} 秒`
                            : ""}
                        </dd>
                      </div>
                      {route.inputs.map((input) => (
                        <div key={input.observation_type}>
                          <dt>
                            {input.observation_type === "usage"
                              ? "API 用量来源"
                              : "Codex 来源"}
                          </dt>
                          <dd>
                            {state.agents.find((a) => a.id === input.agent_id)
                              ?.state.hostLabel || input.agent_id}
                          </dd>
                        </div>
                      ))}
                      {!route.profile.endsWith("-oled-128x32") && (
                        <div>
                          <dt>会话详情与会话打开操作</dt>
                          <dd>
                            {sessionsEnabled(route)
                              ? "已开启（仍需本机允许执行）"
                              : "已关闭"}
                          </dd>
                        </div>
                      )}
                    </dl>
                    <p className="field-hint">
                      更换这里的数据来源不会改变快捷配对。
                    </p>
                  </>
                ) : (
                  <Empty title="尚未设置数据内容">
                    需要状态摘要时，在这里选择来源。不影响收件箱和独立快捷操作。
                  </Empty>
                )}
              </div>
            )}
            <div hidden={tab !== "shortcuts"}>
              {current.model === "oled-128x32" ? (
                <div className="panel device-section">
                  <p>此设备只显示指标，不提供快捷操作。</p>
                </div>
              ) : enabled ? (
                <DeviceShortcuts
                  key={current.id}
                  nodeId={current.id}
                  revoked={!!current.revoked}
                  nfcd={
                    current.profile === "overview-app" &&
                    current.app?.platform === "android"
                  }
                />
              ) : (
                <div className="panel device-section">
                  <p>
                    请先在 Core 部署配置中启用 App 服务，以管理和同步快捷操作。
                  </p>
                </div>
              )}
            </div>
            {tab === "access" && (
              <>
                {current.app && (
                  <Apps
                    deviceId={current.id}
                    onChanged={() => void load()}
                    onRoutesChange={onChange}
                  />
                )}
                <div className="device-section panel">
                  <h2>状态与诊断</h2>
                  <dl className="device-facts">
                    <div>
                      <dt>设备 ID</dt>
                      <dd className="mono">{current.id}</dd>
                    </div>
                    <div>
                      <dt>接入方式</dt>
                      <dd>{current.app ? "App 令牌 · HTTP/SSE" : "MQTT"}</dd>
                    </div>
                    {node && (
                      <>
                        <div>
                          <dt>固件版本</dt>
                          <dd>{node.firmwareVersion}</dd>
                        </div>
                        <div>
                          <dt>硬件变体</dt>
                          <dd>{node.variantId}</dd>
                        </div>
                        <div>
                          <dt>自描述更新</dt>
                          <dd>{date(node.metadata?.producedAt)}</dd>
                        </div>
                      </>
                    )}
                  </dl>
                  {!current.app && (
                    <p className="field-hint">
                      MQTT 凭据在设备本机与 Broker
                      配置。已发现自描述不代表此刻在线。
                    </p>
                  )}
                </div>
              </>
            )}
            {editing && (
              <Editor
                key={current.id}
                editing={{ id: current.id, route }}
                state={state}
                targets={targets}
                document={document}
                onClose={() => setEditing(false)}
                onSaved={(d) => {
                  onChange(d);
                  setEditing(false);
                }}
              />
            )}
          </>
        ) : (
          <Empty title={appDevices === null ? "正在读取设备…" : "未找到设备"}>
            <Link to="/nodes">返回设备列表</Link>
          </Empty>
        )
      ) : (
        <>
          <PageHead
            title="设备"
            description="选择一台设备，配置它的数据内容、快捷操作和连接访问。"
            action={
              <>
                <button
                  className="button secondary"
                  disabled={appDevices === null}
                  onClick={() => setPreconfigure(true)}
                >
                  预配置设备
                </button>
                <button
                  className="button primary"
                  disabled={!enabled || appDevices === null}
                  onClick={() => setCreating(true)}
                >
                  <Plus size={16} />
                  添加 App 设备
                </button>
              </>
            }
          />
          <div className="toolbar">
            <div className="tabs">
              {[
                ["all", "全部设备"],
                ["app", "Orbit App"],
                ["oled-128x32", "OLED"],
                ["web", "Web"],
                ["android", "旧版 Android"],
              ].map(([id, label]) => (
                <button
                  key={id}
                  className={filter === id ? "selected" : ""}
                  onClick={() => setFilter(id)}
                >
                  {label}
                </button>
              ))}
            </div>
            <label className="search">
              <Search size={16} />
              <input
                aria-label="搜索设备"
                placeholder="搜索设备名称或 ID"
                value={query}
                onChange={(e) => setQuery(e.target.value)}
              />
            </label>
          </div>
          <div className="node-grid">
            {filtered.map((device) => (
              <article className="panel node-card" key={device.id}>
                <div className="node-card-top">
                  <DeviceIcon model={device.model} />
                  <Badge tone={device.revoked ? "warn" : "neutral"}>
                    {device.revoked
                      ? "已撤销访问"
                      : device.app
                        ? device.app.activity.connections > 0
                          ? "已连接"
                          : "未连接"
                        : device.discovered
                          ? "已发现"
                          : "预配置"}
                  </Badge>
                </div>
                <h2>{device.label}</h2>
                <p>
                  {device.app
                    ? `Orbit App · ${device.app.platform}`
                    : modelName(device.model)}
                </p>
                <div className="node-route">
                  <span>
                    {document.routes[device.id]
                      ? "已设置数据内容"
                      : "尚未设置数据内容"}
                  </span>
                  <small>快捷操作独立配置</small>
                </div>
                <Link
                  className="node-link"
                  to={`/nodes/${encodeURIComponent(device.id)}`}
                >
                  管理设备
                  <ArrowUpRight size={15} />
                </Link>
              </article>
            ))}
          </div>
          {!filtered.length && (
            <Empty title="没有匹配的设备">
              添加 App 设备，或等待 MQTT 设备发布自描述。
            </Empty>
          )}
          {!enabled && (
            <p className="footnote">App 服务尚未启用；当前可查看 MQTT 设备。</p>
          )}
        </>
      )}
      {preconfigure && (
        <Editor
          editing={{ id: "" }}
          state={state}
          targets={targets}
          document={document}
          onClose={() => setPreconfigure(false)}
          onSaved={(d) => {
            const added = Object.keys(d.routes).find(
              (id) => !document.routes[id],
            );
            onChange(d);
            setPreconfigure(false);
            if (added) navigate(`/nodes/${encodeURIComponent(added)}`);
          }}
        />
      )}
      {creating && (
        <DeviceForm
          onClose={() => setCreating(false)}
          onCreated={(s) => {
            setCreating(false);
            setSecret(s);
            void load();
          }}
        />
      )}
      {secret && (
        <TokenDialog
          secret={secret}
          onClose={() => {
            setSecret(null);
            navigate(`/nodes/${encodeURIComponent(secret.id)}`);
          }}
        />
      )}
    </>
  );
}
