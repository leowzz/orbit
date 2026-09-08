import { useState } from "react";
import { ArrowRight, Cpu, Pause, Play, Radio, Server } from "lucide-react";
import { Link } from "react-router-dom";
import type { NetworkState, RouteDocument } from "./api";
import { profileName } from "./api";
import { DeviceIcon, Empty } from "./components";

/** Motion represents source freshness, not individual MQTT packets or delivery. */
function Signal({ active, kind }: { active: boolean; kind: string }) {
  return (
    <svg
      className={`route-signal ${kind} ${active ? "is-fresh" : ""}`}
      viewBox="0 0 120 24"
      preserveAspectRatio="none"
      aria-hidden="true"
    >
      <path className="signal-track" d="M0 12 H120" />
      <path className="signal-pulse" d="M0 12 H120" pathLength="100" />
      <path className="signal-arrow" d="m110 8 5 4-5 4" />
    </svg>
  );
}

export default function RouteFlow({
  state,
  document,
}: {
  state: NetworkState;
  document: RouteDocument;
}) {
  const [paused, setPaused] = useState(false);
  const [expanded, setExpanded] = useState(false);
  const routes = Object.entries(document.routes);
  const visible = expanded ? routes : routes.slice(0, 5);
  const agents = new Map(state.agents.map((agent) => [agent.id, agent]));
  const nodes = new Map(state.nodes.map((node) => [node.nodeId, node]));

  if (!routes.length) {
    return (
      <div className="panel">
        <Empty
          title="还没有数据流向"
          action={
            <Link className="button" to="/routes">
              创建第一条规则 <ArrowRight size={15} />
            </Link>
          }
        >
          选择来源 Agent，把数据送到你的设备。
        </Empty>
      </div>
    );
  }

  return (
    <div className={`panel routing-map ${paused ? "is-paused" : ""}`}>
      <div className="routing-toolbar">
        <div className="routing-legend" aria-label="数据类型图例">
          <span className="usage">
            <i />
            Usage
          </span>
          <span className="codex">
            <i />
            Codex
          </span>
        </div>
        <button
          type="button"
          className="routing-motion"
          aria-pressed={paused}
          onClick={() => setPaused(!paused)}
        >
          {paused ? <Play size={14} /> : <Pause size={14} />}
          {paused ? "播放动画" : "暂停动画"}
        </button>
      </div>
      <div className="routing-columns" aria-hidden="true">
        <span>来源 Agent</span>
        <span>Core 分流</span>
        <span>目标 Node</span>
      </div>
      <div className="routing-routes">
        {visible.map(([id, route], index) => {
          const node = nodes.get(id);
          const inputs = route.inputs.map((input) => {
            const agent = agents.get(input.agent_id);
            const fresh =
              !!agent?.[
                input.observation_type === "usage"
                  ? "usage_fresh"
                  : "codex_fresh"
              ];
            return { ...input, agent, fresh };
          });
          const hasFresh = inputs.some((input) => input.fresh);
          const status = document.publish_pending
            ? "视图投递待重试"
            : !node
              ? "等待设备自描述"
              : hasFresh
                ? "来源数据新鲜"
                : "等待新鲜数据";
          return (
            <Link
              className={`routing-route ${hasFresh ? "has-fresh" : ""}`}
              to="/routes"
              key={id}
              aria-label={`${id}，${status}，查看转发规则`}
            >
              <div className="routing-route-meta">
                <span className="routing-sequence">
                  {String(index + 1).padStart(2, "0")}
                </span>
                <span
                  className={`routing-status ${hasFresh && node && !document.publish_pending ? "fresh" : ""}`}
                >
                  <i />
                  {status}
                </span>
                <ArrowRight size={14} />
              </div>
              <div className="routing-chain">
                <div className="routing-inputs">
                  {inputs.map((input) => (
                    <div
                      className={`routing-input ${input.observation_type}`}
                      key={input.observation_type}
                    >
                      <div className="routing-source">
                        <span className="routing-source-icon">
                          <Server size={16} />
                        </span>
                        <div>
                          <strong>
                            {input.agent?.state.hostLabel || input.agent_id}
                          </strong>
                          <small>
                            {input.observation_type === "usage"
                              ? "Usage"
                              : "Codex"}{" "}
                            ·{" "}
                            {input.fresh
                              ? "新鲜"
                              : input.agent
                                ? "暂无新鲜数据"
                                : "未发现"}
                          </small>
                        </div>
                      </div>
                      <Signal
                        active={input.fresh}
                        kind={input.observation_type}
                      />
                    </div>
                  ))}
                </div>
                <div className="routing-core" title={`Core: ${state.core_id}`}>
                  <span className="routing-core-ring" />
                  <Cpu size={21} />
                  <small>CORE</small>
                </div>
                <Signal
                  active={hasFresh && !!node && !document.publish_pending}
                  kind="output"
                />
                <div className="routing-target">
                  {node ? (
                    <DeviceIcon model={node.modelId} />
                  ) : (
                    <span className="device-icon">
                      <Radio size={20} />
                    </span>
                  )}
                  <div>
                    <strong>{id}</strong>
                    <small>{profileName(route.profile)}</small>
                  </div>
                </div>
              </div>
            </Link>
          );
        })}
      </div>
      <div className="routing-footer">
        <span>动态表示来源新鲜度 · 非实时消息轨迹</span>
        {routes.length > 5 && (
          <button type="button" onClick={() => setExpanded(!expanded)}>
            {expanded ? "收起" : `展开全部 ${routes.length} 条`}{" "}
            <ArrowRight size={13} />
          </button>
        )}
      </div>
    </div>
  );
}
