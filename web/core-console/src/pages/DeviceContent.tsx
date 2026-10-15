import { useEffect, useState } from "react";
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
  const [baseline, setBaseline] = useState(() =>
    initialRoute(document.routes[nodeId], profile, state),
  );
  const [draft, setDraft] = useState(baseline);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");
  const [conflict, setConflict] = useState(false);
  const [message, setMessage] = useState("");
  const route = base.routes[nodeId];
  const dirty = JSON.stringify(draft) !== JSON.stringify(baseline);
  useEffect(() => {
    if (!dirty) {
      setBase(document);
      const next = initialRoute(document.routes[nodeId], profile, state);
      setBaseline(next);
      setDraft(next);
    }
  }, [document, nodeId, profile, state, dirty]);
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
    setDraft(next);
    setMessage("");
    setError("");
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
  async function save(remove = false) {
    if (
      remove &&
      !window.confirm("停止向此设备同步数据内容？收件箱和快捷操作会保留。")
    )
      return;
    if (!remove) {
      if (
        (needsUsage && !source("usage")) ||
        (needsCodex && !source("codex"))
      ) {
        setError("请选择显示内容所需的电脑。");
        return;
      }
      if (
        mode === "rotating" &&
        (!Number.isInteger(draft.rotation_seconds) ||
          draft.rotation_seconds! < 1 ||
          draft.rotation_seconds! > 3600)
      ) {
        setError("循环间隔需为 1–3600 秒的整数。");
        return;
      }
    }
    setBusy(true);
    setError("");
    setMessage("");
    setConflict(false);
    const routes = { ...base.routes };
    if (remove) delete routes[nodeId];
    else routes[nodeId] = draft;
    try {
      const result = await api<RouteDocument>("/routes", {
        method: "PUT",
        body: JSON.stringify({ revision: base.revision, routes }),
      });
      setBase(result);
      const next = initialRoute(result.routes[nodeId], profile, state);
      setBaseline(next);
      setDraft(next);
      onChange(result);
      setMessage(
        result.publish_pending
          ? "已保存，连接恢复后同步。"
          : remove
            ? "已停止同步数据内容。"
            : "已保存，设备连接后同步。",
      );
    } catch (e) {
      setConflict(e instanceof APIError && e.status === 409);
      setError(
        e instanceof APIError && e.status === 409
          ? "配置已在别处更新。你的修改已保留，可载入最新配置后重新调整。"
          : "未能保存，请检查连接后重试。",
      );
    } finally {
      setBusy(false);
    }
  }
  async function reload() {
    if (dirty && !window.confirm("载入最新配置会放弃当前修改，继续？")) return;
    setBusy(true);
    try {
      const result = await api<RouteDocument>("/routes");
      setBase(result);
      const next = initialRoute(result.routes[nodeId], profile, state);
      setBaseline(next);
      setDraft(next);
      onChange(result);
      setError("");
      setConflict(false);
      setMessage("");
    } catch {
      setError("未能载入配置，请重试。");
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
    <div className="device-content">
      <section className="device-section panel">
        <h2>显示内容</h2>
        <fieldset className="settings-fields" disabled={busy || revoked}>
          <div className="content-modes" role="group" aria-label="显示内容">
            {!mode && (
              <button className="content-mode" aria-pressed onClick={() => {}}>
                <strong>综合展示</strong>
                <span>沿用当前内容</span>
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
                <span>
                  {
                    {
                      usage: "查看 API 消耗",
                      codex: "查看 Codex 额度",
                      rotating: "两种指标轮流显示",
                    }[item.value]
                  }
                </span>
              </button>
            ))}
          </div>
          {mode === "rotating" && (
            <label className="setting-row">
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
            </label>
          )}
          {!oled && (
            <label className="setting-row">
              <span>
                <strong>显示 Codex 会话</strong>
                <small>同步会话详情；打开会话仍需电脑允许执行</small>
              </span>
              <input
                className="toggle-input"
                role="switch"
                type="checkbox"
                checked={sessions}
                onChange={(e) => update({ sessions_enabled: e.target.checked })}
              />
            </label>
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
      </section>
      {error && (
        <div className="banner error" role="alert">
          {error}
          {conflict && (
            <button disabled={busy} onClick={() => void reload()}>
              <RefreshCw size={14} />
              载入最新配置
            </button>
          )}
        </div>
      )}
      {message && (
        <div className="banner success" role="status">
          {message}
        </div>
      )}
      <div className="device-save">
        <span>
          {dirty
            ? "有未保存的修改"
            : route
              ? "已保存"
              : "选择内容和来源后开始同步"}
        </span>
        {dirty && (
          <button
            className="button"
            disabled={busy}
            onClick={() => {
              setDraft(baseline);
              setError("");
              setConflict(false);
            }}
          >
            撤销修改
          </button>
        )}
        <button
          className="button primary"
          disabled={busy || revoked || (!!route && !dirty)}
          onClick={() => void save()}
        >
          {busy ? "保存中…" : route ? "保存修改" : "开始同步"}
        </button>
      </div>
      {route && (
        <details className="device-more">
          <summary>更多操作</summary>
          <button
            className="button danger"
            disabled={busy || revoked}
            onClick={() => void save(true)}
          >
            停止同步数据内容
          </button>
          <p className="field-hint">收件箱和快捷操作仍可使用。</p>
        </details>
      )}
    </div>
  );
}
