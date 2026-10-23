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
}
export const audioStatus = (status: string) =>
  ({ pending: "等待识别", completed: "已识别", failed: "识别失败" })[status] ??
  status;
export const workerStatus = (worker: AudioWorker) =>
  !worker.online
    ? "未连接"
    : ({
        starting: "正在加载",
        ready: "就绪",
        processing: "正在识别",
        error: "发生错误",
        offline: "未连接",
      }[worker.status] ?? worker.status);
export const seconds = (ms: number) => `${(ms / 1000).toFixed(1)} 秒`;
