import { useEffect, useState } from "react";
import { Link } from "react-router-dom";
import { api, date, errorText } from "./api";
import {
  audioDevice,
  workerStatus,
  workerTone,
  workerDescription,
  type AudioWorker,
} from "./audio";
import { Badge } from "./components";

export function useAudioWorkers() {
  const [workers, setWorkers] = useState<AudioWorker[] | null>(null);
  const [error, setError] = useState("");
  useEffect(() => {
    const controller = new AbortController();
    let timer: ReturnType<typeof setTimeout>;
    async function load() {
      try {
        const result = await api<{ items: AudioWorker[] }>("/audio/workers", {
          signal: controller.signal,
        });
        if (controller.signal.aborted) return;
        setWorkers(result.items);
        setError("");
      } catch (e) {
        if (controller.signal.aborted) return;
        setError(errorText(e));
      }
      timer = setTimeout(load, 5000);
    }
    void load();
    return () => {
      controller.abort();
      clearTimeout(timer);
    };
  }, []);
  return { workers, error };
}

export default function AudioWorkers({
  workers,
  error = "",
  query = "",
}: {
  workers: AudioWorker[] | null;
  error?: string;
  query?: string;
}) {
  const visible = workers?.filter((w) =>
    `${w.host_label} ${w.agent_id}`.toLowerCase().includes(query.toLowerCase()),
  );
  return (
    <section className="panel audio-workers" aria-label="语音识别主机">
      <div className="audio-worker">
        <div>
          <h2>语音识别主机</h2>
          <span className="audio-worker-model">每 5 秒刷新</span>
        </div>
        <Badge
          tone={
            error ? "warn" : workers?.some((w) => w.online) ? "good" : "neutral"
          }
        >
          {error
            ? "状态更新失败"
            : workers
              ? `${workers.filter((w) => w.online).length} / ${workers.length} 在线`
              : "正在读取…"}
        </Badge>
      </div>
      {error && (
        <p className="banner error" role="alert">
          无法更新识别服务状态，以下为上次结果：{error}。正在自动重试。
        </p>
      )}
      {!workers && !error && <p role="status">正在读取识别主机…</p>}
      {visible?.map((worker) => (
        <div className="audio-worker-entry" key={worker.agent_id}>
          <div className="audio-worker">
            <div>
              <strong>{worker.host_label || worker.agent_id}</strong>
              <span className="audio-worker-model">
                {worker.model} · {audioDevice(worker.device)}
              </span>
            </div>
            <div>
              <Badge tone={error ? "neutral" : workerTone(worker)}>
                {error ? "状态待更新" : workerStatus(worker)}
              </Badge>
            </div>
          </div>
          <p className="audio-worker-description">
            {error ? "当前连接状态尚未确认。" : workerDescription(worker)}
          </p>
          <div className="audio-worker-footer">
            <span className="mono">{worker.agent_id}</span>
            <span>最近上报：{date(worker.received_at)}</span>
            <Link
              className="text-link"
              to={`/audio?agent_id=${encodeURIComponent(worker.agent_id)}`}
            >
              查看识别记录
            </Link>
          </div>
        </div>
      ))}
      {workers?.length === 0 && (
        <p className="audio-worker-description">
          尚未发现识别主机。请在桌面 Agent 中启用语音识别并连接 Core。
        </p>
      )}
      {!!workers?.length && visible?.length === 0 && (
        <p className="audio-worker-description">没有匹配的识别主机。</p>
      )}
    </section>
  );
}
