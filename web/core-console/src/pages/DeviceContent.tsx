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
  const kinds: InputKind[] = [];
  if (mode === "usage" || mode === "rotating") kinds.push("usage");
  if (mode === "codex" || mode === "rotating" || sessionsEnabled(route))
    kinds.push("codex");
  if (
    kinds.some(
      (kind) =>
        !route.inputs.some(
          (input) => input.observation_type === kind && input.agent_id,
        ),
    )
  )
    throw new InvalidRoute("请选择显示内容所需的电脑，选好后会自动保存。");
  if (
    mode === "rotating" &&
    (!Number.isInteger(route.rotation_seconds) ||
      route.rotation_seconds! < 1 ||
      route.rotation_seconds! > 3600)
  )
    throw new InvalidRoute("循环间隔需为 1–3600 秒的整数，填写后会自动保存。");
}

function suggestedInputs(state: NetworkState): Route["inputs"] {
  return (["usage", "codex"] as const).flatMap((kind) => {
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
      : inputs.length
        ? "codex"
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
            (input.observation_type === "usage"
              ? mode === "usage" || mode === "rotating"
              : mode === "codex" ||
                mode === "rotating" ||
                sessionsEnabled(next)),
        ),
      ];
    }
    auto.queue.set(next, 350, !baseRef.current.routes[nodeId]);
    setActionError("");
  }
  const mode = displayMode(draft);
  const sessions = sessionsEnabled(draft);
  const oled = profile.endsWith("-oled-128x32");
  const needsUsage =
    mode === "usage" ||
    mode === "rotating" ||
    (mode === "" && draft.inputs.some((i) => i.observation_type === "usage"));
  const needsCodex =
    mode === "codex" ||
    mode === "rotating" ||
    sessions ||
    (mode === "" && draft.inputs.some((i) => i.observation_type === "codex"));
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
  function sourceSelect(kind: InputKind, label: string) {
    const selected = source(kind);
    return (
      <label className="content-source" key={kind}>
        <span>{label}</span>
        <select
          aria-label={label}
          value={selected}
          onChange={(e) => selectSource(kind, e.target.value)}
        >
          <option value="">选择电脑</option>
          {selected && !state.agents.some((a) => a.id === selected) && (
            <option value={selected}>{selected}（暂未发现）</option>
          )}
          {state.agents.map((a) => {
            const status = agentSource(a, kind);
            return (
              <option key={a.id} value={a.id}>
                {a.state.hostLabel || a.id}
                {status
                  ? status.enabled
                    ? ""
                    : kind === "usage"
                      ? "（未开启用量采集）"
                      : "（未开启 Codex 采集）"
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
              <small>两种指标自动轮换</small>
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
        {!oled && (
          <SettingSwitch
            title="显示 Codex 会话"
            description="同步会话详情；打开会话仍需电脑允许执行"
            checked={sessions}
            onChange={(checked) => update({ sessions_enabled: checked })}
          />
        )}
        <div className="content-sources">
          <h3>数据来自</h3>
          {needsUsage && sourceSelect("usage", "API 用量来源")}
          {needsCodex && sourceSelect("codex", "Codex 来源")}
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
