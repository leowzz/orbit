import { useEffect, useState } from "react";
import { Link, useSearchParams } from "react-router-dom";
import AudioWorkers from "../AudioWorkers";
import { ChevronLeft, ChevronRight, RefreshCw, Search } from "lucide-react";
import {
  api,
  date,
  errorText,
  type AppDevice,
  type NetworkState,
} from "../api";
import { Badge, Empty } from "../components";
import {
  type AudioPage,
  type AudioDetail,
  type AudioWorker,
  audioStatus,
  audioDevice,
  seconds,
} from "../audio";
import { Modal } from "./Apps";

const pageSize = 25;
export default function Audio({ state }: { state: NetworkState }) {
  const [params, setParams] = useSearchParams();
  const nodeFilter = params.get("node_id") ?? "";
  const agentFilter = params.get("agent_id") ?? "";
  const statusFilter = params.get("status") ?? "";
  const textFilter = params.get("q") ?? "";
  const [draft, setDraft] = useState({
    node_id: nodeFilter,
    agent_id: agentFilter,
    status: statusFilter,
    q: textFilter,
  });
  useEffect(() => {
    setDraft({
      node_id: nodeFilter,
      agent_id: agentFilter,
      status: statusFilter,
      q: textFilter,
    });
    setOffset(0);
  }, [nodeFilter, agentFilter, statusFilter, textFilter]);
  const [devices, setDevices] = useState<AppDevice[]>([]);
  useEffect(() => {
    const controller = new AbortController();
    void api<{ devices?: AppDevice[] }>("/app/devices", {
      signal: controller.signal,
    })
      .then((result) => {
        if (!controller.signal.aborted) setDevices(result.devices ?? []);
      })
      .catch(() => {
        /* Device names are optional; IDs remain usable. */
      });
    return () => controller.abort();
  }, []);
  const [hideEmpty, setHideEmpty] = useState(true);
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
        node_id: nodeFilter,
        agent_id: agentFilter,
        status: statusFilter,
        q: textFilter,
        hide_empty: String(hideEmpty),
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
        if (offset > 0 && offset >= records.value.total) {
          setOffset(
            Math.max(
              0,
              Math.floor((records.value.total - 1) / pageSize) * pageSize,
            ),
          );
        }
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
  }, [
    nodeFilter,
    agentFilter,
    statusFilter,
    textFilter,
    hideEmpty,
    sort,
    offset,
    refresh,
  ]);
  const label = (id: string) =>
    state.node_labels?.[id] || devices.find((d) => d.id === id)?.label || id;
  const deviceIDs = [
    ...new Set([
      ...state.nodes.map((n) => n.nodeId),
      ...devices.map((d) => d.id),
    ]),
  ];
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
      {page && (
        <section className="audio-metrics" aria-label="音频处理指标">
          <div className="audio-metric">
            <span>总记录</span>
            <strong>{page.metrics.total}</strong>
          </div>
          <div className="audio-metric pending">
            <span>待处理</span>
            <strong>{page.metrics.pending}</strong>
          </div>
          <div className="audio-metric completed">
            <span>已处理</span>
            <strong>{page.metrics.completed}</strong>
          </div>
          <div className="audio-metric failed">
            <span>处理失败</span>
            <strong>{page.metrics.failed}</strong>
          </div>
        </section>
      )}
      <AudioWorkers workers={workers} error={workerError} />
      {!workerError &&
        workers &&
        !workers.some(
          (w) =>
            w.online && (w.status === "ready" || w.status === "processing"),
        ) &&
        !!page?.metrics.pending && (
          <div className="banner" role="status">
            当前没有就绪的识别主机，{page.metrics.pending}{" "}
            条音频等待处理。主机恢复后会自动继续识别。
          </div>
        )}
      {agentFilter && (
        <p className="app-note">
          仅显示所选主机已返回结果的记录；等待识别的音频尚未归属具体主机。
        </p>
      )}
      <form
        className="audio-filters"
        onSubmit={(event) => {
          event.preventDefault();
          setOffset(0);
          setParams(
            Object.fromEntries(
              Object.entries(draft)
                .map(([key, value]) => [key, value.trim()])
                .filter(([, value]) => value),
            ),
          );
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
          {deviceIDs.map((id) => (
            <option key={id} value={id}>
              {label(id)}
            </option>
          ))}
        </datalist>
        <select
          aria-label="识别主机"
          value={draft.agent_id}
          onChange={(e) => setDraft({ ...draft, agent_id: e.target.value })}
        >
          <option value="">全部识别主机</option>
          {draft.agent_id &&
            !workers?.some((w) => w.agent_id === draft.agent_id) && (
              <option value={draft.agent_id}>{draft.agent_id}</option>
            )}
          {workers?.map((w) => (
            <option key={w.agent_id} value={w.agent_id}>
              {w.host_label || w.agent_id}
            </option>
          ))}
        </select>
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
        <select
          className="audio-visibility"
          aria-label="空识别结果筛选"
          value={String(hideEmpty)}
          onChange={(event) => {
            setOffset(0);
            setHideEmpty(event.target.value === "true");
          }}
        >
          <option value="true">隐藏空转写结果</option>
          <option value="false">显示全部结果</option>
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
      {(nodeFilter || agentFilter || statusFilter || textFilter) && (
        <button
          className="text-link"
          onClick={() => {
            setParams({});
            setOffset(0);
          }}
        >
          清除筛选
        </button>
      )}
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
              {textFilter || nodeFilter || agentFilter || statusFilter
                ? "试试其他筛选条件。"
                : hideEmpty
                  ? "未识别到文字的记录已隐藏，可切换为“显示全部结果”。"
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
                  {record.agent_id && (
                    <span>
                      识别主机：
                      {workers?.find((w) => w.agent_id === record.agent_id)
                        ?.host_label || record.agent_id}
                    </span>
                  )}
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
          workerLabel={(id) =>
            workers?.find((w) => w.agent_id === id)?.host_label || id
          }
          onClose={() => setSelected(null)}
        />
      )}
    </>
  );
}

function AudioRecordDetail({
  id,
  label,
  workerLabel,
  onClose,
}: {
  id: string;
  label: (id: string) => string;
  workerLabel: (id: string) => string;
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
            <dd>
              <Link
                className="text-link"
                to={`/nodes/${encodeURIComponent(detail.segment.node_id)}`}
              >
                {label(detail.segment.node_id)}
              </Link>
            </dd>
            <dt>录制时间</dt>
            <dd>
              {new Date(detail.segment.started_at).toLocaleString("zh-CN")}
            </dd>
            <dt>时长</dt>
            <dd>{seconds(detail.segment.duration_ms)}</dd>
            <dt>人声时长</dt>
            <dd>{seconds(detail.segment.speech_ms)}</dd>
            <dt>任务派发次数</dt>
            <dd>{detail.job.attempts}</dd>
            <dt>最近派发</dt>
            <dd>{date(detail.job.last_dispatched_at)}</dd>
            <dt>状态</dt>
            <dd>{audioStatus(detail.job.status)}</dd>
            {detail.result && (
              <>
                <dt>识别主机</dt>
                <dd>
                  <Link
                    className="text-link"
                    to={`/audio?agent_id=${encodeURIComponent(detail.result.agent_id)}`}
                    onClick={onClose}
                  >
                    {workerLabel(detail.result.agent_id)}
                  </Link>
                </dd>
                <dt>完成时间</dt>
                <dd>{date(detail.result.completed_at)}</dd>
                <dt>模型</dt>
                <dd>
                  {detail.result.model} · {audioDevice(detail.result.device)}
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
