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
    shortcutsAtTop: false,
    nfcdMetricsLeftPadding: 16,
    hostLabel: "我的电脑",
    core: { url: "" },
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
    inference: {
      enabled: false,
      modelId: "qwen3-asr-0.6b-q8_0",
      modelPath: "",
      runtimePath: "",
      device: "auto",
      queueCapacity: 1000,
    },
  },
  mqttPassword: null,
  coreToken: null,
  usagePassword: null,
  hasMqttPassword: false,
  hasUsagePassword: false,
  hasCoreToken: false,
};
const status: Status = {
  running: false,
  connected: false,
  message: "请完成连接设置",
  codex: empty,
  usage: empty,
  weeklyError: "",
  weeklyDiagnostics: {
    lastAttempt: null,
    lastSuccess: null,
    lastFailure: null,
    durationMs: 0,
    failureDurationMs: 0,
    consecutiveFailures: 0,
    nextAttempt: null,
    lastError: null,
  },
  clockError: "",
  lastAck: null,
  inference: {
    status: "offline",
    connected: false,
    device: "",
    queued: 0,
    outbox: 0,
    error: "",
    modelId: "qwen3-asr-0.6b-q8_0",
    modelReady: false,
    modelDownloading: false,
    modelDownloaded: 0,
    modelTotal: 0,
    modelError: "",
  },
};
export async function call<T>(
  command: string,
  args?: Record<string, unknown>,
): Promise<T> {
  if (isTauri()) return invoke<T>(command, args);
  if (preview) {
    if (command === "settings_get") return structuredClone(settings) as T;
    if (command === "agent_status") return structuredClone(status) as T;
    if (command === "inference_models")
      return [
        {
          id: "qwen3-asr-0.6b-q8_0",
          name: "Qwen3-ASR 0.6B Q8_0",
          filename: "Qwen3-ASR-0.6B-Q8_0.gguf",
          revision: "ee78f8709547f39bea3b924a16c72403826cac92",
          url: "",
          size: 841502336,
          downloaded: 0,
          installed: false,
          active: false,
          path: "~/Library/Application Support/Orbit/inference/models/Qwen3-ASR-0.6B-Q8_0.gguf",
          runtimePath: "",
          runtimeInstalled: false,
        },
      ] as T;
    if (command === "autostart_get") return false as T;
    if (command === "update_status" || command === "update_check")
      return {
        currentVersion: "0.3.1",
        latestVersion: "0.3.2",
        available: true,
        supported: true,
        phase: "idle",
        downloaded: 0,
        total: 0,
        error: "",
      } as T;
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
            iconKey: "preview-calendar",
            status: "running",
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
            iconKey: "preview-notes",
            status: "recent",
          },
        ],
      } as T;
    throw new Error("界面预览中不可执行此操作");
  }
  throw new Error("请在 Orbit Agent 桌面程序中打开设置");
}
