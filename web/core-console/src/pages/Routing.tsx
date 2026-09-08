import { useEffect, useRef, useState } from "react";
import type { FormEvent } from "react";
import { useSearchParams } from "react-router-dom";
import {
  ArrowRight,
  Check,
  Pencil,
  Plus,
  RefreshCw,
  Trash2,
  X,
} from "lucide-react";
import { api, errorText, profileName } from "../api";
import type { NetworkState, Route, RouteDocument } from "../api";
import { Badge, Empty, PageHead } from "../components";
interface Props {
  state: NetworkState;
  document: RouteDocument;
  onChange: (d: RouteDocument) => void;
}
export default function Routing({ state, document, onChange }: Props) {
  const [params, setParams] = useSearchParams(),
    [editing, setEditing] = useState<{ id: string; route?: Route } | null>(
      null,
    ),
    [message, setMessage] = useState(""),
    [error, setError] = useState("");
  const requested = params.get("node");
  useEffect(() => {
    if (requested) {
      setEditing({ id: requested, route: document.routes[requested] });
      setParams({}, { replace: true });
    }
  }, [requested, document.routes, setParams]);
  async function reload() {
    try {
      onChange(await api<RouteDocument>("/routes"));
      setError("");
      setMessage("已载入最新规则。");
    } catch (e) {
      setError(errorText(e));
    }
  }
  const entries = Object.entries(document.routes);
  return (
    <>
      <PageHead
        eyebrow="03 / PROJECTION ROUTES"
        title="转发规则"
        description="为每个 Node 选择视图，并决定 usage、codex 分别来自哪台主机。"
        action={
          <button
            className="button primary"
            onClick={() => setEditing({ id: "" })}
          >
            <Plus size={16} />
            新建规则
          </button>
        }
      />
      {message && (
        <div className="banner success" role="status">
          <Check size={16} />
          {message}
        </div>
      )}
      {error && (
        <div className="banner error" role="alert">
          {error}
        </div>
      )}
      <div className="toolbar">
        <div className="tabs">
          <span className="selected">
            全部规则 <b>{entries.length}</b>
          </span>
        </div>
        <div className="rule-version">
          <span>修订 {document.revision.toString().padStart(3, "0")}</span>
          <button
            className="icon-button"
            aria-label="重新载入规则"
            onClick={reload}
          >
            <RefreshCw size={15} />
          </button>
        </div>
      </div>
      <div className="panel routes-table">
        <div className="route-table-head">
          <span>接收设备 / NODE</span>
          <span>视图配置</span>
          <span>数据来源 / AGENT</span>
          <span />
        </div>
        {entries.map(([id, r]) => (
          <div className="route-table-row" key={id}>
            <div>
              <strong>{id}</strong>
              <small>
                {state.nodes.some((n) => n.nodeId === id)
                  ? "已发现设备"
                  : "预配置 · 等待设备接入"}
              </small>
            </div>
            <div>
              <span className="profile-label">{profileName(r.profile)}</span>
            </div>
            <div className="route-inputs">
              {r.inputs.map((input) => (
                <div key={input.observation_type}>
                  <span className="kind-label">{input.observation_type}</span>
                  <ArrowRight size={12} />
                  <span className="mono">{input.agent_id}</span>
                </div>
              ))}
            </div>
            <button
              className="icon-button edit-rule"
              aria-label={"编辑 " + id}
              onClick={() => setEditing({ id, route: r })}
            >
              <Pencil size={16} />
            </button>
          </div>
        ))}
        {!entries.length && (
          <Empty
            title="从一条规则开始"
            action={
              <button className="button" onClick={() => setEditing({ id: "" })}>
                <Plus size={15} />
                新建规则
              </button>
            }
          >
            一个 Node 对应一条规则。你可以将不同主机的数据组合到同一设备。
          </Empty>
        )}
      </div>
      <div className="routing-note">
        <span className="note-number">i</span>
        <p>
          保存后立即应用。移除来源或删除规则时，Core 会清空设备上的旧数据。
          <br />
          <span>每种数据只能选择一个来源；OLED 用量屏仅支持 usage。</span>
        </p>
        <Badge>本地持久化</Badge>
      </div>
      {editing && (
        <Editor
          key={editing.id}
          editing={editing}
          state={state}
          document={document}
          onClose={() => setEditing(null)}
          onSaved={(d) => {
            onChange(d);
            setEditing(null);
            setError("");
            setMessage(
              d.publish_pending
                ? "规则已保存，设备投递将在连接恢复后重试。"
                : "规则已保存并应用。",
            );
          }}
        />
      )}
    </>
  );
}
function Editor({
  editing,
  state,
  document,
  onClose,
  onSaved,
}: {
  editing: { id: string; route?: Route };
  state: NetworkState;
  document: RouteDocument;
  onClose: () => void;
  onSaved: (d: RouteDocument) => void;
}) {
  const dialog = useRef<HTMLDialogElement>(null);
  const [id, setId] = useState(editing.id),
    [profile, setProfile] = useState(
      editing.route?.profile ??
        {
          web: "overview-web",
          android: "overview-android",
          "oled-128x32": "usage-oled-128x32",
        }[state.nodes.find((n) => n.nodeId === editing.id)?.modelId ?? ""] ??
        "overview-web",
    ),
    [usage, setUsage] = useState(
      editing.route?.inputs.find((i) => i.observation_type === "usage")
        ?.agent_id ?? "",
    ),
    [codex, setCodex] = useState(
      editing.route?.inputs.find((i) => i.observation_type === "codex")
        ?.agent_id ?? "",
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
      if (!/^[a-z0-9][a-z0-9_-]{0,63}$/.test(node)) {
        setError("Node ID 需以小写字母或数字开头，最多 64 位，可包含 - 和 _。");
        return;
      }
      if (!editing.route && Object.hasOwn(document.routes, node)) {
        setError("这个 Node 已有规则，请编辑现有规则。");
        return;
      }
      if (!usage.trim() && (profile === "usage-oled-128x32" || !codex.trim())) {
        setError("请至少选择一个数据来源。");
        return;
      }
    }
    const routes = { ...document.routes };
    if (deleted) delete routes[editing.id];
    else
      routes[node] = {
        profile,
        inputs: [
          ...(usage.trim()
            ? [{ agent_id: usage.trim(), observation_type: "usage" as const }]
            : []),
          ...(codex.trim() && profile !== "usage-oled-128x32"
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
            <div className="eyebrow">PROJECTION ROUTE</div>
            <h2>{editing.route ? "编辑规则" : "新建规则"}</h2>
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
              <label htmlFor="node-id">Node ID</label>
              <input
                id="node-id"
                list="known-nodes"
                autoFocus
                value={id}
                disabled={!!editing.route}
                onChange={(e) => {
                  setId(e.target.value);
                  setDirty(true);
                }}
                placeholder="例如 desk-display"
                required
              />
              <datalist id="known-nodes">
                {state.nodes.map((n) => (
                  <option key={n.nodeId} value={n.nodeId} />
                ))}
              </datalist>
              <p className="field-hint">
                选择已发现设备，或填写新设备的 ID 进行预配置。
              </p>
              <label htmlFor="profile">设备视图</label>
              <select
                id="profile"
                value={profile}
                onChange={(e) => {
                  setProfile(e.target.value);
                  setDirty(true);
                }}
              >
                {["usage-oled-128x32", "overview-web", "overview-android"].map(
                  (p) => (
                    <option key={p} value={p}>
                      {profileName(p)}
                    </option>
                  ),
                )}
              </select>
            </div>
            <div className="form-section">
              <span className="step-label">02 / 数据来源</span>
              <label htmlFor="usage-source">
                <span className="source-glyph">U</span>Usage 用量
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
                <span className="source-glyph">C</span>Codex 会话
              </label>
              <input
                id="codex-source"
                list="known-agents"
                value={codex}
                disabled={profile === "usage-oled-128x32"}
                placeholder={
                  profile === "usage-oled-128x32"
                    ? "此视图不支持 Codex"
                    : "选择或输入 Agent ID"
                }
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
                留空表示不转发这一类数据。两种数据可以来自不同 Agent。
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
                <strong>删除 {editing.id} 的转发规则？</strong>
                <p>设备将停止接收数据，并清空旧内容。</p>
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
                aria-label="删除规则"
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
