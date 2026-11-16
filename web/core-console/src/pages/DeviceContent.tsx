import { useEffect, useRef, useState } from "react";
import { Link } from "react-router-dom";
import { Check, RefreshCw } from "lucide-react";
import {
  api,
  agentSource,
  APIError,
  displayMode,
  displayModes,
  sessionsEnabled,
} from "../api";
import type { InputKind, NetworkState, Route, RouteDocument } from "../api";
import { Empty } from "../components";
import useAutoSave from "../useAutoSave";
import AutoSaveStatus from "../AutoSaveStatus";
import SettingSwitch from "../SettingSwitch";

class InvalidRoute extends Error {}
function validateRoute(route: Route) {
  const mode = displayMode(route);
  const configured = new Set(
    route.inputs.filter((i) => i.agent_id).map((i) => i.observation_type),
  );
  if (mode === "usage" && !configured.has("usage"))
    throw new InvalidRoute("请选择 API 用量来源电脑，选好后会自动保存。");
  if ((mode === "codex" || sessionsEnabled(route)) && !configured.has("codex"))
    throw new InvalidRoute("请选择 Codex 来源电脑，选好后会自动保存。");
  if (mode === "system" && !configured.has("system"))
    throw new InvalidRoute("请选择系统资源来源电脑，选好后会自动保存。");
  if (mode === "rotating") {
    if (configured.size < 2)
      throw new InvalidRoute("循环显示至少需要选择 2 种数据来源。");
    if (
      !Number.isInteger(route.rotation_seconds) ||
      route.rotation_seconds! < 1 ||
      route.rotation_seconds! > 3600
    )
      throw new InvalidRoute(
        "循环间隔需为 1–3600 秒的整数，填写后会自动保存。",
      );
  }
}

function suggestedInputs(state: NetworkState): Route["inputs"] {
  return (["usage", "codex", "system"] as const).flatMap((kind) => {
    const candidates = state.agents.filter(
      (a) => agentSource(a, kind)?.enabled,
    );
    return candidates.length === 1
      ? [{ agent_id: candidates[0].id, observation_type: kind }]
      : [];
  });
}

function initialRoute(
  route: Route | undefined,
  profile: string,
  state: NetworkState,
): Route {
  if (route)
    return {
      ...route,
      sessions_enabled: sessionsEnabled(route),
      ...(displayMode(route) === "rotating"
        ? { rotation_seconds: route.rotation_seconds || 3 }
        : {}),
    };
  const inputs = suggestedInputs(state);
  return {
    profile,
    display_mode: inputs.some((i) => i.observation_type === "usage")
      ? "usage"
      : inputs.some((i) => i.observation_type === "codex")
        ? "codex"
        : inputs.some((i) => i.observation_type === "system")
          ? "system"
          : "usage",
    sessions_enabled: false,
    inputs,
  };
}

export default function DeviceContent({
  nodeId,
  profile,
  revoked,
  state,
  document,
  onChange,
}: {
  nodeId: string;
  profile: string;
  revoked: boolean;
  state: NetworkState;
  document: RouteDocument;
  onChange: (d: RouteDocument) => void;
}) {
  const [base, setBase] = useState(document);
  const baseRef = useRef(document);
  const [busy, setBusy] = useState(false);
  const [actionError, setActionError] = useState("");
  const [pending, setPending] = useState(false);
  function acceptDocument(next: RouteDocument) {
    baseRef.current = next;
    setBase(next);
    onChange(next);
  }
  const auto = useAutoSave<Route>(
    initialRoute(document.routes[nodeId], profile, state),
    async (value) => {
      if (revoked) throw new InvalidRoute("此设备的访问已撤销，设置未保存。");
      validateRoute(value);
      const current = baseRef.current;
      const result = await api<RouteDocument>("/routes", {
        method: "PUT",
        body: JSON.stringify({
          revision: current.revision,
          routes: { ...current.routes, [nodeId]: value },
        }),
      });
      acceptDocument(result);
      setPending(!!result.publish_pending);
      return initialRoute(result.routes[nodeId], profile, state);
    },
  );
  const draft = auto.value!;
  const route = base.routes[nodeId];
  const conflict = auto.error instanceof APIError && auto.error.status === 409;
  const error =
    actionError ||
    (auto.error
      ? conflict
        ? "配置已在别处更新。你的修改已保留，请载入最新配置后重新调整。"
        : auto.error instanceof InvalidRoute
          ? auto.error.message
          : "未能自动保存，修改已保留，请检查连接后重试。"
      : "");
  useEffect(() => {
    const current = auto.queue.getSnapshot();
    if (current.dirty || current.saving || busy) return;
    baseRef.current = document;
    setBase(document);
    const next = initialRoute(document.routes[nodeId], profile, state);
    if (JSON.stringify(next) !== JSON.stringify(current.value))
      auto.queue.reset(next);
  }, [document, nodeId, profile, state, auto.queue, busy]);
  function update(patch: Partial<Route>) {
    const next = { ...draft, ...patch };
    if (patch.display_mode && patch.display_mode !== "rotating")
      delete next.rotation_seconds;
    if (patch.display_mode || patch.sessions_enabled) {
      const mode = displayMode(next);
      next.inputs = [
        ...next.inputs,
        ...suggestedInputs(state).filter(
          (input) =>
            !next.inputs.some(
              (existing) =>
                existing.observation_type === input.observation_type,
            ) &&
            (mode === "rotating" ||
              input.observation_type === mode ||
              (input.observation_type === "codex" && sessionsEnabled(next))),
        ),
      ];
    }
    auto.queue.set(next, 350, !baseRef.current.routes[nodeId]);
    setActionError("");
  }
  const mode = displayMode(draft);
  const sessions = sessionsEnabled(draft);
  const metricsOnly =
    profile.endsWith("-oled-128x32") || profile === "metrics-xiaozhi-320x240";
  const hasSystemInput = draft.inputs.some(
    (i) => i.observation_type === "system",
  );
  const needsUsage =
    mode === "usage" ||
    mode === "rotating" ||
    (mode === "" && draft.inputs.some((i) => i.observation_type === "usage"));
  const needsCodex =
    mode === "codex" ||
    mode === "rotating" ||
    sessions ||
    (mode === "" && draft.inputs.some((i) => i.observation_type === "codex"));
  const needsSystem =
    mode === "system" || mode === "rotating" || (mode === "" && hasSystemInput);
  const showSystemPrimary =
    mode === "system" || (needsSystem && hasSystemInput);
  const preview = state.node_views?.[nodeId];
  function source(kind: InputKind) {
    return (
      draft.inputs.find((i) => i.observation_type === kind)?.agent_id ?? ""
    );
  }
  function selectSource(kind: InputKind, id: string) {
    update({
      inputs: [
        ...draft.inputs.filter((i) => i.observation_type !== kind),
        ...(id ? [{ agent_id: id, observation_type: kind }] : []),
      ],
    });
  }
  async function stopSync() {
    if (!window.confirm("停止向此设备同步数据内容？收件箱和快捷操作会保留。"))
      return;
    setBusy(true);
    setActionError("");
    try {
      await auto.queue.flush();
      const current = baseRef.current;
      const routes = { ...current.routes };
      delete routes[nodeId];
      const result = await api<RouteDocument>("/routes", {
        method: "PUT",
        body: JSON.stringify({ revision: current.revision, routes }),
      });
      acceptDocument(result);
      auto.queue.reset(initialRoute(undefined, profile, state));
      setPending(false);
    } catch {
      setActionError("未能停止同步，请刷新配置后重试。");
    } finally {
      setBusy(false);
    }
  }
  async function reload() {
    if (
      auto.dirty &&
      !window.confirm("载入最新配置会放弃尚未保存的修改，继续？")
    )
      return;
    setBusy(true);
    try {
      const result = await api<RouteDocument>("/routes");
      acceptDocument(result);
      auto.queue.reset(initialRoute(result.routes[nodeId], profile, state));
      setActionError("");
      setPending(!!result.publish_pending);
    } catch {
      setActionError("未能载入配置，请重试。");
    } finally {
      setBusy(false);
    }
  }
  function sourceSelect(kind: InputKind, label: string, optional = false) {
    const selected = source(kind);
    return (
      <label className="content-source" key={kind}>
        <span>{label}</span>
        <select
          aria-label={label}
          value={selected}
          onChange={(e) => selectSource(kind, e.target.value)}
        >
          <option value="">{optional ? "不参与轮换" : "选择电脑"}</option>
          {selected && !state.agents.some((a) => a.id === selected) && (
            <option value={selected}>{selected}（暂未发现）</option>
          )}
          {state.agents.map((a) => {
            const status = agentSource(a, kind);
            const disabledLabel =
              kind === "usage"
                ? "（未开启用量采集）"
                : kind === "codex"
                  ? "（未开启 Codex 采集）"
                  : "（未开启系统资源采集）";
            return (
              <option key={a.id} value={a.id}>
                {a.state.hostLabel || a.id}
                {status
                  ? status.enabled
                    ? ""
                    : disabledLabel
                  : "（未报告采集状态）"}
              </option>
            );
          })}
        </select>
      </label>
    );
  }
  return (
    <div className="device-content device-section panel">
      <h2>显示内容</h2>
      <fieldset className="settings-fields" disabled={busy || revoked}>
        <div className="content-modes" role="group" aria-label="显示内容">
          {!mode && (
            <button className="content-mode" aria-pressed onClick={() => {}}>
              <strong>综合展示</strong>
            </button>
          )}
          {displayModes.map((item) => (
            <button
              key={item.value}
              className="content-mode"
              aria-pressed={mode === item.value}
              onClick={() =>
                update({
                  display_mode: item.value,
                  ...(item.value === "rotating"
                    ? { rotation_seconds: draft.rotation_seconds || 3 }
                    : {}),
                })
              }
            >
              <strong>
                {item.label}
                {mode === item.value && <Check size={17} />}
              </strong>
            </button>
          ))}
        </div>
        {mode === "rotating" && (
          <div className="setting-row">
            <span>
              <strong>切换间隔</strong>
              <small>至少选择 2 种数据来源，按间隔自动轮换</small>
            </span>
            <span className="interval-input">
              <input
                aria-label="切换间隔（秒）"
                type="number"
                min={1}
                max={3600}
                step={1}
                value={draft.rotation_seconds ?? 3}
                onChange={(e) =>
                  update({ rotation_seconds: Number(e.target.value) })
                }
              />
              秒
            </span>
          </div>
        )}
        {showSystemPrimary && (
          <div className="setting-row">
            <span>
              <strong>系统资源主指标</strong>
              <small>决定小屏左侧大字优先显示 CPU 还是内存占比</small>
            </span>
            <div
              className="content-modes"
              role="group"
              aria-label="系统资源主指标"
            >
              {(
                [
                  { value: "cpu", label: "CPU 优先" },
                  { value: "memory", label: "内存优先" },
                ] as const
              ).map((item) => {
                const active = (draft.system_primary || "cpu") === item.value;
                return (
                  <button
                    key={item.value}
                    type="button"
                    className="content-mode"
                    aria-pressed={active}
                    onClick={() => update({ system_primary: item.value })}
                  >
                    <strong>
                      {item.label}
                      {active && <Check size={15} />}
                    </strong>
                  </button>
                );
              })}
            </div>
          </div>
        )}
        {!metricsOnly && (
          <SettingSwitch
            title="显示 Codex 会话"
            description="同步会话详情；打开会话仍需电脑允许执行"
            checked={sessions}
            onChange={(checked) => update({ sessions_enabled: checked })}
          />
        )}
        <div className="content-sources">
          <h3>数据来自</h3>
          {needsUsage &&
            sourceSelect("usage", "API 用量来源", mode === "rotating")}
          {needsCodex &&
            sourceSelect("codex", "Codex 来源", mode === "rotating")}
          {needsSystem &&
            sourceSelect("system", "系统资源来源", mode === "rotating")}
          {!state.agents.length && !route && (
            <Empty title="还没有可用电脑">
              先连接 Orbit Agent，再选择数据来源。
              <br />
              <Link className="text-link" to="/agents">
                查看电脑与来源
              </Link>
            </Empty>
          )}
        </div>
        {preview && (
          <div className="screen-preview" aria-label="当前屏幕文本预览">
            <div className="screen-preview-head">
              <span>当前渲染输出</span>
              <small>
                {preview.rotation_total && preview.rotation_total > 1
                  ? `第 ${preview.rotation_step}/${preview.rotation_total} 页 · `
                  : ""}
                {preview.display_kind === "system"
                  ? "系统资源"
                  : preview.display_kind === "codex"
                    ? "账号周限"
                    : preview.display_kind === "usage"
                      ? "API 用量"
                      : "默认"}
                {" · "}
                {preview.freshness === "fresh" ? "新鲜" : "已过期/等待"}
              </small>
            </div>
            <div className="screen-preview-slots">
              <strong className="screen-preview-primary">
                {preview.primary || "--"}
              </strong>
              <div className="screen-preview-meta">
                <span>{preview.secondary || "--"}</span>
                <span>{preview.footer || "--"}</span>
              </div>
            </div>
          </div>
        )}
      </fieldset>
      {error ? (
        <div className="banner error" role="alert">
          {error}
          {conflict || actionError ? (
            <button
              disabled={busy || auto.saving}
              onClick={() => void reload()}
            >
              <RefreshCw size={14} />
              载入最新配置
            </button>
          ) : (
            !(auto.error instanceof InvalidRoute) && (
              <button
                disabled={auto.saving}
                onClick={() => void auto.queue.retry()}
              >
                重试
              </button>
            )
          )}
        </div>
      ) : (
        <AutoSaveStatus
          saving={auto.saving}
          dirty={auto.dirty}
          changed={auto.changed}
          pending={pending}
          empty={!route}
        />
      )}
      {route && (
        <details className="device-more">
          <summary>更多操作</summary>
          <button
            className="button danger"
            disabled={busy || revoked}
            onClick={() => void stopSync()}
          >
            停止同步数据内容
          </button>
          <p className="field-hint">收件箱和快捷操作仍可使用。</p>
        </details>
      )}
    </div>
  );
}
