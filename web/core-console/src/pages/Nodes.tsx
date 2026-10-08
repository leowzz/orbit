import { useState } from "react";
import { Link } from "react-router-dom";
import { ArrowUpRight, Search } from "lucide-react";
import type { NetworkState, RouteDocument } from "../api";
import { date, modelName, profileName } from "../api";
import { Badge, DeviceIcon, Empty, PageHead } from "../components";
export default function Nodes({
  state,
  document,
}: {
  state: NetworkState;
  document: RouteDocument;
}) {
  const [query, setQuery] = useState(""),
    [filter, setFilter] = useState("all");
  const nodes = state.nodes.filter(
    (n) =>
      (filter === "all" || n.modelId === filter) &&
      (n.nodeId + " " + n.modelId).toLowerCase().includes(query.toLowerCase()),
  );
  return (
    <>
      <PageHead
        eyebrow="02 / CONNECTED DEVICES"
        title="Nodes"
        description="每一块屏幕，每一个工作台。查看设备身份与当前的数据来源。"
      />
      <div className="toolbar">
        <div className="tabs">
          {[
            ["all", "全部设备"],
            ["oled-128x32", "OLED"],
            ["web", "Web"],
            ["android", "Android"],
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
            aria-label="搜索 Node"
            value={query}
            onChange={(e) => setQuery(e.target.value)}
            placeholder="搜索设备 ID"
          />
        </label>
      </div>
      <div className="node-grid">
        {nodes.map((n) => {
          const route = document.routes[n.nodeId];
          return (
            <article key={n.nodeId} className="panel node-card">
              <div className="node-card-top">
                <DeviceIcon model={n.modelId} />
                <Badge tone={route ? "good" : "neutral"}>
                  {route ? "已配置规则" : "未分配规则"}
                </Badge>
              </div>
              <h2>{n.nodeId}</h2>
              <p>{modelName(n.modelId)}</p>
              <dl>
                <div>
                  <dt>固件版本</dt>
                  <dd>{n.firmwareVersion}</dd>
                </div>
                <div>
                  <dt>硬件变体</dt>
                  <dd>{n.variantId}</dd>
                </div>
                <div>
                  <dt>自描述更新</dt>
                  <dd>{date(n.metadata?.producedAt)}</dd>
                </div>
              </dl>
              <div className="node-route">
                <span className="eyebrow">数据来源</span>
                {route ? (
                  <>
                    <strong>{profileName(route.profile)}</strong>
                    {route.inputs.map((i) => (
                      <div key={i.observation_type}>
                        <span>{i.observation_type}</span>
                        <span className="mono">{i.agent_id}</span>
                      </div>
                    ))}
                  </>
                ) : (
                  <p>为这台设备选择一个数据来源。</p>
                )}
              </div>
              <Link
                className="node-link"
                to={"/routes?node=" + encodeURIComponent(n.nodeId)}
              >
                {route ? "编辑转发规则" : "配置转发规则"}
                <ArrowUpRight size={15} />
              </Link>
            </article>
          );
        })}
      </div>
      {!nodes.length && (
        <Empty title="没有匹配的设备">
          设备发布自描述后会自动出现，也可以先到转发规则中预配置设备。
        </Empty>
      )}
      <p className="footnote">
        设备状态来自 MQTT 自描述；当前协议尚未提供连接
        Presence，已发现不代表设备此刻在线。
      </p>
    </>
  );
}
