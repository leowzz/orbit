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
}
export interface Settings {
  config: Config;
  mqttPassword: string | null;
  usagePassword: string | null;
  hasMqttPassword: boolean;
  hasUsagePassword: boolean;
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
  clockError: string;
  lastAck: number | null;
}
