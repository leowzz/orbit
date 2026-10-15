import { useCallback, useEffect, useState } from "react";
import {
  Link,
  useNavigate,
  useParams,
  useSearchParams,
} from "react-router-dom";
import {
  Archive,
  ArchiveRestore,
  ArrowLeft,
  ArrowUpRight,
  Plus,
  Search,
} from "lucide-react";
import type { AppDevice, NetworkState, RouteDocument } from "../api";
import { api, appConnectionLabel, date, modelName } from "../api";
import { Badge, DeviceIcon, Empty, PageHead } from "../components";
import DeviceContent from "./DeviceContent";
import DeviceName from "../DeviceName";
import DeviceSummary from "../DeviceSummary";
import Apps, { DeviceForm, TokenDialog } from "./Apps";
import DeviceShortcuts from "./DeviceShortcuts";

type Device = {
  id: string;
  label: string;
  profile: string;
  revoked?: boolean;
  app?: AppDevice;
  model: string;
  discovered: boolean;
};
export default function Nodes({
  state,
  document,
  onChange,
  onLabelChange,
  onArchiveChange,
}: {
  state: NetworkState;
  document: RouteDocument;
  onChange: (d: RouteDocument) => void;
  onLabelChange: (id: string, label: string) => void;
  onArchiveChange: (id: string, archived: boolean) => void;
}) {
  const { nodeId } = useParams<{ nodeId: string }>();
  const navigate = useNavigate();
  const [params, setParams] = useSearchParams();
  const filter = params.get("filter") || "all";
  const [archiveBusy, setArchiveBusy] = useState(false),
    [archiveError, setArchiveError] = useState("");
  const archived = new Set(state.archived_nodes ?? []);
  async function changeArchive(id: string, value: boolean) {
    setArchiveBusy(true);
    setArchiveError("");
    try {
      const result = await api<{ id: string; archived: boolean }>(
        `/nodes/${encodeURIComponent(id)}/archive`,
        { method: "PUT", body: JSON.stringify({ archived: value }) },
      );
      onArchiveChange(result.id, result.archived);
      if (!value && !known.has(id)) navigate("/nodes");
    } catch {
      setArchiveError("未能更新归档状态，请重试。");
    } finally {
      setArchiveBusy(false);
    }
  }
  const [appDevices, setAppDevices] = useState<AppDevice[] | null>(null),
    [enabled, setEnabled] = useState(false);
  const [error, setError] = useState(""),
    [query, setQuery] = useState("");
  const [tab, setTab] = useState("content"),
    [creating, setCreating] = useState(false);
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
    setArchiveError("");
  }, [nodeId]);
  const devices = new Map<string, Device>();
  for (const node of state.nodes)
    devices.set(node.nodeId, {
      id: node.nodeId,
      label: state.node_labels?.[node.nodeId] || node.nodeId,
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
        label: state.node_labels?.[id] || id,
        profile: route.profile,
        model: route.profile.endsWith("-oled-128x32")
          ? "oled-128x32"
          : route.profile === "overview-web"
            ? "web"
            : "android",
        discovered: false,
      });
  const known = new Set(devices.keys());
  for (const id of archived)
    if (!devices.has(id))
      devices.set(id, {
        id,
        label: state.node_labels?.[id] || id,
        profile: "",
        model: "unknown",
        discovered: false,
      });
  const current = nodeId ? devices.get(nodeId) : null;
  const targets = [...devices.values()];
  const node = state.nodes.find((n) => n.nodeId === nodeId);
  const filtered = targets.filter(
    (d) =>
      (filter === "archived" ? archived.has(d.id) : !archived.has(d.id)) &&
      (filter === "all" ||
        filter === "archived" ||
        (filter === "app"
          ? !!d.app
          : filter === "web"
            ? d.model === "web"
            : !d.app && d.model === filter)) &&
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
            <Link
              className="text-link"
              to={
                archived.has(current.id) ? "/nodes?filter=archived" : "/nodes"
              }
            >
              <ArrowLeft size={15} />
              {archived.has(current.id) ? "已归档设备" : "全部设备"}
            </Link>
            <DeviceName
              key={current.id}
              id={current.id}
              name={current.label}
              app={!!current.app}
              disabled={
                appDevices === null || archiveBusy || !known.has(current.id)
              }
              action={
                <button
                  className="button secondary"
                  disabled={archiveBusy}
                  aria-label={
                    archived.has(current.id) ? "恢复设备" : "归档设备"
                  }
                  title={
                    archived.has(current.id)
                      ? "恢复到默认设备列表和总览"
                      : "从默认列表和总览隐藏，保留同步和访问权限"
                  }
                  onClick={() =>
                    void changeArchive(current.id, !archived.has(current.id))
                  }
                >
                  {archived.has(current.id) ? (
                    <ArchiveRestore size={16} />
                  ) : (
                    <Archive size={16} />
                  )}
                  {archiveBusy
                    ? "处理中…"
                    : archived.has(current.id)
                      ? "恢复设备"
                      : "归档"}
                </button>
              }
              onSaved={(label) => {
                if (current.app)
                  setAppDevices(
                    (devices) =>
                      devices?.map((device) =>
                        device.id === current.id
                          ? { ...device, label }
                          : device,
                      ) ?? null,
                  );
                else onLabelChange(current.id, label);
              }}
              description={
                current.app
                  ? current.app.platform === "web"
                    ? "Web 浏览器"
                    : `Orbit App · ${current.app.platform}`
                  : current.model === "unknown"
                    ? "暂未发现设备"
                    : modelName(current.model)
              }
            />
            {archiveError && (
              <div className="banner error" role="alert">
                {archiveError}
              </div>
            )}
            {archived.has(current.id) && (
              <div className="banner" role="status">
                <span>
                  此设备已归档，从默认列表和总览隐藏。已有同步和访问权限保持不变。
                </span>
              </div>
            )}
            {!known.has(current.id) ? (
              <Empty title="暂未发现设备">
                恢复后，设备再次接入时会重新显示。
              </Empty>
            ) : (
              <>
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
                <div hidden={tab !== "content"}>
                  <DeviceContent
                    key={current.id}
                    nodeId={current.id}
                    profile={current.profile}
                    revoked={!!current.revoked || appDevices === null}
                    state={state}
                    document={document}
                    onChange={onChange}
                  />
                </div>
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
                        请先在 Core 部署配置中启用 App
                        服务，以管理和同步快捷操作。
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
                          <dd>
                            {current.app ? "App 令牌 · HTTP/SSE" : "MQTT"}
                          </dd>
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
              </>
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
            description="选择设备，管理显示内容和快捷操作。"
            action={
              <>
                <button
                  className="button primary"
                  disabled={!enabled || appDevices === null}
                  onClick={() => setCreating(true)}
                >
                  <Plus size={16} />
                  添加设备
                </button>
              </>
            }
          />
          <div className="toolbar">
            <div className="tabs">
              {[
                ["all", "全部设备"],
                ["archived", "已归档"],
                ["app", "Orbit App"],
                ["oled-128x32", "OLED"],
                ["web", "Web"],
                ["android", "旧版 Android"],
              ].map(([id, label]) => (
                <button
                  key={id}
                  className={filter === id ? "selected" : ""}
                  onClick={() => setParams(id === "all" ? {} : { filter: id })}
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
                    {archived.has(device.id)
                      ? "已归档"
                      : device.revoked
                        ? "已撤销访问"
                        : device.app
                          ? appConnectionLabel(device.app.activity)
                          : device.discovered
                            ? "已发现"
                            : "预配置"}
                  </Badge>
                </div>
                <h2>{device.label}</h2>
                <p>
                  {device.app
                    ? device.app.platform === "web"
                      ? "Web 浏览器"
                      : `Orbit App · ${device.app.platform}`
                    : device.model === "unknown"
                      ? "暂未发现设备"
                      : modelName(device.model)}
                </p>
                <DeviceSummary
                  route={document.routes[device.id]}
                  agents={state.agents}
                  app={!!device.app}
                />
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
            <Empty
              title={
                appDevices === null
                  ? error
                    ? "暂时无法加载设备"
                    : "正在加载设备…"
                  : filter === "archived" && !query
                    ? "没有已归档的设备"
                    : filter === "all" && !query && archived.size > 0
                      ? "没有未归档的设备"
                      : query || filter !== "all"
                        ? "没有匹配的设备"
                        : "连接第一台设备"
              }
            >
              {error && appDevices === null
                ? "请重试加载设备列表。"
                : filter === "archived" && !query
                  ? "在设备详情中归档后，可在这里查看和恢复。"
                  : filter === "all" && !query && archived.size > 0
                    ? "已归档的设备可在「已归档」中查看和恢复。"
                    : query || filter !== "all"
                      ? "试试其他名称，或切换到全部设备。"
                      : "添加 Orbit App 设备；显示屏接入后会自动出现在这里。"}
            </Empty>
          )}
          {!enabled && appDevices !== null && (
            <p className="footnote">App 服务尚未启用；当前可查看 MQTT 设备。</p>
          )}
        </>
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
