import { ArrowUpRight, GitBranch, Server, Radio } from "lucide-react";
import { Link } from "react-router-dom";
import type { NetworkState, RouteDocument } from "../api";
import { modelName } from "../api";
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
  const unassigned = state.nodes.filter(
    (n) => !document.routes[n.nodeId],
  ).length;
  return (
    <>
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
            <span>接收设备</span>
            <strong>{state.nodes.length}</strong>
          </Link>
          <Link to="/routes">
            <GitBranch size={17} />
            <span>转发规则</span>
            <strong>{Object.keys(document.routes).length}</strong>
          </Link>
        </div>
        <div className="overview-header-actions">
          <Badge tone={fresh ? "good" : "neutral"}>
            {fresh ? `${fresh} 个来源数据新鲜` : "等待来源数据"}
          </Badge>
          <Link to="/routes" className="button">
            管理转发规则 <ArrowUpRight size={16} />
          </Link>
        </div>
      </header>
      <div className="overview-columns">
        <section>
          <SectionTitle title="数据流向" meta="LIVE ROUTING" link="/routes" />
          <RouteFlow state={state} document={document} />
        </section>
        <section className="overview-devices">
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
          <p className="overview-status-note">
            {unassigned ? `${unassigned} 个设备尚未配置规则。` : ""}
            已发现不等于在线，请结合来源数据新鲜度判断状态。
          </p>
        </section>
      </div>
    </>
  );
}
