import { useEffect, useState } from "react";
import { ChevronLeft, ChevronRight, RefreshCw, Search } from "lucide-react";
import { api, date, errorText, type NetworkState } from "../api";
import { Badge, Empty } from "../components";
import {
  type AudioPage,
  type AudioDetail,
  type AudioWorker,
  audioStatus,
  workerStatus,
  seconds,
} from "../audio";
import { Modal } from "./Apps";

const pageSize = 25;
export default function Audio({ state }: { state: NetworkState }) {
  const [draft, setDraft] = useState({ node_id: "", status: "", q: "" });
  const [filter, setFilter] = useState(draft);
  const [sort, setSort] = useState("started_at:desc");
  const [offset, setOffset] = useState(0);
  const [page, setPage] = useState<AudioPage | null>(null);
  const [workers, setWorkers] = useState<AudioWorker[] | null>(null);
  const [error, setError] = useState("");
  const [workerError, setWorkerError] = useState("");
  const [busy, setBusy] = useState(true);
  const [refresh, setRefresh] = useState(0);
  const [selected, setSelected] = useState<string | null>(null);
  useEffect(() => {
    const controller = new AbortController();
    let timer: ReturnType<typeof setTimeout>;
    setPage(null);
    async function load() {
      setBusy(true);
      const [sortBy, order] = sort.split(":");
      const query = new URLSearchParams({
        ...filter,
        sort_by: sortBy,
        order,
        limit: String(pageSize),
        offset: String(offset),
      });
      const [records, agents] = await Promise.allSettled([
        api<AudioPage>(`/audio/segments?${query}`, {
          signal: controller.signal,
        }),
        api<{ items: AudioWorker[] }>("/audio/workers", {
          signal: controller.signal,
        }),
      ]);
      if (controller.signal.aborted) return;
      if (records.status === "fulfilled") {
        setPage(records.value);
        setError("");
      } else setError(errorText(records.reason));
      if (agents.status === "fulfilled") {
        setWorkers(agents.value.items);
        setWorkerError("");
      } else setWorkerError(errorText(agents.reason));
      setBusy(false);
      timer = setTimeout(load, 5000);
    }
    void load();
    return () => {
      controller.abort();
      clearTimeout(timer);
    };
  }, [filter, sort, offset, refresh]);
  const label = (id: string) => state.node_labels?.[id] || id;
  return (
    <>
      <div className="page-head">
        <h1>音频记录</h1>
        <button
          className="button secondary"
          disabled={busy}
          onClick={() => setRefresh((v) => v + 1)}
        >
          <RefreshCw size={16} />
          刷新
        </button>
      </div>
      {workerError && (
        <div className="banner error" role="alert">
          识别服务状态读取失败：{workerError}
        </div>
      )}
      {workers && workers.length > 0 && (
        <section className="panel audio-workers" aria-label="语音识别服务">
          {workers.map((worker) => (
            <div className="audio-worker" key={worker.agent_id}>
              <div>
                <strong>{worker.host_label || worker.agent_id}</strong>
                <span className="audio-worker-model">
                  {worker.model} · {worker.device === "cuda" ? "GPU" : "CPU"}
                </span>
              </div>
              <div>
                <Badge
                  tone={
                    worker.online && worker.status === "ready"
                      ? "good"
                      : worker.status === "error"
                        ? "warn"
                        : "neutral"
                  }
                >
                  {workerStatus(worker)}
                </Badge>
                <span className="audio-worker-time">
                  {date(worker.received_at)}
                </span>
              </div>
            </div>
          ))}
        </section>
      )}
      {workers?.length === 0 && (
        <div className="banner">
          尚未连接语音识别服务。已收到的音频会保留，等待 worker 上线。
        </div>
      )}
      <form
        className="audio-filters"
        onSubmit={(event) => {
          event.preventDefault();
          setOffset(0);
          setFilter({
            ...draft,
            node_id: draft.node_id.trim(),
            q: draft.q.trim(),
          });
        }}
      >
        <input
          aria-label="设备 ID"
          list="audio-device-ids"
          placeholder="全部设备"
          value={draft.node_id}
          onChange={(e) => setDraft({ ...draft, node_id: e.target.value })}
        />
        <datalist id="audio-device-ids">
          {state.nodes.map((node) => (
            <option key={node.nodeId} value={node.nodeId}>
              {label(node.nodeId)}
            </option>
          ))}
        </datalist>
        <select
          aria-label="识别状态"
          value={draft.status}
          onChange={(e) => setDraft({ ...draft, status: e.target.value })}
        >
          <option value="">全部状态</option>
          <option value="pending">等待识别</option>
          <option value="completed">已识别</option>
          <option value="failed">识别失败</option>
        </select>
        <input
          aria-label="搜索转写文字"
          placeholder="搜索转写文字"
          maxLength={200}
          value={draft.q}
          onChange={(e) => setDraft({ ...draft, q: e.target.value })}
        />
        <button className="button secondary" type="submit">
          <Search size={16} />
          搜索
        </button>
        <select
          className="audio-sort"
          aria-label="音频排序"
          value={sort}
          onChange={(event) => {
            setOffset(0);
            setSort(event.target.value);
          }}
        >
          <option value="started_at:desc">录制时间：从新到旧</option>
          <option value="started_at:asc">录制时间：从旧到新</option>
          <option value="duration_ms:desc">音频时长：从长到短</option>
          <option value="duration_ms:asc">音频时长：从短到长</option>
          <option value="speech_ms:desc">人声时长：从长到短</option>
          <option value="speech_ms:asc">人声时长：从短到长</option>
        </select>
      </form>
      {error && (
        <div className="banner error" role="alert">
          音频记录读取失败：{error}
          <button
            className="text-link"
            onClick={() => setRefresh((v) => v + 1)}
          >
            重试
          </button>
        </div>
      )}
      {!page && busy && <p role="status">正在读取音频记录…</p>}
      {page && (
        <section
          className="panel audio-records"
          aria-label="音频记录列表"
          aria-busy={busy}
        >
          {page.items.length === 0 ? (
            <Empty title="暂无音频记录">
              {filter.q || filter.node_id || filter.status
                ? "试试其他筛选条件。"
                : "设备检测到语音并上传后，记录会显示在这里。"}
            </Empty>
          ) : (
            page.items.map((record) => (
              <button
                className="audio-record"
                key={record.segment_id}
                onClick={() => setSelected(record.segment_id)}
              >
                <div className="audio-record-meta">
                  <strong>{label(record.node_id)}</strong>
                  <time dateTime={record.started_at}>
                    {date(record.started_at)}
                  </time>
                  <span>{seconds(record.duration_ms)}</span>
                  {sort.startsWith("speech_ms:") && (
                    <span>人声 {seconds(record.speech_ms)}</span>
                  )}
                  <Badge
                    tone={
                      record.status === "completed"
                        ? "good"
                        : record.status === "failed"
                          ? "warn"
                          : "neutral"
                    }
                  >
                    {audioStatus(record.status)}
                  </Badge>
                </div>
                <p
                  className={
                    record.text ? "audio-transcript" : "audio-transcript muted"
                  }
                >
                  {record.text ||
                    (record.status === "failed"
                      ? "查看失败原因"
                      : record.status === "completed"
                        ? "未识别到文字"
                        : "等待识别服务处理")}
                </p>
              </button>
            ))
          )}
          {page.total > 0 && (
            <div className="audio-pagination">
              <span>
                {offset + 1}–{offset + page.items.length} / {page.total}
              </span>
              <div className="app-actions">
                <button
                  className="button secondary"
                  disabled={offset === 0 || busy}
                  onClick={() => setOffset(Math.max(0, offset - pageSize))}
                >
                  <ChevronLeft size={16} />
                  上一页
                </button>
                <button
                  className="button secondary"
                  disabled={offset + page.items.length >= page.total || busy}
                  onClick={() => setOffset(offset + pageSize)}
                >
                  下一页
                  <ChevronRight size={16} />
                </button>
              </div>
            </div>
          )}
        </section>
      )}
      {selected && (
        <AudioRecordDetail
          id={selected}
          label={label}
          onClose={() => setSelected(null)}
        />
      )}
    </>
  );
}

function AudioRecordDetail({
  id,
  label,
  onClose,
}: {
  id: string;
  label: (id: string) => string;
  onClose: () => void;
}) {
  const [detail, setDetail] = useState<AudioDetail | null>(null);
  const [error, setError] = useState("");
  const [refresh, setRefresh] = useState(0);
  useEffect(() => {
    const controller = new AbortController();
    let timer: ReturnType<typeof setTimeout>;
    async function load() {
      try {
        const value = await api<AudioDetail>(
          `/audio/segments/${encodeURIComponent(id)}`,
          { signal: controller.signal },
        );
        if (controller.signal.aborted) return;
        setDetail(value);
        setError("");
        if (value.job.status === "pending") timer = setTimeout(load, 5000);
      } catch (e) {
        if (!controller.signal.aborted) setError(errorText(e));
      }
    }
    void load();
    return () => {
      controller.abort();
      clearTimeout(timer);
    };
  }, [id, refresh]);
  return (
    <Modal title="音频详情" onClose={onClose}>
      {error && (
        <div className="banner error" role="alert">
          {error}
          <button
            className="text-link"
            onClick={() => setRefresh((v) => v + 1)}
          >
            重试
          </button>
        </div>
      )}
      {!detail && !error && <p role="status">正在读取详情…</p>}
      {detail && (
        <div className="audio-detail">
          <dl>
            <dt>设备</dt>
            <dd>{label(detail.segment.node_id)}</dd>
            <dt>录制时间</dt>
            <dd>
              {new Date(detail.segment.started_at).toLocaleString("zh-CN")}
            </dd>
            <dt>时长</dt>
            <dd>{seconds(detail.segment.duration_ms)}</dd>
            <dt>状态</dt>
            <dd>{audioStatus(detail.job.status)}</dd>
            {detail.result && (
              <>
                <dt>模型</dt>
                <dd>
                  {detail.result.model} ·{" "}
                  {detail.result.device === "cuda" ? "GPU" : "CPU"}
                </dd>
                <dt>语言</dt>
                <dd>{detail.result.language || "未识别"}</dd>
                <dt>识别耗时</dt>
                <dd>{seconds(detail.result.processing_ms)}</dd>
              </>
            )}
          </dl>
          {detail.result?.status === "failed" && (
            <div className="banner error">
              {detail.result.error_message || "识别失败"}
              {detail.result.error_code && (
                <code>{detail.result.error_code}</code>
              )}
            </div>
          )}
          {detail.result?.status === "completed" && (
            <section>
              <h3>转写文本</h3>
              <p className="audio-detail-text">
                {detail.result.text || "未识别到文字"}
              </p>
            </section>
          )}
          {detail.result && (
            <details>
              <summary>结构化结果</summary>
              <pre className="audio-json">
                {JSON.stringify(detail.result, null, 2)}
              </pre>
            </details>
          )}
        </div>
      )}
    </Modal>
  );
}
