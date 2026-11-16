export type InputKind = "usage" | "codex" | "system";
export type DisplayMode =
  "usage" | "codex" | "sessions" | "system" | "rotating";
export type SystemPrimary = "cpu" | "memory" | "load" | "lav";
export interface DisplayOffSchedule {
  enabled: boolean;
  start: string;
  end: string;
}
export interface Route {
  profile: string;
  sessions_enabled?: boolean;
  nfcd_metrics_left_padding?: number;
  display_mode?: DisplayMode;
  system_primary?: SystemPrimary;
  rotation_seconds?: number;
  display_brightness_percent?: number;
  display_off_on_source_offline?: boolean;
  display_off_schedule?: DisplayOffSchedule;
  inputs: { agent_id: string; observation_type: InputKind }[];
}
export interface RouteDocument {
  revision: number;
  routes: Record<string, Route>;
  publish_pending?: boolean;
}
export interface Source {
  observationType?: string;
  enabled?: boolean;
  health?: string;
  lastSuccessAt?: string;
  errorCode?: string;
}
export interface AgentSystemSummary {
  fresh: boolean;
  cpu_usage_permille: number;
  cpu_cores: number;
  memory_used_bytes: number;
  memory_total_bytes: number;
  swap_used_bytes?: number;
  swap_total_bytes?: number;
  load_1m_milli?: number;
  uptime_seconds?: number;
  observed_at?: string;
  fresh_until?: string;
}
export interface NodePreview {
  display_kind: string;
  primary: string;
  secondary: string;
  footer: string;
  freshness: string;
  updated_at?: string;
  rotation_step?: number;
  rotation_total?: number;
  brightness_percent?: number;
  brightness_off_reason?: "manual" | "schedule" | "offline";
}
export interface Agent {
  id: string;
  usage_fresh: boolean;
  codex_fresh: boolean;
  system_fresh?: boolean;
  system?: AgentSystemSummary;
  state: {
    agentId: string;
    hostLabel: string;
    agentVersion: string;
    agentEpoch: string;
    metadata?: { producedAt?: string };
    sources?: Source[];
  };
}
export function agentSource(agent: Agent, kind: InputKind): Source | undefined {
  const observationType =
    kind === "usage"
      ? "OBSERVATION_TYPE_USAGE"
      : kind === "codex"
        ? "OBSERVATION_TYPE_CODEX"
        : "OBSERVATION_TYPE_SYSTEM";
  return agent.state.sources?.find(
    (source) => source.observationType === observationType,
  );
}
export function agentFresh(agent: Agent, kind: InputKind): boolean {
  if (kind === "usage") return agent.usage_fresh;
  if (kind === "codex") return agent.codex_fresh;
  return Boolean(agent.system_fresh);
}
export function agentAnyFresh(agent: Agent): boolean {
  return agent.usage_fresh || agent.codex_fresh || Boolean(agent.system_fresh);
}
export function formatBytesGiB(bytes: number): string {
  return `${(bytes / 1024 ** 3).toFixed(1)} GiB`;
}
export interface Node {
  nodeId: string;
  modelId: string;
  seriesId: string;
  variantId: string;
  firmwareVersion: string;
  nodeEpoch: string;
  metadata?: { producedAt?: string };
}
export interface NetworkState {
  core_id: string;
  core_epoch: string;
  agents: Agent[];
  nodes: Node[];
  node_labels?: Record<string, string>;
  archived_nodes?: string[];
  node_views?: Record<string, NodePreview>;
  now: string;
}
export function visibleDeviceRoutes(
  document: RouteDocument,
  state: NetworkState,
): RouteDocument {
  const archived = new Set(state.archived_nodes ?? []);
  return {
    ...document,
    routes: Object.fromEntries(
      Object.entries(document.routes).filter(([id]) => !archived.has(id)),
    ),
  };
}
export interface AppDevice {
  id: string;
  label: string;
  platform: string;
  revoked: boolean;
  activity: { connections: number; last_seen?: string; last_sync?: string };
}
export function appConnectionLabel(
  activity: AppDevice["activity"],
  now = Date.now(),
): string {
  if (activity.connections > 0) return "已连接";
  const age = now - Date.parse(activity.last_seen ?? "");
  return age >= -5000 && age <= 30000 ? "最近活跃" : "未连接";
}
export interface SystemInfo {
  started_at: string;
  policies: Record<string, { max_ttl: string; max_future_skew: string }>;
  session_hours: number;
}
export class APIError extends Error {
  status: number;
  constructor(status: number, text: string) {
    super(text);
    this.status = status;
  }
}
export async function api<T>(path: string, init?: RequestInit): Promise<T> {
  const response = await fetch("/api" + path, {
    ...init,
    credentials: "same-origin",
    headers: {
      ...(init?.body ? { "Content-Type": "application/json" } : {}),
      ...init?.headers,
    },
  });
  if (!response.ok) {
    const text = (await response.text()).trim();
    if (
      response.status === 401 &&
      path !== "/auth/login" &&
      path !== "/auth/session"
    )
      window.dispatchEvent(new Event("orbit:unauthorized"));
    throw new APIError(response.status, text);
  }
  return response.json() as Promise<T>;
}
export const date = (value?: string) =>
  value
    ? new Date(value).toLocaleString("zh-CN", {
        month: "2-digit",
        day: "2-digit",
        hour: "2-digit",
        minute: "2-digit",
      })
    : "暂无记录";
export const profileName = (profile: string) =>
  ({
    "usage-oled-128x32": "OLED 显示屏",
    "codex-weekly-oled-128x32": "OLED 显示屏",
    "rotating-oled-128x32": "OLED 显示屏",
    "overview-web": "Web · 工作台",
    "metrics-xiaozhi-320x240": "小智彩屏",
    "overview-android": "Android · 旧版连接",
    "overview-app": "Orbit App · 统一连接",
  })[profile] ?? profile;
export const modelName = (model: string) =>
  ({
    "oled-128x32": "OLED 显示屏",
    "xiaozhi-320x240": "小智彩屏",
    web: "Web 工作台",
    android: "Android 设备",
  })[model] ?? model;
export function errorText(error: unknown) {
  if (error instanceof APIError && error.status === 409)
    return "规则已被其他会话修改。请先重新载入，再保存你的修改。";
  return error instanceof Error ? error.message : "请求失败，请稍后重试。";
}

export const displayMode = (
  route: Route,
): "usage" | "codex" | "sessions" | "system" | "rotating" | "" =>
  (route.display_mode === "sessions" ? "codex" : route.display_mode) ??
  (
    {
      "usage-oled-128x32": "usage",
      "codex-weekly-oled-128x32": "codex",
      "rotating-oled-128x32": "rotating",
    } as Record<string, Route["display_mode"]>
  )[route.profile] ??
  "";
export const displayModes = [
  { value: "usage", label: "API 用量" },
  { value: "codex", label: "账号周限" },
  { value: "system", label: "系统资源" },
  { value: "rotating", label: "循环显示" },
] as const;

export const supportsSessions = (route: Route) =>
  !route.profile.endsWith("-oled-128x32") &&
  route.profile !== "metrics-xiaozhi-320x240";
export const sessionsEnabled = (route: Route) =>
  supportsSessions(route) &&
  (route.sessions_enabled ??
    ((!route.display_mode || route.display_mode === "sessions") &&
      route.inputs.some((input) => input.observation_type === "codex")));

export interface ShortcutEntry {
  id: string;
  title: string;
  kind: string;
  icon_name?: string;
}
export interface ExecutionEndpoint {
  executor_kind: "agent" | "node";
  executor_id: string;
  host_label: string;
  revision: number;
  items: ShortcutEntry[];
}
export interface ShortcutPairing {
  executor_kind: "agent" | "node";
  executor_id: string;
  enabled: boolean;
  items: string[] | null;
}
export interface NavigationSettings {
  metrics_visible?: boolean;
  metrics_width?: number;
  revision: number;
  show_titles: boolean;
  at_top: boolean;
  nfcd_metrics_left_padding: number;
  pairs: ShortcutPairing[];
  publish_pending?: boolean;
}
