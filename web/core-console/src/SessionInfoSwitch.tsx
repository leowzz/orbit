import { useState } from "react";
import {
  api,
  APIError,
  errorText,
  sessionsEnabled,
  supportsSessions,
} from "./api";
import type { RouteDocument } from "./api";

export default function SessionInfoSwitch({
  nodeId,
  document,
  onChange,
}: {
  nodeId: string;
  document: RouteDocument;
  onChange: (document: RouteDocument) => void;
}) {
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");
  const route = document.routes[nodeId];
  if (!route) return null;
  if (!supportsSessions(route))
    return (
      <div className="session-info-switch">
        <small>仅显示指标</small>
      </div>
    );
  const enabled = sessionsEnabled(route);
  async function toggle() {
    setBusy(true);
    setError("");
    try {
      const latest = await api<RouteDocument>("/routes");
      onChange(latest);
      const current = latest.routes[nodeId];
      if (!current || !supportsSessions(current))
        throw new Error("规则已更改，请重新载入。");
      const next = !enabled;
      if (
        next &&
        !current.inputs.some((input) => input.observation_type === "codex")
      )
        throw new Error("请先编辑规则，配置 Codex 数据来源。");
      onChange(
        await api<RouteDocument>("/routes", {
          method: "PUT",
          body: JSON.stringify({
            revision: latest.revision,
            routes: {
              ...latest.routes,
              [nodeId]: { ...current, sessions_enabled: next },
            },
          }),
        }),
      );
    } catch (e) {
      setError(
        e instanceof APIError && e.status === 409
          ? "规则已更新，请重试。"
          : errorText(e),
      );
    } finally {
      setBusy(false);
    }
  }
  return (
    <div className="session-info-switch">
      <button
        type="button"
        role="switch"
        aria-label={nodeId + " Session 信息"}
        aria-checked={enabled}
        disabled={busy}
        onClick={() => void toggle()}
      >
        <span className="switch-track" aria-hidden="true">
          <span />
        </span>
        {enabled ? "已开启" : "已关闭"}
      </button>
      {error && <small role="alert">{error}</small>}
    </div>
  );
}
