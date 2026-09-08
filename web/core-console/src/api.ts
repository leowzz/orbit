export type InputKind = "usage" | "codex";
export interface Route {
  profile: string;
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
export interface Agent {
  id: string;
  usage_fresh: boolean;
  codex_fresh: boolean;
  state: {
    agentId: string;
    hostLabel: string;
    agentVersion: string;
    agentEpoch: string;
    metadata?: { producedAt?: string };
    sources?: Source[];
  };
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
  now: string;
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
    "usage-oled-128x32": "OLED · 用量屏",
    "overview-web": "Web · 工作台",
    "overview-android": "Android · 小组件",
  })[profile] ?? profile;
export const modelName = (model: string) =>
  ({
    "oled-128x32": "OLED 显示屏",
    web: "Web 工作台",
    android: "Android 设备",
  })[model] ?? model;
export function errorText(error: unknown) {
  if (error instanceof APIError && error.status === 409)
    return "规则已被其他会话修改。请先重新载入，再保存你的修改。";
  return error instanceof Error ? error.message : "请求失败，请稍后重试。";
}
