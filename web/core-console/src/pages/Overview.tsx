import { useCallback, useEffect, useState } from "react";
import { ArrowUpRight } from "lucide-react";
import { Link } from "react-router-dom";
import type { AppDevice, NetworkState, RouteDocument } from "../api";
import { api, date, displayMode, modelName, sessionsEnabled } from "../api";
import { Badge, Empty } from "../components";
import DeviceStatus from "../DeviceStatus";

export default function Overview({
  state,
  document,
}: {
  state: NetworkState;
  document: RouteDocument;
}) {
  const [apps, setApps] = useState<AppDevice[] | null>(null);
  const [error, setError] = useState("");
  const load = useCallback(async () => {
    const result = await api<{ devices?: AppDevice[] }>("/app/devices");
    setApps(result.devices ?? []);
    setError("");
  }, []);
  useEffect(() => {
    let active = true;
    let timer: ReturnType<typeof setTimeout>;
    async function poll() {
      try {
        await load();
      } catch {
        if (active) setError("设备列表未能完整加载，请重试。");
      }
      if (active)
        timer = setTimeout(poll, window.document.hidden ? 30000 : 5000);
    }
    void poll();
    return () => {
      active = false;
      clearTimeout(timer);
    };
  }, [load]);
  const devices = new Map<
    string,
    {
      id: string;
      label: string;
      description: string;
      discovered: boolean;
      reportedAt?: string;
      app?: AppDevice;
    }
  >();
  for (const node of state.nodes)
    devices.set(node.nodeId, {
      id: node.nodeId,
      label: state.node_labels?.[node.nodeId] || node.nodeId,
      description: modelName(node.modelId),
      discovered: true,
      reportedAt: node.metadata?.producedAt,
    });
  for (const app of apps ?? [])
    devices.set(app.id, {
      id: app.id,
      label: app.label || app.id,
      description:
        app.platform === "web" ? "浏览器" : `Orbit App · ${app.platform}`,
      discovered: false,
      app,
    });
  for (const [id, route] of Object.entries(document.routes))
    if (!devices.has(id))
      devices.set(id, {
        id,
        label: state.node_labels?.[id] || id,
        description:
          route.profile === "metrics-xiaozhi-320x240"
            ? "小智彩屏"
            : route.profile.endsWith("-oled-128x32")
              ? "OLED 显示屏"
              : "预配置设备",
        discovered: false,
      });
  for (const id of state.archived_nodes ?? []) devices.delete(id);
  return (
    <div className="workbench-overview">
      <header className="workbench-heading">
        <h1>工作台</h1>
        <Link to="/nodes" className="button primary">
          管理设备 <ArrowUpRight size={15} />
        </Link>
      </header>
      {error && (
        <div className="banner error" role="alert">
          {error}
          <button
            onClick={() =>
              void load().catch(() =>
                setError("设备列表未能完整加载，请重试。"),
              )
            }
          >
            重试
          </button>
        </div>
      )}
      <section aria-labelledby="source-heading">
        <div className="workbench-section">
          <h2 id="source-heading">电脑与来源</h2>
          <Link className="text-link" to="/agents">
            管理来源 <ArrowUpRight size={14} />
          </Link>
        </div>
        {state.agents.length ? (
          <div className="workbench-table-wrap">
            <table className="workbench-table sources-table">
              <thead>
                <tr>
                  <th scope="col">电脑</th>
                  <th scope="col">数据新鲜度</th>
                  <th scope="col">最近上报</th>
                </tr>
              </thead>
              <tbody>
                {state.agents.map((agent) => (
                  <tr key={agent.id}>
                    <td>
                      <Link className="table-name" to="/agents">
                        {agent.state.hostLabel || agent.id}
                      </Link>
                    </td>
                    <td>
                      <Badge
                        tone={
                          agent.usage_fresh || agent.codex_fresh
                            ? "good"
                            : "warn"
                        }
                      >
                        {agent.usage_fresh || agent.codex_fresh
                          ? "有新鲜数据"
                          : "无新鲜数据"}
                      </Badge>
                    </td>
                    <td>
                      <time dateTime={agent.state.metadata?.producedAt}>
                        {date(agent.state.metadata?.producedAt)}
                      </time>
                    </td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        ) : (
          <div className="workbench-empty">
            <Empty title="等待来源接入">
              <Link className="text-link" to="/agents">
                查看电脑与来源
              </Link>
            </Empty>
          </div>
        )}
      </section>
      <section aria-labelledby="device-heading">
        <div className="workbench-section">
          <h2 id="device-heading">设备</h2>
        </div>
        {devices.size ? (
          <div className="workbench-table-wrap">
            <table className="workbench-table devices-table">
              <thead>
                <tr>
                  <th scope="col">设备名称</th>
                  <th scope="col">显示内容</th>
                  <th scope="col">数据来源</th>
                  <th scope="col">状态</th>
                </tr>
              </thead>
              <tbody>
                {[...devices.values()].map((device) => {
                  const route = document.routes[device.id];
                  const mode = route ? displayMode(route) : "";
                  const content = route
                    ? {
                        usage: "API 用量",
                        codex: "账号周限",
                        rotating: "循环显示",
                        sessions: "Codex 会话",
                        "": "状态摘要",
                      }[mode] + (sessionsEnabled(route) ? " · 会话" : "")
                    : device.app
                      ? "收件箱"
                      : "未配置";
                  const names = [
                    ...new Set(
                      route?.inputs.map(
                        (input) =>
                          state.agents.find(
                            (agent) => agent.id === input.agent_id,
                          )?.state.hostLabel || input.agent_id,
                      ) ?? [],
                    ),
                  ];
                  return (
                    <tr key={device.id}>
                      <td>
                        <Link
                          className="table-name"
                          to={`/nodes/${encodeURIComponent(device.id)}`}
                        >
                          {device.label}
                          <ArrowUpRight size={14} />
                        </Link>
                        <small>{device.description}</small>
                      </td>
                      <td>{content}</td>
                      <td>{names.join("、") || "—"}</td>
                      <td>
                        <DeviceStatus
                          app={device.app}
                          discovered={device.discovered}
                          reportedAt={device.reportedAt}
                        />
                      </td>
                    </tr>
                  );
                })}
              </tbody>
            </table>
          </div>
        ) : apps === null ? (
          <div className="workbench-empty" role="status">
            {error ? "设备列表未能加载" : "正在读取设备…"}
          </div>
        ) : (
          <div className="workbench-empty">
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
                <Link className="text-link" to="/nodes">
                  管理设备
                </Link>
              )}
            </Empty>
          </div>
        )}
      </section>
    </div>
  );
}
