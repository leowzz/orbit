import { useEffect, useState } from "react";
import { createPortal } from "react-dom";
import { APIError, api, errorText } from "./api";
import type { InputKind, RouteDocument } from "./api";

const modes = [
  { profile: "usage-oled-128x32", kinds: ["usage"], label: "API 用量" },
  { profile: "codex-weekly-oled-128x32", kinds: ["codex"], label: "账号周限" },
  {
    profile: "rotating-oled-128x32",
    kinds: ["usage", "codex"],
    label: "循环显示",
  },
] satisfies { profile: string; kinds: InputKind[]; label: string }[];

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
  const [interval, setInterval] = useState(
    String(route?.rotation_seconds || 3),
  );
  useEffect(() => {
    setInterval(String(route?.rotation_seconds || 3));
  }, [nodeId, route?.rotation_seconds]);
  useEffect(() => {
    if (!message && !error) return;
    const timer = window.setTimeout(
      () => {
        setMessage("");
        setError("");
      },
      error ? 5000 : 2500,
    );
    return () => window.clearTimeout(timer);
  }, [message, error]);
  const validInterval =
    Number.isInteger(Number(interval)) &&
    Number(interval) >= 1 &&
    Number(interval) <= 3600;
  if (!route || !modes.some((m) => m.profile === route.profile)) return null;

  async function select(mode: (typeof modes)[number], seconds = 3) {
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
        current.inputs.length < 1 ||
        current.inputs.length > 2
      ) {
        throw new Error("这台设备的规则已更改，请到转发规则中重新配置。");
      }
      if (
        current.profile === mode.profile &&
        (mode.profile !== "rotating-oled-128x32" ||
          (current.rotation_seconds || 3) === seconds)
      ) {
        setMessage("已应用");
        return;
      }
      const saved = await api<RouteDocument>("/routes", {
        method: "PUT",
        body: JSON.stringify({
          revision: latest.revision,
          routes: {
            ...latest.routes,
            [nodeId]: {
              profile: mode.profile,
              ...(mode.profile === "rotating-oled-128x32"
                ? { rotation_seconds: seconds }
                : {}),
              inputs: mode.kinds.map((kind) => ({
                agent_id:
                  current.inputs.find(
                    (input) => input.observation_type === kind,
                  )?.agent_id || current.inputs[0].agent_id,
                observation_type: kind,
              })),
            },
          },
        }),
      });
      onChange(saved);
      setMessage(
        saved.publish_pending ? "已保存，连接恢复后更新屏幕。" : "已应用",
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
    <section
      className="oled-mode-switch"
      aria-label={nodeId + " 显示模式"}
      aria-busy={busy}
    >
      <div className="oled-mode-buttons" role="group" aria-label="显示模式">
        {modes.slice(0, 2).map((mode) => (
          <button
            key={mode.profile}
            type="button"
            aria-pressed={route.profile === mode.profile}
            disabled={busy}
            onClick={() => {
              if (route.profile !== mode.profile || error) void select(mode);
            }}
          >
            {mode.label}
          </button>
        ))}
        <form
          className="oled-rotation-controls"
          onSubmit={(e) => {
            e.preventDefault();
            if (validInterval && !busy) void select(modes[2], Number(interval));
          }}
        >
          <button
            type="button"
            aria-pressed={route.profile === modes[2].profile}
            disabled={busy || !validInterval}
            onClick={() => {
              if (
                validInterval &&
                (route.profile !== modes[2].profile || error)
              )
                void select(modes[2], Number(interval));
            }}
          >
            循环显示
          </button>
          {route.profile === modes[2].profile && (
            <input
              type="number"
              min={1}
              max={3600}
              step={1}
              aria-label="循环间隔（秒）"
              title="循环间隔（秒），按回车应用"
              value={interval}
              disabled={busy}
              onChange={(e) => setInterval(e.target.value)}
            />
          )}
        </form>
      </div>
      {(message || error) &&
        createPortal(
          <div
            className={"oled-mode-toast" + (error ? " error" : "")}
            role={error ? "alert" : "status"}
          >
            {error || message}
          </div>,
          window.document.body,
        )}
    </section>
  );
}
