import { useEffect, useState } from "react";
import { ArrowUpRight, GitBranch, Server, Radio } from "lucide-react";
import { Link } from "react-router-dom";
import type { AppDevice, NetworkState, RouteDocument } from "../api";
import { api, modelName, visibleDeviceRoutes } from "../api";
import RouteFlow from "../RouteFlow";
import { Badge, DeviceIcon, Empty, SectionTitle } from "../components";
export default function Overview({
  state,
  document,
}: {
  state: NetworkState;
  document: RouteDocument;
}) {
  const fresh = state.agents.filter(
    (a) => a.usage_fresh || a.codex_fresh,
  ).length;
  const [apps, setApps] = useState<AppDevice[]>([]),
    [deviceError, setDeviceError] = useState("");
  useEffect(() => {
    let active = true;
    let timer: ReturnType<typeof setTimeout>;
    async function poll() {
      try {
        const result = await api<{ devices?: AppDevice[] }>("/app/devices");
        if (active) {
          setApps(result.devices ?? []);
          setDeviceError("");
        }
      } catch {
        if (active)
          setDeviceError("App 设备列表未能加载，设备数量可能不完整。");
      }
      if (active) timer = setTimeout(poll, 5000);
    }
    void poll();
    return () => {
      active = false;
      clearTimeout(timer);
    };
  }, []);
  const devices = new Map(
    state.nodes.map((n) => [
      n.nodeId,
      {
        id: n.nodeId,
        label: state.node_labels?.[n.nodeId] || n.nodeId,
        model: n.modelId,
        description: modelName(n.modelId),
      },
    ]),
  );
  for (const app of apps)
    devices.set(app.id, {
      id: app.id,
      label: app.label || app.id,
      model: app.platform === "web" ? "web" : "android",
      description: `Orbit App · ${app.platform}`,
    });
  for (const [id, route] of Object.entries(document.routes))
    if (!devices.has(id))
      devices.set(id, {
        id,
        label: state.node_labels?.[id] || id,
        model: route.profile.endsWith("-oled-128x32") ? "oled-128x32" : "web",
        description: "预配置设备",
      });
  for (const id of state.archived_nodes ?? []) devices.delete(id);
  const visibleDocument = visibleDeviceRoutes(document, state);
  const unassigned = [...devices.keys()].filter(
    (id) => !document.routes[id],
  ).length;
  return (
    <>
      {deviceError && (
        <p className="banner error" role="alert">
          {deviceError}
        </p>
      )}
      <header className="overview-header">
        <h1>网络总览</h1>
        <div className="overview-metrics" aria-label="网络指标">
          <Link to="/agents">
            <Server size={17} />
            <span>数据来源</span>
            <strong>{state.agents.length}</strong>
          </Link>
          <Link to="/nodes">
            <Radio size={17} />
            <span>设备</span>
            <strong>{devices.size}</strong>
          </Link>
          <Link to="/nodes">
            <GitBranch size={17} />
            <span>数据配置</span>
            <strong>{Object.keys(visibleDocument.routes).length}</strong>
          </Link>
        </div>
        <div className="overview-header-actions">
          <Badge tone={fresh ? "good" : "neutral"}>
            {fresh ? `${fresh} 个来源数据新鲜` : "等待来源数据"}
          </Badge>
          <Link to="/nodes" className="button">
            管理设备 <ArrowUpRight size={16} />
          </Link>
        </div>
      </header>
      <div className="overview-columns">
        <section>
          <SectionTitle title="数据流向" link="/nodes" />
          <RouteFlow
            state={state}
            document={visibleDocument}
            labels={Object.fromEntries(
              [...devices].map(([id, device]) => [id, device.label]),
            )}
          />
        </section>
        <section className="overview-devices">
          <SectionTitle
            title="设备一览"
            meta={String(devices.size)}
            link="/nodes"
          />
          <div className="panel device-list">
            {[...devices.values()].slice(0, 5).map((n) => (
              <Link
                to={`/nodes/${encodeURIComponent(n.id)}`}
                className="device-row"
                key={n.id}
              >
                <DeviceIcon model={n.model} />
                <div>
                  <strong>{n.label}</strong>
                  <small>{n.description}</small>
                </div>
                <span
                  className={
                    "mini-dot " + (document.routes[n.id] ? "active" : "")
                  }
                />
                <ArrowUpRight size={14} />
              </Link>
            ))}
            {!devices.size && (
              <Empty
                title={
                  state.archived_nodes?.length
                    ? "没有未归档的设备"
                    : "等待设备接入"
                }
              >
                {state.archived_nodes?.length ? (
                  <Link className="text-link" to="/nodes?filter=archived">
                    查看已归档设备
                  </Link>
                ) : (
                  "设备发布自描述后会出现在这里。"
                )}
              </Empty>
            )}
          </div>
          <p className="overview-status-note">
            {unassigned
              ? `${unassigned} 个设备未设置状态摘要（不影响收件箱和快捷操作）。`
              : ""}
            已发现不等于在线，请结合来源数据新鲜度判断状态。
          </p>
        </section>
      </div>
    </>
  );
}
