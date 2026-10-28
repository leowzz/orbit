export interface Shortcut {
  id: string;
  title: string;
  kind: "app" | "url";
  target: string;
  icon: string;
}
export interface Config {
  shortcuts: Shortcut[];
  shortcutsRevision: number;
  shortcutsShowTitles: boolean;
  shortcutsAtTop: boolean;
  nfcdMetricsLeftPadding: number;
  agentId: string;
  hostLabel: string;
  allowOpen: boolean;
  ntpServer: string;
  ntpInterval: number;
  ntpTimeout: number;
  mqtt: {
    url: string;
    username: string;
    caFile: string;
    certFile: string;
    keyFile: string;
  };
  codex: {
    enabled: boolean;
    home: string;
    binary: string;
    rateLimits: boolean;
    interval: number;
    ttl: number;
    limit: number;
    includeArchived: boolean;
    includeTitle: boolean;
    includeProject: boolean;
    ignoreCwd: string[];
    ignoreSource: string[];
  };
  usage: {
    enabled: boolean;
    baseUrl: string;
    email: string;
    timezone: string;
    interval: number;
    ttl: number;
    timeout: number;
    loginEndpoint: string;
    refreshEndpoint: string;
    usageEndpoint: string;
  };
  inference: {
    enabled: boolean;
    modelId: string;
    agentId: string;
    username: string;
    modelPath: string;
    runtimePath: string;
    device: "auto" | "cpu" | "gpu";
    queueCapacity: number;
  };
}
export interface Settings {
  config: Config;
  mqttPassword: string | null;
  usagePassword: string | null;
  inferencePassword: string | null;
  hasMqttPassword: boolean;
  hasUsagePassword: boolean;
  hasInferencePassword: boolean;
}
export interface Source {
  lastSuccess: number | null;
  error: string;
  sessions: number | null;
  runningSessions: number | null;
}
export interface Status {
  running: boolean;
  connected: boolean;
  message: string;
  codex: Source;
  usage: Source;
  weeklyError: string;
  weeklyDiagnostics: {
    lastAttempt: number | null;
    lastSuccess: number | null;
    lastFailure: number | null;
    durationMs: number;
    failureDurationMs: number;
    consecutiveFailures: number;
    nextAttempt: number | null;
    lastError: {
      stage: string;
      code: string;
      rpcCode: number | null;
      httpStatus: number | null;
    } | null;
  };
  clockError: string;
  lastAck: number | null;
  inference: InferenceStatus;
}
export interface InferenceStatus {
  status: string;
  connected: boolean;
  device: string;
  queued: number;
  outbox: number;
  error: string;
  modelId: string;
  modelReady: boolean;
  modelDownloading: boolean;
  modelDownloaded: number;
  modelTotal: number;
  modelError: string;
}
export interface InferenceModel {
  id: string;
  name: string;
  filename: string;
  revision: string;
  url: string;
  size: number;
  downloaded: number;
  installed: boolean;
  active: boolean;
  path: string;
  runtimePath: string;
  runtimeInstalled: boolean;
}
