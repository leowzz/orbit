import { useCallback, useEffect, useRef, useState } from "react";
import { ArrowDown, ArrowUp, Plus, Trash2 } from "lucide-react";
import { api, APIError } from "../api";
import type {
  ExecutionEndpoint,
  NavigationSettings,
  ShortcutPairing,
} from "../api";
import { Empty } from "../components";
import useAutoSave from "../useAutoSave";
import AutoSaveStatus from "../AutoSaveStatus";
import SettingSwitch, { InlineCheckbox } from "../SettingSwitch";

function navigationEqual(
  a: NavigationSettings | null,
  b: NavigationSettings | null,
) {
  const content = (value: NavigationSettings | null) => {
    if (!value) return null;
    const {
      revision: _revision,
      publish_pending: _pending,
      ...settings
    } = value;
    return settings;
  };
  return JSON.stringify(content(a)) === JSON.stringify(content(b));
}

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
  displayPadding,
}: {
  nodeId: string;
  revoked: boolean;
  displayPadding: boolean;
}) {
  const [endpoints, setEndpoints] = useState<ExecutionEndpoint[]>([]);
  const [busy, setBusy] = useState(false);
  const [loadError, setLoadError] = useState("");
  const [pending, setPending] = useState(false);
  const loadRequest = useRef(0);
  const auto = useAutoSave<NavigationSettings>(
    null,
    async (value, saved) => {
      if (revoked) throw new Error("revoked");
      const result = await api<NavigationSettings>(
        `/app/navigation/${encodeURIComponent(nodeId)}`,
        {
          method: "PUT",
          body: JSON.stringify({
            ...value,
            revision: saved?.revision ?? value.revision,
          }),
        },
      );
      const { publish_pending, ...cfg } = result;
      setPending(!!publish_pending);
      return cfg;
    },
    navigationEqual,
  );
  const draft = auto.value;
  const setDraft = auto.queue.set;
  const conflict = auto.error instanceof APIError && auto.error.status === 409;
  const error =
    loadError ||
    (auto.error
      ? conflict
        ? "配置或入口已在别处更新。修改已保留，请载入最新配置后调整。"
        : "未能自动保存，修改已保留，请检查连接后重试。"
      : "");
  const load = useCallback(async () => {
    const request = ++loadRequest.current;
    setBusy(true);
    setLoadError("");
    try {
      const [cfg, result] = await Promise.all([
        api<NavigationSettings>(
          `/app/navigation/${encodeURIComponent(nodeId)}`,
        ),
        api<{ executors: ExecutionEndpoint[] }>("/app/executors"),
      ]);
      if (request !== loadRequest.current) return;
      const { publish_pending, ...settings } = cfg;
      auto.queue.reset(settings);
      setPending(!!publish_pending);
      setEndpoints(result.executors);
    } catch {
      if (request === loadRequest.current)
        setLoadError("未能读取快捷操作配置，请重试。");
    } finally {
      if (request === loadRequest.current) setBusy(false);
    }
  }, [nodeId, auto.queue]);
  useEffect(() => {
    void load();
    return () => {
      loadRequest.current++;
    };
  }, [load]);
  function updatePair(index: number, patch: Partial<ShortcutPairing>) {
    if (!draft) return;
    setDraft({
      ...draft,
      pairs: draft.pairs.map((pair, i) =>
        i === index ? { ...pair, ...patch } : pair,
      ),
    });
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
                  <InlineCheckbox
                    title="启用"
                    label={`启用 ${endpoint?.host_label || pair.executor_id}`}
                    checked={pair.enabled}
                    disabled={busy || revoked || (!endpoint && !pair.enabled)}
                    onChange={(checked) =>
                      updatePair(index, { enabled: checked })
                    }
                  />
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
                <InlineCheckbox
                  title="跟随来源的全部入口与排序"
                  checked={pair.items === null}
                  disabled={busy || revoked}
                  onChange={(checked) =>
                    updatePair(index, {
                      items: checked ? null : entries.map((item) => item.id),
                    })
                  }
                />
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
        {error ? (
          <div className="banner error" role="alert">
            {error}
            {conflict ? (
              <button
                disabled={auto.saving || busy}
                onClick={() => {
                  if (
                    !auto.dirty ||
                    window.confirm("载入最新配置会放弃尚未保存的修改，继续？")
                  )
                    void load();
                }}
              >
                载入最新配置
              </button>
            ) : (
              <button
                disabled={auto.saving || busy}
                onClick={() =>
                  loadError ? void load() : void auto.queue.retry()
                }
              >
                重试
              </button>
            )}
          </div>
        ) : (
          <AutoSaveStatus
            saving={auto.saving}
            dirty={auto.dirty}
            changed={auto.changed}
            pending={pending}
          />
        )}
      </div>
      <div className="device-layout-options">
        <div className="device-section panel">
          <h2>快捷栏</h2>
          <SettingSwitch
            title="显示入口名称"
            description="在图标旁显示文字"
            checked={draft.show_titles}
            disabled={busy || revoked}
            onChange={(checked) => setDraft({ ...draft, show_titles: checked })}
          />
          <SettingSwitch
            title="置于顶部"
            description="在内容上方显示快捷栏"
            checked={draft.at_top}
            disabled={busy || revoked}
            onChange={(checked) => setDraft({ ...draft, at_top: checked })}
          />
        </div>
        {displayPadding && (
          <div className="device-section panel">
            <h2>工作台布局</h2>
            <SettingSwitch
              title="显示用量区"
              description="关闭后，会话列表使用全部内容区域"
              checked={draft.metrics_visible !== false}
              disabled={busy || revoked}
              onChange={(checked) =>
                setDraft({ ...draft, metrics_visible: checked })
              }
            />
            <div className="device-padding">
              用量区宽度 · {draft.metrics_width ?? 150}
              <input
                type="range"
                aria-label="用量区宽度"
                min={96}
                max={320}
                step={1}
                value={draft.metrics_width ?? 150}
                disabled={busy || revoked || draft.metrics_visible === false}
                onChange={(e) =>
                  setDraft({ ...draft, metrics_width: Number(e.target.value) })
                }
              />
              <small>横屏时生效，会话列数随剩余宽度自动调整。</small>
            </div>
            <div className="device-padding">
              全面屏适配-左侧空隙宽度 · {draft.nfcd_metrics_left_padding}
              <input
                type="range"
                aria-label="全面屏适配左侧空隙宽度"
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
            </div>
          </div>
        )}
      </div>
    </div>
  );
}
