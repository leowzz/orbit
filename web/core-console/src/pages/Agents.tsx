import { useState } from "react";
import { Search, Server, ArrowUpRight } from "lucide-react";
import { Link } from "react-router-dom";
import { date, type NetworkState, type RouteDocument } from "../api";
import AudioWorkers, { useAudioWorkers } from "../AudioWorkers";
import { Badge, Empty, PageHead } from "../components";
export default function Agents({
  state,
  document,
}: {
  state: NetworkState;
  document: RouteDocument;
}) {
  const [query, setQuery] = useState("");
  const { workers, error: workerError } = useAudioWorkers();
  const agents = state.agents.filter((a) =>
    (a.id + " " + a.state.hostLabel)
      .toLowerCase()
      .includes(query.toLowerCase()),
  );
  return (
    <>
      <PageHead
        title="电脑与来源"
        description="查看语音识别主机、数据采集来源及最近上报状态。"
      />
      <AudioWorkers workers={workers} error={workerError} query={query} />
      <div className="toolbar">
        <div className="tabs">
          <span className="selected">
            数据采集主机 <b>{state.agents.length}</b>
          </span>
        </div>
        <label className="search">
          <Search size={16} />
          <input
            aria-label="搜索 Agent"
            placeholder="搜索名称或 Agent ID"
            value={query}
            onChange={(e) => setQuery(e.target.value)}
          />
        </label>
      </div>
      <div className="agent-list">
        {agents.map((a) => {
          const targets = Object.entries(document.routes).filter(([, r]) =>
            r.inputs.some((i) => i.agent_id === a.id),
          );
          return (
            <article className="panel agent-card" key={a.id}>
              <div className="agent-heading">
                <span className="device-icon">
                  <Server size={21} strokeWidth={1.4} />
                </span>
                <div>
                  <h2>{a.state.hostLabel}</h2>
                  <span className="mono">{a.id}</span>
                </div>
                <Badge tone={a.usage_fresh || a.codex_fresh ? "good" : "warn"}>
                  {a.usage_fresh || a.codex_fresh ? "有新鲜数据" : "无新鲜数据"}
                </Badge>
              </div>
              <div className="agent-details">
                <div>
                  <span>运行版本</span>
                  <strong>{a.state.agentVersion}</strong>
                </div>
                <div>
                  <span>状态更新时间</span>
                  <strong>{date(a.state.metadata?.producedAt)}</strong>
                </div>
                <div>
                  <span>接收设备</span>
                  <strong>{targets.length} 台设备</strong>
                </div>
              </div>
              <div className="source-table">
                <div className="source-table-head">
                  <span>数据来源</span>
                  <span>健康状态</span>
                  <span>数据新鲜度</span>
                  <span>最近成功</span>
                </div>
                {(a.state.sources ?? []).map((s, i) => {
                  const kind =
                    s.observationType === "OBSERVATION_TYPE_CODEX"
                      ? "codex"
                      : "usage";
                  const healthy = s.health === "SOURCE_HEALTH_HEALTHY";
                  return (
                    <div className="source-table-row" key={i}>
                      <strong>
                        <span className="source-glyph">
                          {kind === "usage" ? "U" : "C"}
                        </span>
                        {kind === "usage" ? "Usage 用量" : "Codex 会话"}
                      </strong>
                      <span>
                        <Badge
                          tone={
                            !s.enabled ? "neutral" : healthy ? "good" : "warn"
                          }
                        >
                          {!s.enabled
                            ? "已禁用"
                            : healthy
                              ? "正常"
                              : s.health === "SOURCE_HEALTH_FAILED"
                                ? "失败"
                                : s.health === "SOURCE_HEALTH_DEGRADED"
                                  ? "降级"
                                  : "未知"}
                        </Badge>
                        {s.errorCode && (
                          <small className="error-code">{s.errorCode}</small>
                        )}
                      </span>
                      <span>
                        {a[`${kind}_fresh`] ? (
                          <Badge tone="good">新鲜</Badge>
                        ) : (
                          <Badge>暂无新鲜数据</Badge>
                        )}
                      </span>
                      <span>{date(s.lastSuccessAt)}</span>
                    </div>
                  );
                })}
              </div>
              <div className="agent-bottom">
                <span>
                  {targets.length
                    ? targets.map(([id]) => id).join(" · ")
                    : "尚未关联接收设备"}
                </span>
                <Link className="text-link" to="/nodes">
                  查看设备配置
                  <ArrowUpRight size={14} />
                </Link>
              </div>
            </article>
          );
        })}
      </div>
      {!agents.length && workers?.length === 0 && (
        <Empty title={query ? "没有匹配的 Agent" : "还没有发现 Agent"}>
          {query
            ? "试试其他名称或 ID。"
            : "启动 Agent 并连接 MQTT 后，主机会自动出现在这里。"}
        </Empty>
      )}
    </>
  );
}
