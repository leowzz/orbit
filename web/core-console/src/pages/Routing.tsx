import { useEffect, useRef, useState } from "react";
import type { FormEvent } from "react";
import { ArrowRight, Trash2, X } from "lucide-react";
import {
  api,
  errorText,
  profileName,
  displayMode,
  displayModes,
  sessionsEnabled,
  supportsSessions,
} from "../api";
import type { NetworkState, Route, RouteDocument } from "../api";
export type Target = {
  id: string;
  label: string;
  profile: string;
  revoked?: boolean;
};
export function Editor({
  editing,
  state,
  targets,
  document,
  onClose,
  onSaved,
}: {
  editing: { id: string; route?: Route };
  state: NetworkState;
  targets: Target[];
  document: RouteDocument;
  onClose: () => void;
  onSaved: (d: RouteDocument) => void;
}) {
  const dialog = useRef<HTMLDialogElement>(null);
  const [id, setId] = useState(editing.id),
    [profile, setProfile] = useState(
      targets.find((t) => t.id === editing.id)?.profile === "overview-app"
        ? "overview-app"
        : (editing.route?.profile ??
            targets.find((t) => t.id === editing.id)?.profile ??
            "overview-web"),
    ),
    [mode, setMode] = useState<string>(
      editing.route ? displayMode(editing.route) : "usage",
    ),
    [sessions, setSessions] = useState(
      editing.route ? sessionsEnabled(editing.route) : false,
    ),
    [nfcdPadding] = useState<number | undefined>(
      editing.route?.nfcd_metrics_left_padding,
    ),
    [usage, setUsage] = useState(
      editing.route?.inputs.find((i) => i.observation_type === "usage")
        ?.agent_id ?? "",
    ),
    [codex, setCodex] = useState(
      editing.route?.inputs.find((i) => i.observation_type === "codex")
        ?.agent_id ?? "",
    ),
    [rotationSeconds, setRotationSeconds] = useState(
      editing.route?.rotation_seconds || 3,
    ),
    [busy, setBusy] = useState(false),
    [error, setError] = useState(""),
    [dirty, setDirty] = useState(false),
    [discard, setDiscard] = useState(false),
    [remove, setRemove] = useState(false);
  useEffect(() => {
    dialog.current?.showModal();
    const leave = (e: BeforeUnloadEvent) => {
      if (dirty) {
        e.preventDefault();
        e.returnValue = "";
      }
    };
    window.addEventListener("beforeunload", leave);
    return () => window.removeEventListener("beforeunload", leave);
  }, [dirty]);
  function close() {
    if (busy) return;
    if (dirty) setDiscard(true);
    else onClose();
  }
  async function save(e?: FormEvent, deleted = false) {
    e?.preventDefault();
    setError("");
    const node = id.trim();
    if (!deleted) {
      if (
        mode === "rotating" &&
        (!usage.trim() ||
          !codex.trim() ||
          !Number.isInteger(rotationSeconds) ||
          rotationSeconds < 1 ||
          rotationSeconds > 3600)
      ) {
        setError("循环显示需要两种数据来源，间隔为 1–3600 秒。");
        return;
      }
      if (!/^[a-z0-9][a-z0-9_-]{0,63}$/.test(node)) {
        setError("Node ID 需以小写字母或数字开头，最多 64 位，可包含 - 和 _。");
        return;
      }
      if (!editing.route && Object.hasOwn(document.routes, node)) {
        setError("这个 Node 已有规则，请编辑现有规则。");
        return;
      }
      if (
        (mode === "usage" && !usage.trim()) ||
        ((mode === "codex" ||
          (sessions && !profile.endsWith("-oled-128x32"))) &&
          !codex.trim()) ||
        (!usage.trim() && !codex.trim())
      ) {
        setError("请选择当前展示方式需要的数据来源。");
        return;
      }
    }
    const routes = { ...document.routes };
    if (deleted) delete routes[editing.id];
    else
      routes[node] = {
        profile,
        ...(profile === "overview-app" && nfcdPadding !== undefined
          ? { nfcd_metrics_left_padding: nfcdPadding }
          : {}),
        sessions_enabled: sessions && supportsSessions({ profile } as Route),
        ...(mode ? { display_mode: mode as Route["display_mode"] } : {}),
        ...(mode === "rotating" ? { rotation_seconds: rotationSeconds } : {}),
        inputs: [
          ...(usage.trim()
            ? [{ agent_id: usage.trim(), observation_type: "usage" as const }]
            : []),
          ...(codex.trim()
            ? [{ agent_id: codex.trim(), observation_type: "codex" as const }]
            : []),
        ],
      };

    setBusy(true);
    try {
      onSaved(
        await api<RouteDocument>("/routes", {
          method: "PUT",
          body: JSON.stringify({ revision: document.revision, routes }),
        }),
      );
    } catch (e) {
      setError(errorText(e));
      setBusy(false);
    }
  }
  return (
    <dialog
      className="drawer"
      ref={dialog}
      onCancel={(e) => {
        e.preventDefault();
        close();
      }}
    >
      <form onSubmit={save}>
        <header>
          <div>
            <h2>{editing.route ? "设置数据内容" : "配置数据内容"}</h2>
          </div>
          <button
            type="button"
            className="icon-button"
            aria-label="关闭编辑面板"
            onClick={close}
            disabled={busy}
          >
            <X size={20} />
          </button>
        </header>
        <fieldset disabled={busy}>
          <div className="drawer-body">
            <div className="form-section">
              <span className="step-label">01 / 接收端</span>
              <label htmlFor="target-device">接收设备</label>
              <select
                id="target-device"
                autoFocus
                value={targets.some((t) => t.id === id) ? id : ""}
                disabled={!!editing.route}
                onChange={(e) => {
                  setId(e.target.value);
                  setProfile(
                    targets.find((t) => t.id === e.target.value)?.profile ??
                      "overview-web",
                  );
                  setDirty(true);
                }}
              >
                <option value="">手动填写设备 ID</option>
                {targets.map((t) => (
                  <option key={t.id} value={t.id} disabled={t.revoked}>
                    {t.label} · {t.revoked ? "已撤销" : profileName(t.profile)}
                  </option>
                ))}
              </select>
              <label htmlFor="node-id">设备 ID</label>
              <input
                id="node-id"
                value={id}
                disabled={!!editing.route}
                onChange={(e) => {
                  setId(e.target.value);
                  const target = targets.find((t) => t.id === e.target.value);
                  if (target) setProfile(target.profile);
                  setDirty(true);
                }}
                placeholder="例如 desk-display"
                required
              />
              <p className="field-hint">
                选择已发现设备，或填写新设备的 ID 进行预配置。
              </p>
              <label htmlFor="profile">接收方式</label>
              <select
                id="profile"
                value={
                  profile.endsWith("-oled-128x32")
                    ? "usage-oled-128x32"
                    : profile
                }
                disabled={targets.some((t) => t.id === id)}
                onChange={(e) => {
                  setProfile(e.target.value);
                  setDirty(true);
                }}
              >
                {[
                  "usage-oled-128x32",
                  "overview-web",
                  "overview-android",
                  "overview-app",
                ].map((p) => (
                  <option key={p} value={p}>
                    {p === "usage-oled-128x32" ? "OLED 显示屏" : profileName(p)}
                  </option>
                ))}
              </select>
              <label htmlFor="display-mode">展示方式</label>
              <select
                id="display-mode"
                value={mode}
                onChange={(e) => {
                  setMode(e.target.value);
                  setDirty(true);
                }}
              >
                {!mode && <option value="">综合展示（现有配置）</option>}
                {displayModes.map((m) => (
                  <option key={m.value} value={m.value}>
                    {m.label}
                  </option>
                ))}
              </select>
              {!profile.endsWith("-oled-128x32") && (
                <label className="session-info-option">
                  <input
                    type="checkbox"
                    checked={sessions}
                    onChange={(e) => {
                      setSessions(e.target.checked);
                      setDirty(true);
                    }}
                  />
                  向此设备同步会话详情（含打开会话操作）
                </label>
              )}
              {mode === "rotating" && (
                <>
                  <label htmlFor="rotation-seconds">循环间隔（秒）</label>
                  <input
                    id="rotation-seconds"
                    type="number"
                    min={1}
                    max={3600}
                    step={1}
                    value={rotationSeconds}
                    onChange={(e) => {
                      setRotationSeconds(Number(e.target.value));
                      setDirty(true);
                    }}
                    required
                  />
                  <p className="field-hint">
                    在 API 用量与账号周限之间循环，关闭控制台后仍会继续。
                  </p>
                </>
              )}
            </div>
            <div className="form-section">
              <span className="step-label">02 / 数据来源</span>
              <label htmlFor="usage-source">
                <span className="source-glyph">U</span>API 用量
              </label>
              <input
                id="usage-source"
                list="known-agents"
                value={usage}
                placeholder="选择或输入 Agent ID"
                onChange={(e) => {
                  setUsage(e.target.value);
                  setDirty(true);
                }}
              />
              <label htmlFor="codex-source">
                <span className="source-glyph">C</span>
                Codex（周限 / 会话）
              </label>
              <input
                id="codex-source"
                list="known-agents"
                value={codex}
                placeholder="选择或输入 Agent ID"
                onChange={(e) => {
                  setCodex(e.target.value);
                  setDirty(true);
                }}
              />
              <datalist id="known-agents">
                {state.agents.map((a) => (
                  <option key={a.id} value={a.id}>
                    {a.state.hostLabel}
                  </option>
                ))}
              </datalist>
              <p className="field-hint">
                {mode === "rotating"
                  ? "循环显示需要两种数据来源，可来自不同 Agent。"
                  : "两种数据可来自不同主机。配置后保留，展示切换无需重新选择。"}
              </p>
            </div>
            {error && (
              <div className="form-error" role="alert">
                {error}
              </div>
            )}
            {discard && (
              <div className="confirm-box">
                <strong>放弃未保存的修改？</strong>
                <div>
                  <button
                    type="button"
                    className="button"
                    onClick={() => setDiscard(false)}
                  >
                    继续编辑
                  </button>
                  <button
                    type="button"
                    className="button danger"
                    onClick={onClose}
                  >
                    放弃修改
                  </button>
                </div>
              </div>
            )}
            {remove && (
              <div className="confirm-box">
                <strong>移除 {editing.id} 的数据内容？</strong>
                <p>设备将停止接收状态数据。已有收件箱消息会保留。</p>
                <div>
                  <button
                    type="button"
                    className="button"
                    onClick={() => setRemove(false)}
                  >
                    取消
                  </button>
                  <button
                    type="button"
                    className="button danger"
                    onClick={() => save(undefined, true)}
                  >
                    确认删除
                  </button>
                </div>
              </div>
            )}
          </div>
          <footer>
            {editing.route && (
              <button
                type="button"
                className="icon-button danger"
                aria-label="移除数据内容"
                onClick={() => setRemove(true)}
              >
                <Trash2 size={17} />
              </button>
            )}
            <span />
            <button type="button" className="button" onClick={close}>
              取消
            </button>
            <button className="button primary" type="submit">
              {busy ? "保存中…" : "保存并应用"}
              <ArrowRight size={15} />
            </button>
          </footer>
        </fieldset>
      </form>
    </dialog>
  );
}
