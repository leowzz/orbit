export type AudioStatus = "pending" | "completed" | "failed";
export interface AudioRecord {
  segment_id: string;
  session_id: string;
  node_id: string;
  sequence: number;
  started_at: string;
  duration_ms: number;
  speech_ms: number;
  status: AudioStatus;
  text: string;
  language: string;
  agent_id: string;
}
export interface AudioPage {
  items: AudioRecord[];
  total: number;
  limit: number;
  offset: number;
  metrics: AudioMetrics;
}
export interface AudioMetrics {
  total: number;
  pending: number;
  completed: number;
  failed: number;
}
export interface AudioResult {
  schema_version: number;
  job_id: string;
  segment_id: string;
  node_id: string;
  agent_id: string;
  status: "completed" | "failed";
  text: string;
  language: string;
  segments: { start_ms: number; end_ms: number; text: string }[];
  model: string;
  device: string;
  processing_ms: number;
  completed_at: string;
  error_code?: string;
  error_message?: string;
}
export interface AudioDetail {
  segment: Omit<AudioRecord, "text" | "language" | "agent_id" | "status">;
  job: {
    job_id: string;
    status: AudioStatus;
    attempts: number;
    last_dispatched_at?: string;
    created_at: string;
  };
  result: AudioResult | null;
}
export interface AudioWorker {
  agent_id: string;
  host_label: string;
  model: string;
  device: string;
  status: string;
  updated_at: string;
  received_at: string;
  online: boolean;
  connection_state?: "online" | "offline" | "stale" | "unconfirmed";
  heartbeat_timeout_seconds?: number;
}
export const audioStatus = (status: string) =>
  ({ pending: "等待识别", completed: "已识别", failed: "识别失败" })[status] ??
  status;
export const workerStatus = (worker: AudioWorker) =>
  !worker.online
    ? ({
        stale: "心跳超时",
        unconfirmed: "等待心跳",
        offline: "已离线",
        online: "状态待更新",
      }[worker.connection_state ?? "offline"] ?? "已离线")
    : ({
        starting: "正在加载模型",
        ready: "在线 · 就绪",
        processing: "正在识别",
        error: "在线 · 服务异常",
        offline: "已离线",
      }[worker.status] ?? worker.status);
export const workerTone = (worker: AudioWorker): "good" | "warn" | "neutral" =>
  !worker.online ? "neutral" : worker.status === "error" ? "warn" : "good";
export const workerDescription = (worker: AudioWorker) => {
  if (!worker.online) {
    if (worker.connection_state === "unconfirmed")
      return "已发现历史注册信息，等待识别主机发送实时心跳。";
    if (worker.connection_state === "stale")
      return `超过 ${worker.heartbeat_timeout_seconds ?? 60} 秒未收到心跳，请检查主机运行状态和网络。`;
    return "识别服务已离线，请在主机上启动服务并连接网络。";
  }
  return worker.status === "processing"
    ? "正在转写音频，完成后结果会自动更新。"
    : worker.status === "error"
      ? "连接正常，识别服务发生异常，请查看主机上的错误提示。"
      : worker.status === "starting"
        ? "模型正在加载，准备完成后会开始识别。"
        : "识别服务在线，等待接收音频任务。";
};
export const audioDevice = (device: string) =>
  ({ cpu: "CPU", cuda: "CUDA", vulkan: "Vulkan", metal: "Metal" })[device] ??
  device;
export const seconds = (ms: number) => `${(ms / 1000).toFixed(1)} 秒`;
