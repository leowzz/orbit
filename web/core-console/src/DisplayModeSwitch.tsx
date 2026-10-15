import { useEffect, useState } from "react";
import { createPortal } from "react-dom";
import {
  APIError,
  api,
  errorText,
  displayMode,
  displayModes,
  sessionsEnabled,
} from "./api";
import type { RouteDocument } from "./api";

const rotationMode = displayModes.find((mode) => mode.value === "rotating")!;

export default function DisplayModeSwitch({
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
  if (!route) return null;
  const selected = displayMode(route);

  async function select(mode: (typeof displayModes)[number], seconds = 3) {
    setBusy(true);
    setError("");
    setMessage("");
    try {
      // Read the latest revision so switching one screen preserves other rules.
      const latest = await api<RouteDocument>("/routes");
      onChange(latest);
      const current = latest.routes[nodeId];
      if (!current || current.inputs.length < 1 || current.inputs.length > 2) {
        throw new Error("这台设备的规则已更改，请到转发规则中重新配置。");
      }
      if (
        displayMode(current) === mode.value &&
        (mode.value !== "rotating" ||
          (current.rotation_seconds || 3) === seconds)
      ) {
        setMessage("已应用");
        return;
      }
      const required =
        mode.value === "rotating" ? ["usage", "codex"] : [mode.value];
      if (
        required.some(
          (kind) =>
            !current.inputs.some((input) => input.observation_type === kind),
        )
      ) {
        throw new Error("请先编辑规则，配置此展示方式需要的数据来源。");
      }
      const saved = await api<RouteDocument>("/routes", {
        method: "PUT",
        body: JSON.stringify({
          revision: latest.revision,
          routes: {
            ...latest.routes,
            [nodeId]: {
              ...current,
              display_mode: mode.value,
              sessions_enabled: sessionsEnabled(current),
              rotation_seconds: mode.value === "rotating" ? seconds : undefined,
            },
          },
        }),
      });
      onChange(saved);
      setMessage(
        saved.publish_pending ? "已保存，连接恢复后更新设备。" : "已应用",
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
      {!selected && <small>综合展示 · 选择下方模式切换</small>}
      <div className="oled-mode-buttons" role="group" aria-label="显示模式">
        {displayModes
          .filter((mode) => mode.value !== "rotating")
          .map((mode) => (
            <button
              key={mode.value}
              type="button"
              aria-pressed={selected === mode.value}
              disabled={busy}
              onClick={() => {
                if (selected !== mode.value || error) void select(mode);
              }}
            >
              {mode.label}
            </button>
          ))}
        <form
          className="oled-rotation-controls"
          onSubmit={(e) => {
            e.preventDefault();
            if (validInterval && !busy)
              void select(rotationMode, Number(interval));
          }}
        >
          <button
            type="button"
            aria-pressed={selected === rotationMode.value}
            disabled={busy || !validInterval}
            onClick={() => {
              if (validInterval && (selected !== rotationMode.value || error))
                void select(rotationMode, Number(interval));
            }}
          >
            循环显示
          </button>
          {selected === rotationMode.value && (
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
