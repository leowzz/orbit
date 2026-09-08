import { useLayoutEffect, useRef, useState } from "react";
import { ArrowRight, Cpu, Pause, Play, Server } from "lucide-react";
import { Link } from "react-router-dom";
import type { NetworkState, RouteDocument } from "./api";
import { profileName } from "./api";
import { DeviceIcon, Empty } from "./components";

type Edge = { key: string; path: string };
const sourceKey = (input: { agent_id: string; observation_type: string }) =>
  JSON.stringify([input.agent_id, input.observation_type]);

export default function RouteFlow({
  state,
  document,
}: {
  state: NetworkState;
  document: RouteDocument;
}) {
  const [paused, setPaused] = useState(false);
  const [expanded, setExpanded] = useState(false);
  const [focused, setFocused] = useState<string | null>(null);
  const [edges, setEdges] = useState<Edge[]>([]);
  const canvas = useRef<HTMLDivElement>(null);
  const routes = Object.entries(document.routes);
  const visible = expanded ? routes : routes.slice(0, 5);
  const sources = [
    ...new Map(
      visible.flatMap(([, route]) =>
        route.inputs.map((input) => [sourceKey(input), input] as const),
      ),
    ).entries(),
  ];
  const agents = new Map(state.agents.map((a) => [a.id, a]));
  const nodes = new Map(state.nodes.map((n) => [n.nodeId, n]));
  const isFresh = (input: { agent_id: string; observation_type: string }) =>
    !!agents.get(input.agent_id)?.[
      input.observation_type === "usage" ? "usage_fresh" : "codex_fresh"
    ];
  const layoutKey = JSON.stringify([
    sources.map(([id]) => id),
    visible.map(([id]) => id),
  ]);

  // Measure actual card edges so curves stay connected when labels wrap or the viewport changes.
  useLayoutEffect(() => {
    const element = canvas.current;
    if (!element) {
      setEdges([]);
      return;
    }
    const measure = () => {
      const bounds = element.getBoundingClientRect();
      const core = element
        .querySelector<HTMLElement>(".topology-core")!
        .getBoundingClientRect();
      const curve = (x1: number, y1: number, x2: number, y2: number) => {
        const bend = (x2 - x1) * 0.5;
        return `M ${x1} ${y1} C ${x1 + bend} ${y1}, ${x2 - bend} ${y2}, ${x2} ${y2}`;
      };
      setEdges(
        [...element.querySelectorAll<HTMLElement>("[data-flow-anchor]")].map(
          (card) => {
            const rect = card.getBoundingClientRect();
            const incoming = card.dataset.flowSide === "source";
            const cy = core.top + core.height / 2 - bounds.top;
            const y = rect.top + rect.height / 2 - bounds.top;
            return {
              key: card.dataset.flowAnchor!,
              path: incoming
                ? curve(
                    rect.right - bounds.left,
                    y,
                    core.left - bounds.left,
                    cy,
                  )
                : curve(
                    core.right - bounds.left,
                    cy,
                    rect.left - bounds.left,
                    y,
                  ),
            };
          },
        ),
      );
    };
    const observer = new ResizeObserver(measure);
    observer.observe(element);
    element
      .querySelectorAll<HTMLElement>("[data-flow-anchor], .topology-core")
      .forEach((card) => observer.observe(card));
    measure();
    return () => observer.disconnect();
  }, [layoutKey]);

  if (!routes.length)
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

  const selected = focused ? document.routes[focused] : undefined;
  return (
    <div className={`panel routing-map topology ${paused ? "is-paused" : ""}`}>
      <div className="routing-toolbar">
        <div className="routing-legend">
          <span className="usage">
            <i />
            Usage
          </span>
          <span className="codex">
            <i />
            Codex
          </span>
          <span className="output">
            <i />
            设备视图
          </span>
        </div>
        <button
          type="button"
          className="routing-motion"
          aria-pressed={paused}
          onClick={() => setPaused(!paused)}
        >
          {paused ? <Play size={14} /> : <Pause size={14} />}{" "}
          {paused ? "播放动画" : "暂停动画"}
        </button>
      </div>
      <div
        className="topology-scroll"
        role="region"
        aria-label="数据流拓扑，可横向滚动"
        tabIndex={0}
      >
        <div className="topology-head">
          <span>来源 Agent / 数据类型</span>
          <span>汇聚 · 分流</span>
          <span>目标 Node</span>
        </div>
        <div className="topology-canvas" ref={canvas}>
          <svg className="topology-wires" aria-hidden="true">
            {edges.map((edge) => {
              const source = sources.find(
                ([id]) => `source:${id}` === edge.key,
              )?.[1];
              const target = visible.find(
                ([id]) => `target:${id}` === edge.key,
              );
              const fresh = source
                ? isFresh(source)
                : !!target &&
                  !!nodes.get(target[0]) &&
                  !document.publish_pending &&
                  target[1].inputs.some(isFresh);
              const highlighted =
                !selected ||
                (source
                  ? selected.inputs.some(
                      (i) => sourceKey(i) === sourceKey(source),
                    )
                  : target?.[0] === focused);
              const kind = source?.observation_type ?? "output";
              return (
                <g
                  key={edge.key}
                  className={`topology-edge ${kind} ${fresh ? "is-fresh" : ""} ${highlighted ? "is-highlighted" : "is-muted"}`}
                >
                  <path className="signal-track" d={edge.path} />
                  <path
                    className="signal-pulse"
                    d={edge.path}
                    pathLength="100"
                  />
                </g>
              );
            })}
          </svg>
          <div className="topology-sources">
            {sources.map(([id, input]) => {
              const agent = agents.get(input.agent_id);
              return (
                <div
                  className={`topology-card topology-source ${input.observation_type}`}
                  key={id}
                  data-flow-anchor={`source:${id}`}
                  data-flow-side="source"
                >
                  <Server size={18} />
                  <div>
                    <strong>{agent?.state.hostLabel || input.agent_id}</strong>
                    <small>
                      {input.observation_type === "usage" ? "Usage" : "Codex"} ·{" "}
                      {isFresh(input)
                        ? "新鲜"
                        : agent
                          ? "暂无新鲜数据"
                          : "未发现"}
                    </small>
                  </div>
                  <i className="topology-port" />
                </div>
              );
            })}
          </div>
          <div
            className={`topology-core ${sources.some(([, i]) => isFresh(i)) ? "has-fresh" : ""}`}
          >
            <span className="routing-core-ring" />
            <Cpu size={28} />
            <strong>CORE</strong>
            <small>{state.core_id}</small>
          </div>
          <div className="topology-targets">
            {visible.map(([id, route]) => {
              const node = nodes.get(id);
              return (
                <Link
                  className={`topology-card topology-target ${focused === id ? "is-selected" : ""}`}
                  key={id}
                  to="/routes"
                  data-flow-anchor={`target:${id}`}
                  data-flow-side="target"
                  onMouseEnter={() => setFocused(id)}
                  onMouseLeave={() => setFocused(null)}
                  onFocus={() => setFocused(id)}
                  onBlur={() => setFocused(null)}
                >
                  <i className="topology-port" />
                  <DeviceIcon
                    model={
                      node?.modelId ??
                      (route.profile === "overview-web"
                        ? "web"
                        : route.profile === "overview-android"
                          ? "android"
                          : "oled-128x32")
                    }
                  />
                  <div>
                    <strong>{id}</strong>
                    <small>{profileName(route.profile)}</small>
                    <small className="topology-input-label">
                      {route.inputs
                        .map((i) =>
                          i.observation_type === "usage" ? "Usage" : "Codex",
                        )
                        .join(" + ")}
                      {document.publish_pending
                        ? " · 投递待重试"
                        : !node
                          ? " · 未发现设备"
                          : ""}
                    </small>
                  </div>
                </Link>
              );
            })}
          </div>
        </div>
      </div>
      <div className="routing-footer">
        <span>动态表示来源新鲜度 · 聚焦设备查看关联来源</span>
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
