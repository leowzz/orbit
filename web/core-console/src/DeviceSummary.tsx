import type { Agent, InputKind, NodePreview, Route } from "./api";
import { displayMode, sessionsEnabled, supportsSessions } from "./api";

export default function DeviceSummary({
  route,
  agents,
  app,
  preview,
}: {
  route?: Route;
  agents: Agent[];
  app: boolean;
  preview?: NodePreview;
}) {
  if (!route)
    return (
      <div className="node-summary node-summary-empty">
        <strong>未设置显示内容</strong>
        <p>
          {app
            ? "收件箱和快捷操作可单独管理。"
            : "选择显示内容与数据来源后，开始同步指标。"}
        </p>
      </div>
    );

  const mode = displayMode(route);
  const sessions = sessionsEnabled(route);
  const hasKind = (kind: InputKind) =>
    route.inputs.some((i) => i.observation_type === kind);
  const usage =
    mode === "usage" ||
    (mode === "rotating" && hasKind("usage")) ||
    (!mode && hasKind("usage"));
  const codex =
    mode === "codex" ||
    (mode === "rotating" && hasKind("codex")) ||
    sessions ||
    (!mode && hasKind("codex"));
  const system =
    mode === "system" ||
    (mode === "rotating" && hasKind("system")) ||
    (!mode && hasKind("system"));
  const kinds: InputKind[] = [
    ...(usage ? (["usage"] as const) : []),
    ...(codex ? (["codex"] as const) : []),
    ...(system ? (["system"] as const) : []),
  ];
  const sources = kinds.map((kind) => ({
    kind,
    ids: [
      ...new Set(
        route.inputs
          .filter((i) => i.observation_type === kind)
          .map((i) => i.agent_id),
      ),
    ],
  }));
  const names = (ids: string[]) =>
    ids
      .map((id) => agents.find((a) => a.id === id)?.state.hostLabel || id)
      .join("、") || "尚未选择";
  const sharedSource =
    sources.length > 0 &&
    sources[0].ids.length > 0 &&
    sources.every(
      (source) =>
        source.ids.length === sources[0].ids.length &&
        source.ids.every((id) => sources[0].ids.includes(id)),
    );
  const label =
    mode === "rotating"
      ? "循环显示"
      : mode === "usage"
        ? "API 用量"
        : mode === "codex"
          ? "账号周限"
          : mode === "system"
            ? "系统资源"
            : usage && codex && system
              ? "综合指标"
              : usage && codex
                ? "用量与周限"
                : system
                  ? "系统资源"
                  : usage
                    ? "API 用量"
                    : "账号周限";

  return (
    <div className="node-summary">
      <div className="node-summary-mode">
        <strong>{label}</strong>
        {mode === "rotating" && (
          <span>每 {route.rotation_seconds || 3} 秒切换</span>
        )}
      </div>
      <dl>
        {sharedSource ? (
          <div>
            <dt>数据来源</dt>
            <dd>{names(sources[0].ids)}</dd>
          </div>
        ) : (
          sources.map((source) => (
            <div key={source.kind}>
              <dt>
                {source.kind === "usage"
                  ? "用量来源"
                  : source.kind === "system"
                    ? "资源来源"
                    : mode === "usage" || mode === "system"
                      ? "会话来源"
                      : "周限来源"}
              </dt>
              <dd>{names(source.ids)}</dd>
            </div>
          ))
        )}
        {preview && (
          <div>
            <dt>当前文本</dt>
            <dd className="mono">
              {[preview.primary, preview.secondary, preview.footer]
                .filter(Boolean)
                .join(" · ")}
            </dd>
          </div>
        )}
        {system && route.system_primary && (
          <div>
            <dt>主指标</dt>
            <dd>
              {route.system_primary === "memory"
                ? "内存优先"
                : route.system_primary === "load" ||
                    route.system_primary === "lav"
                  ? "LAV 优先"
                  : "CPU 优先"}
            </dd>
          </div>
        )}
        {supportsSessions(route) && (
          <div>
            <dt>Codex 会话</dt>
            <dd>{sessions ? "开启" : "关闭"}</dd>
          </div>
        )}
      </dl>
    </div>
  );
}
