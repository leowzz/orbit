import { useCallback, useEffect, useState } from "react";
import { ArrowDown, ArrowUp, Plus, Trash2 } from "lucide-react";
import { api, APIError } from "../api";
import type {
  ExecutionEndpoint,
  NavigationSettings,
  ShortcutPairing,
} from "../api";
import { Empty } from "../components";

const endpointKey = (p: { executor_kind: string; executor_id: string }) =>
  `${p.executor_kind}/${p.executor_id}`;
function move<T>(items: T[], index: number, delta: number): T[] {
  const next = [...items];
  [next[index], next[index + delta]] = [next[index + delta], next[index]];
  return next;
}
export default function DeviceShortcuts({
  nodeId,
  revoked,
  nfcd,
}: {
  nodeId: string;
  revoked: boolean;
  nfcd: boolean;
}) {
  const [saved, setSaved] = useState<NavigationSettings | null>(null);
  const [draft, setDraft] = useState<NavigationSettings | null>(null);
  const [endpoints, setEndpoints] = useState<ExecutionEndpoint[]>([]);
  const [busy, setBusy] = useState(false),
    [error, setError] = useState(""),
    [message, setMessage] = useState("");
  const load = useCallback(async () => {
    setBusy(true);
    setError("");
    try {
      const [cfg, result] = await Promise.all([
        api<NavigationSettings>(
          `/app/navigation/${encodeURIComponent(nodeId)}`,
        ),
        api<{ executors: ExecutionEndpoint[] }>("/app/executors"),
      ]);
      setSaved(cfg);
      setDraft(cfg);
      setEndpoints(result.executors);
    } catch {
      setError("未能读取快捷操作配置，请重试。");
    } finally {
      setBusy(false);
    }
  }, [nodeId]);
  useEffect(() => {
    void load();
  }, [load]);
  const dirty = JSON.stringify(saved) !== JSON.stringify(draft);
  useEffect(() => {
    const leave = (event: BeforeUnloadEvent) => {
      if (dirty) {
        event.preventDefault();
        event.returnValue = "";
      }
    };
    window.addEventListener("beforeunload", leave);
    return () => window.removeEventListener("beforeunload", leave);
  }, [dirty]);
  function updatePair(index: number, patch: Partial<ShortcutPairing>) {
    if (!draft) return;
    setDraft({
      ...draft,
      pairs: draft.pairs.map((p, i) => (i === index ? { ...p, ...patch } : p)),
    });
  }
  async function save() {
    if (!draft) return;
    setBusy(true);
    setError("");
    setMessage("");
    try {
      const result = await api<NavigationSettings>(
        `/app/navigation/${encodeURIComponent(nodeId)}`,
        { method: "PUT", body: JSON.stringify(draft) },
      );
      const { publish_pending, ...cfg } = result;
      setDraft(cfg);
      setSaved(cfg);
      setMessage(
        publish_pending
          ? "配置已保存，连接恢复后更新设备。"
          : "已保存，设备连接后同步。",
      );
    } catch (e) {
      setError(
        e instanceof APIError && e.status === 409
          ? "配置或入口已在别处更新。修改已保留，请载入最新配置后调整。"
          : "未能保存，请检查连接及执行端状态后重试。",
      );
    } finally {
      setBusy(false);
    }
  }
  if (!draft)
    return (
      <div className="panel device-section">
        {error ? (
          <div className="banner error" role="alert">
            {error}
            <button onClick={() => void load()}>重试</button>
          </div>
        ) : (
          <p role="status">正在加载快捷操作…</p>
        )}
      </div>
    );
  const available = endpoints.filter(
    (e) => !draft.pairs.some((p) => endpointKey(p) === endpointKey(e)),
  );
  return (
    <div className="device-shortcuts">
      <div className="device-section panel">
        <h2>快捷操作来自</h2>
        <p>选择电脑或设备，在这台设备上使用它的快捷入口。</p>
        {draft.pairs.length === 0 && (
          <p className="field-hint">还没有添加来源，从下方选择一个即可。</p>
        )}
        {draft.pairs.map((pair, index) => {
          const endpoint = endpoints.find(
            (e) => endpointKey(e) === endpointKey(pair),
          );
          const entries = endpoint?.items ?? [];
          const order = pair.items ?? entries.map((e) => e.id);
          return (
            <section className="pair-card" key={endpointKey(pair)}>
              <div className="pair-heading">
                <div>
                  <strong>{endpoint?.host_label || pair.executor_id}</strong>
                  <small>
                    {pair.executor_kind === "agent" ? "电脑" : "设备"} ·{" "}
                    {endpoint
                      ? `${entries.length} 个入口`
                      : "暂不可用，可停用或移除"}
                  </small>
                </div>
                <div className="pair-actions">
                  <button
                    className="icon-button"
                    disabled={busy || revoked || index === 0}
                    aria-label={`上移 ${endpoint?.host_label || pair.executor_id}`}
                    onClick={() =>
                      setDraft({
                        ...draft,
                        pairs: move(draft.pairs, index, -1),
                      })
                    }
                  >
                    <ArrowUp size={16} />
                  </button>
                  <button
                    className="icon-button"
                    disabled={
                      busy || revoked || index === draft.pairs.length - 1
                    }
                    aria-label={`下移 ${endpoint?.host_label || pair.executor_id}`}
                    onClick={() =>
                      setDraft({ ...draft, pairs: move(draft.pairs, index, 1) })
                    }
                  >
                    <ArrowDown size={16} />
                  </button>
                  <label className="inline-check">
                    <input
                      type="checkbox"
                      checked={pair.enabled}
                      disabled={busy || revoked || (!endpoint && !pair.enabled)}
                      onChange={(e) =>
                        updatePair(index, { enabled: e.target.checked })
                      }
                    />
                    启用
                  </label>
                  <button
                    className="icon-button"
                    disabled={busy || revoked}
                    aria-label={`移除 ${endpoint?.host_label || pair.executor_id}`}
                    onClick={() =>
                      setDraft({
                        ...draft,
                        pairs: draft.pairs.filter((_, i) => i !== index),
                      })
                    }
                  >
                    <Trash2 size={16} />
                  </button>
                </div>
              </div>
              <details className="pair-customize">
                <summary>
                  {pair.items === null
                    ? "全部入口 · 自定义"
                    : `已选择 ${order.length} 个入口 · 调整`}
                </summary>
                <label className="inline-check">
                  <input
                    type="checkbox"
                    disabled={busy || revoked}
                    checked={pair.items === null}
                    onChange={(e) =>
                      updatePair(index, {
                        items: e.target.checked
                          ? null
                          : entries.map((i) => i.id),
                      })
                    }
                  />
                  跟随来源的全部入口与排序
                </label>
                {pair.items !== null && (
                  <div className="pair-entries">
                    {order.map((id, position) => (
                      <div className="pair-entry" key={id}>
                        <span>
                          {entries.find((e) => e.id === id)?.title ??
                            `${id}（已移除）`}
                        </span>
                        <button
                          className="icon-button"
                          disabled={busy || revoked || position === 0}
                          aria-label="上移入口"
                          onClick={() =>
                            updatePair(index, {
                              items: move(order, position, -1),
                            })
                          }
                        >
                          <ArrowUp size={15} />
                        </button>
                        <button
                          className="icon-button"
                          disabled={
                            busy || revoked || position === order.length - 1
                          }
                          aria-label="下移入口"
                          onClick={() =>
                            updatePair(index, {
                              items: move(order, position, 1),
                            })
                          }
                        >
                          <ArrowDown size={15} />
                        </button>
                        <button
                          className="icon-button"
                          disabled={busy || revoked}
                          aria-label="移除此入口"
                          onClick={() =>
                            updatePair(index, {
                              items: order.filter((i) => i !== id),
                            })
                          }
                        >
                          <Trash2 size={15} />
                        </button>
                      </div>
                    ))}
                    {entries
                      .filter((e) => !order.includes(e.id))
                      .map((entry) => (
                        <button
                          key={entry.id}
                          disabled={busy || revoked}
                          className="button secondary"
                          onClick={() =>
                            updatePair(index, { items: [...order, entry.id] })
                          }
                        >
                          <Plus size={14} />
                          {entry.title}
                        </button>
                      ))}
                    {order.length === 0 && <small>尚未选择入口。</small>}
                  </div>
                )}
              </details>
            </section>
          );
        })}
        {available.length > 0 && (
          <div className="endpoint-options">
            {available.map((endpoint) => (
              <button
                key={endpointKey(endpoint)}
                className="endpoint-option"
                disabled={busy || revoked}
                onClick={() =>
                  setDraft({
                    ...draft,
                    pairs: [
                      ...draft.pairs,
                      {
                        executor_kind: endpoint.executor_kind,
                        executor_id: endpoint.executor_id,
                        enabled: true,
                        items: null,
                      },
                    ],
                  })
                }
              >
                <Plus size={18} />
                <span>
                  <strong>{endpoint.host_label || endpoint.executor_id}</strong>
                  <small>
                    {endpoint.executor_kind === "agent" ? "电脑" : "设备"} ·{" "}
                    {endpoint.items.length} 个入口
                  </small>
                </span>
              </button>
            ))}
          </div>
        )}
        {endpoints.length === 0 && (
          <Empty title="还没有可用快捷来源">
            在电脑 Agent 的「快捷导航」或 App
            的「本机快捷操作」中添加入口，再载入此页。
          </Empty>
        )}
      </div>
      <div className="device-section panel">
        <h2>快捷栏</h2>
        <label className="setting-row">
          <span>
            <strong>显示入口名称</strong>
            <small>在图标旁显示文字</small>
          </span>
          <input
            className="toggle-input"
            role="switch"
            type="checkbox"
            checked={draft.show_titles}
            disabled={busy || revoked}
            onChange={(e) =>
              setDraft({ ...draft, show_titles: e.target.checked })
            }
          />
        </label>
        <label className="setting-row">
          <span>
            <strong>置于顶部</strong>
            <small>在内容上方显示快捷栏</small>
          </span>
          <input
            className="toggle-input"
            role="switch"
            type="checkbox"
            checked={draft.at_top}
            disabled={busy || revoked}
            onChange={(e) => setDraft({ ...draft, at_top: e.target.checked })}
          />
        </label>
        {nfcd && (
          <label className="device-padding">
            NFCD 指标左侧留白 · {draft.nfcd_metrics_left_padding}
            <input
              type="range"
              min={0}
              max={64}
              step={1}
              value={draft.nfcd_metrics_left_padding}
              disabled={busy || revoked}
              onChange={(e) =>
                setDraft({
                  ...draft,
                  nfcd_metrics_left_padding: Number(e.target.value),
                })
              }
            />
          </label>
        )}
      </div>
      {error && (
        <div className="banner error" role="alert">
          {error}
        </div>
      )}
      {message && !dirty && (
        <div className="banner success" role="status">
          {message}
        </div>
      )}
      <div className="device-save">
        <span>{dirty ? "有未保存的修改" : "已保存"}</span>
        {dirty && (
          <button
            className="button"
            disabled={busy}
            onClick={() => {
              setDraft(saved);
              setError("");
            }}
          >
            撤销修改
          </button>
        )}
        <button
          className="button secondary"
          disabled={busy}
          onClick={() => {
            if (!dirty || window.confirm("载入最新配置会放弃当前修改，继续？"))
              void load();
          }}
        >
          载入最新配置
        </button>
        <button
          className="button primary"
          disabled={busy || !dirty || revoked}
          onClick={() => void save()}
        >
          {busy ? "保存中…" : "保存修改"}
        </button>
      </div>
    </div>
  );
}
