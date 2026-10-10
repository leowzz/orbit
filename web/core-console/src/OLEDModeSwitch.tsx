import { useState } from "react";
import { APIError, api, errorText } from "./api";
import type { InputKind, RouteDocument } from "./api";

const modes = [
  { profile: "usage-oled-128x32", kind: "usage", label: "API 用量" },
  { profile: "codex-weekly-oled-128x32", kind: "codex", label: "账号周限" },
] satisfies { profile: string; kind: InputKind; label: string }[];

export default function OLEDModeSwitch({
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
  const [message, setMessage] = useState("");
  const route = document.routes[nodeId];
  if (!route || !modes.some((m) => m.profile === route.profile)) return null;

  async function select(mode: (typeof modes)[number]) {
    setBusy(true);
    setError("");
    setMessage("");
    try {
      // Read the latest revision so switching one screen preserves other rules.
      const latest = await api<RouteDocument>("/routes");
      onChange(latest);
      const current = latest.routes[nodeId];
      if (
        !current ||
        !modes.some((m) => m.profile === current.profile) ||
        current.inputs.length !== 1
      ) {
        throw new Error("这台设备的规则已更改，请到转发规则中重新配置。");
      }
      if (current.profile === mode.profile) return;
      const saved = await api<RouteDocument>("/routes", {
        method: "PUT",
        body: JSON.stringify({
          revision: latest.revision,
          routes: {
            ...latest.routes,
            [nodeId]: {
              profile: mode.profile,
              inputs: [
                {
                  agent_id: current.inputs[0].agent_id,
                  observation_type: mode.kind,
                },
              ],
            },
          },
        }),
      });
      onChange(saved);
      setMessage(
        saved.publish_pending ? "已保存，连接恢复后更新屏幕。" : "已切换。",
      );
    } catch (e) {
      setError(
        e instanceof APIError && e.status === 409
          ? "规则刚被其他会话修改，请再次点击重试。"
          : errorText(e),
      );
    } finally {
      setBusy(false);
    }
  }

  return (
    <section className="oled-mode-switch" aria-label={nodeId + " 显示模式"}>
      <div className="oled-mode-buttons" role="group" aria-label="显示模式">
        {modes.map((mode) => (
          <button
            key={mode.profile}
            type="button"
            aria-pressed={route.profile === mode.profile}
            disabled={busy || route.inputs.length !== 1}
            onClick={() => {
              if (route.profile !== mode.profile || error) void select(mode);
            }}
          >
            {mode.label}
          </button>
        ))}
      </div>
      {busy && <p role="status">正在切换…</p>}
      {!busy && message && <p role="status">{message}</p>}
      {error && (
        <p className="oled-mode-error" role="alert">
          {error}
        </p>
      )}
    </section>
  );
}
