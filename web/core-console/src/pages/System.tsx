import { useEffect, useState } from "react";
import { Database, FileSliders, LockKeyhole, Server } from "lucide-react";
import { api, date, errorText } from "../api";
import type { NetworkState, RouteDocument, SystemInfo } from "../api";
import { Badge, PageHead, SectionTitle } from "../components";
export default function System({
  state,
  document,
}: {
  state: NetworkState;
  document: RouteDocument;
}) {
  const [info, setInfo] = useState<SystemInfo | null>(null),
    [error, setError] = useState("");
  useEffect(() => {
    api<SystemInfo>("/system")
      .then(setInfo)
      .catch((e) => setError(errorText(e)));
  }, []);
  return (
    <>
      <PageHead
        eyebrow="04 / UNDER THE HOOD"
        title="系统信息"
        description="运行身份、数据策略与访问方式。让网络的边界清楚可见。"
      />
      {error && (
        <div className="banner error" role="alert">
          {error}
        </div>
      )}
      <div className="system-grid">
        <section className="panel system-card">
          <Server size={21} strokeWidth={1.5} />
          <SectionTitle title="Core 实例" />
          <dl>
            <div>
              <dt>Core ID</dt>
              <dd>{state.core_id}</dd>
            </div>
            <div>
              <dt>本次启动</dt>
              <dd>{date(info?.started_at)}</dd>
            </div>
            <div className="vertical">
              <dt>Core Epoch</dt>
              <dd className="mono">{state.core_epoch}</dd>
            </div>
          </dl>
        </section>
        <section className="panel system-card">
          <Database size={21} strokeWidth={1.5} />
          <SectionTitle title="本地存储" />
          <dl>
            <div>
              <dt>配置数据库</dt>
              <dd>
                SQLite <Badge tone="good">WAL</Badge>
              </dd>
            </div>
            <div>
              <dt>当前路由修订</dt>
              <dd>{document.revision}</dd>
            </div>
            <div>
              <dt>已存储规则</dt>
              <dd>{Object.keys(document.routes).length} 条</dd>
            </div>
          </dl>
          <p>规则保存后立即应用，Core 重启后从本地数据库恢复。</p>
        </section>
        <section className="panel system-card">
          <LockKeyhole size={21} strokeWidth={1.5} />
          <SectionTitle title="访问认证" />
          <dl>
            <div>
              <dt>认证方式</dt>
              <dd>密码 + 会话 Cookie</dd>
            </div>
            <div>
              <dt>会话有效期</dt>
              <dd>{info?.session_hours ?? 24} 小时</dd>
            </div>
            <div>
              <dt>密码来源</dt>
              <dd>Core YAML 配置</dd>
            </div>
          </dl>
          <p>会话使用 HttpOnly Cookie。退出登录或 Core 重启后，原会话失效。</p>
        </section>
        <section className="panel system-card">
          <FileSliders size={21} strokeWidth={1.5} />
          <SectionTitle title="观测策略" />
          <div className="policy-table">
            <div>
              <span>数据类型</span>
              <span>最大有效期</span>
              <span>时钟偏差</span>
            </div>
            {Object.entries(info?.policies ?? {}).map(([key, value]) => (
              <div key={key}>
                <strong>{key}</strong>
                <span className="mono">{value.max_ttl}</span>
                <span className="mono">{value.max_future_skew}</span>
              </div>
            ))}
          </div>
          <p>
            MQTT、日志、NTP 与观测策略继续由原 YAML 管理，路由在控制台配置。
          </p>
        </section>
      </div>
    </>
  );
}
