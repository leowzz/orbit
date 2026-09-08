import { ArrowUpRight, GitBranch, Server, Radio } from "lucide-react";
import { Link } from "react-router-dom";
import type { NetworkState, RouteDocument } from "../api";
import { modelName } from "../api";
import RouteFlow from "../RouteFlow";
import {
  Badge,
  DeviceIcon,
  Empty,
  PageHead,
  SectionTitle,
} from "../components";
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
  const unassigned = state.nodes.filter(
    (n) => !document.routes[n.nodeId],
  ).length;
  return (
    <>
      <PageHead
        eyebrow="YOUR NETWORK, AT A GLANCE"
        title="网络总览"
        description="主机的状态，设备的去向，在这里串联起来。"
        action={
          <Link to="/routes" className="button">
            管理转发规则
            <ArrowUpRight size={16} />
          </Link>
        }
      />
      <div className="overview-band">
        <div className="network-summary">
          <span className="eyebrow">NETWORK SNAPSHOT</span>
          <h2>
            {state.agents.length
              ? `${fresh} 个 Agent 正在提供新鲜数据`
              : "等待第一个 Agent 接入"}
          </h2>
          <p>
            {unassigned
              ? `${unassigned} 个已发现设备尚未配置转发规则。`
              : "设备与规则已就绪，数据按配置流转。"}
          </p>
          <Badge tone={fresh ? "good" : "neutral"}>
            {fresh ? "数据持续更新" : "等待数据"}
          </Badge>
        </div>
        <div className="stat">
          <span>
            <Server size={15} />
            数据来源
          </span>
          <strong>{String(state.agents.length).padStart(2, "0")}</strong>
          <small>Agents</small>
        </div>
        <div className="stat">
          <span>
            <Radio size={15} />
            接收设备
          </span>
          <strong>{String(state.nodes.length).padStart(2, "0")}</strong>
          <small>Nodes</small>
        </div>
        <div className="stat">
          <span>
            <GitBranch size={15} />
            转发规则
          </span>
          <strong>
            {String(Object.keys(document.routes).length).padStart(2, "0")}
          </strong>
          <small>Projection routes</small>
        </div>
      </div>
      <div className="overview-columns">
        <section>
          <SectionTitle title="数据流向" meta="LIVE ROUTING" link="/routes" />
          <RouteFlow state={state} document={document} />
        </section>
        <section>
          <SectionTitle
            title="设备一览"
            meta={String(state.nodes.length)}
            link="/nodes"
          />
          <div className="panel device-list">
            {state.nodes.slice(0, 5).map((n) => (
              <Link to="/nodes" className="device-row" key={n.nodeId}>
                <DeviceIcon model={n.modelId} />
                <div>
                  <strong>{n.nodeId}</strong>
                  <small>{modelName(n.modelId)}</small>
                </div>
                <span
                  className={
                    "mini-dot " + (document.routes[n.nodeId] ? "active" : "")
                  }
                />
                <ArrowUpRight size={14} />
              </Link>
            ))}
            {!state.nodes.length && (
              <Empty title="等待设备接入">设备发布自描述后会出现在这里。</Empty>
            )}
          </div>
          <div className="note-card">
            <span className="eyebrow">关于设备状态</span>
            <p>已发现，不等于在线。</p>
            <small>
              设备列表来自最近收到的自描述。请结合 Agent
              数据新鲜度判断当前状态。
            </small>
          </div>
        </section>
      </div>
    </>
  );
}
