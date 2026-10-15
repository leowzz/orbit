import { invoke, isTauri } from "@tauri-apps/api/core";
import type { Settings, Status } from "./types";
export const preview =
  import.meta.env.DEV && new URLSearchParams(location.search).has("preview");
const empty = {
  lastSuccess: null,
  error: "",
  sessions: null,
  runningSessions: null,
};
const settings: Settings = {
  config: {
    agentId: "agt_preview",
    shortcuts: [],
    shortcutsRevision: 1,
    shortcutsShowTitles: false,
    hostLabel: "我的电脑",
    allowOpen: false,
    ntpServer: "ntp.aliyun.com",
    ntpInterval: 600,
    ntpTimeout: 2,
    mqtt: { url: "", username: "", caFile: "", certFile: "", keyFile: "" },
    codex: {
      enabled: true,
      home: "",
      binary: "",
      rateLimits: true,
      interval: 5,
      ttl: 15,
      limit: 20,
      includeArchived: false,
      includeTitle: false,
      includeProject: false,
      ignoreCwd: [],
      ignoreSource: [],
    },
    usage: {
      enabled: false,
      baseUrl: "",
      email: "",
      timezone: "Asia/Shanghai",
      interval: 30,
      ttl: 120,
      timeout: 10,
      loginEndpoint: "",
      refreshEndpoint: "",
      usageEndpoint: "",
    },
  },
  mqttPassword: null,
  usagePassword: null,
  hasMqttPassword: false,
  hasUsagePassword: false,
};
const status: Status = {
  running: false,
  connected: false,
  message: "请完成连接设置",
  codex: empty,
  usage: empty,
  weeklyError: "",
  clockError: "",
  lastAck: null,
};
export async function call<T>(
  command: string,
  args?: Record<string, unknown>,
): Promise<T> {
  if (isTauri()) return invoke<T>(command, args);
  if (preview) {
    if (command === "settings_get") return structuredClone(settings) as T;
    if (command === "agent_status") return structuredClone(status) as T;
    if (command === "autostart_get") return false as T;
    if (command === "shortcut_recommendations")
      return {
        supported: true,
        items: [
          {
            app: {
              id: "preview-calendar",
              title: "日历",
              kind: "app",
              target: "/System/Applications/Calendar.app",
              icon: "",
            },
            lastUsed: Date.now() / 1000,
          },
          {
            app: {
              id: "preview-notes",
              title: "备忘录",
              kind: "app",
              target: "/System/Applications/Notes.app",
              icon: "",
            },
            lastUsed: Date.now() / 1000,
          },
        ],
      } as T;
    throw new Error("界面预览中不可执行此操作");
  }
  throw new Error("请在 Orbit Agent 桌面程序中打开设置");
}
