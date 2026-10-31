import { useEffect, useRef, useState } from "react";
import { useSearchParams } from "react-router-dom";
import {
  CalendarDays,
  ChevronLeft,
  ChevronRight,
  RefreshCw,
} from "lucide-react";
import { api, errorText } from "../api";
import { Badge, PageHead } from "../components";
import { Modal } from "./Apps";
import "../reviews.css";
import {
  type Review,
  type ReviewPage,
  reviewCellKey,
  reviewHasCard,
  reviewHasContent,
  reviewKind,
  reviewSource,
  reviewItemStatus,
  reviewStatus,
  reviewWeekStart,
  shanghaiDate,
  shiftReviewDate,
  validReviewDate,
} from "../reviews";

const hours = Array.from({ length: 24 }, (_, i) => i);
const timeLabel = (value: string) =>
  new Date(value).toLocaleTimeString("zh-CN", {
    timeZone: "Asia/Shanghai",
    hour: "2-digit",
    minute: "2-digit",
  });
const dateLabel = (value: string) =>
  new Date(value + "T00:00:00Z").toLocaleDateString("zh-CN", {
    timeZone: "UTC",
    month: "long",
    day: "numeric",
  });
const weekday = (value: string) =>
  new Date(value + "T00:00:00Z").toLocaleDateString("zh-CN", {
    timeZone: "UTC",
    weekday: "short",
  });

export default function Reviews() {
  const [params, setParams] = useSearchParams();
  const today = shanghaiDate();
  const rawDate = params.get("date");
  const selectedDate = validReviewDate(rawDate) ? rawDate : today;
  const view = params.get("view") === "day" ? "day" : "week";
  const node = params.get("node_id") ?? "";
  const start = view === "week" ? reviewWeekStart(selectedDate) : selectedDate;
  const days = Array.from({ length: view === "week" ? 7 : 1 }, (_, i) =>
    shiftReviewDate(start, i),
  );
  const query = new URLSearchParams({
    date: start,
    view,
    node_id: node,
  }).toString();
  const [loaded, setLoaded] = useState<{
    query: string;
    page: ReviewPage;
  } | null>(null);
  const page = loaded?.query === query ? loaded.page : null;
  const [error, setError] = useState("");
  const [busy, setBusy] = useState(false);
  const [refresh, setRefresh] = useState(0);
  const [selected, setSelected] = useState<number | null>(null);
  const scroller = useRef<HTMLDivElement>(null);
  const scrolled = useRef("");

  useEffect(() => {
    const controller = new AbortController();
    let timer: ReturnType<typeof setTimeout>;
    setBusy(true);
    setError("");
    async function load() {
      try {
        const result = await api<ReviewPage>(`/reviews?${query}`, {
          signal: controller.signal,
        });
        if (!controller.signal.aborted) {
          setLoaded({ query, page: result });
          setError("");
        }
      } catch (e) {
        if (!controller.signal.aborted) setError(errorText(e));
      } finally {
        if (!controller.signal.aborted) {
          setBusy(false);
          timer = setTimeout(load, 15000);
        }
      }
    }
    void load();
    return () => {
      controller.abort();
      clearTimeout(timer);
    };
  }, [query, refresh]);

  useEffect(() => {
    if (!page || !scroller.current || scrolled.current === query) return;
    const recordedHours = page.items
      .filter((item) => item.kind === "hour")
      .map((item) => Number(reviewCellKey(item.start).slice(11)));
    const first = recordedHours.length ? Math.min(...recordedHours) : 8;
    const row = scroller.current.querySelector<HTMLElement>(
      ".review-calendar-row",
    );
    scroller.current.scrollTop =
      Math.max(0, first - 1) * (row?.offsetHeight ?? 88);
    scrolled.current = query;
  }, [page, query]);

  function navigate(date: string, nextView = view, nextNode = node) {
    setSelected(null);
    setParams({
      date,
      view: nextView,
      ...(nextNode ? { node_id: nextNode } : {}),
    });
  }
  const label = (id: string) =>
    page?.devices.find((device) => device.id === id)?.label || id;
  const buckets = new Map<string, Review[]>();
  for (const item of page?.items ?? []) {
    const key = item.kind === "day" ? item.start : reviewCellKey(item.start);
    buckets.set(key, [...(buckets.get(key) ?? []), item]);
  }
  const selectedVersion = page?.items.find((item) => item.id === selected);
  return (
    <>
      <PageHead
        title="每日回顾"
        description="按时间回看录音里的话题、进展与想法。所有时间均为北京时间。"
        action={
          <button
            className="button secondary"
            disabled={busy}
            onClick={() => setRefresh((v) => v + 1)}
          >
            <RefreshCw size={16} className={busy ? "spin" : ""} />
            刷新
          </button>
        }
      />
      <div className="review-toolbar">
        <div className="review-date-controls">
          <button
            className="icon-button"
            aria-label={view === "week" ? "上一周" : "前一天"}
            onClick={() =>
              navigate(shiftReviewDate(selectedDate, view === "week" ? -7 : -1))
            }
          >
            <ChevronLeft size={20} />
          </button>
          <strong>
            {view === "week"
              ? `${dateLabel(start)} — ${dateLabel(days[6])}`
              : dateLabel(start)}
            <small>{start.slice(0, 4)} 年</small>
          </strong>
          <button
            className="icon-button"
            aria-label={view === "week" ? "下一周" : "后一天"}
            onClick={() =>
              navigate(shiftReviewDate(selectedDate, view === "week" ? 7 : 1))
            }
          >
            <ChevronRight size={20} />
          </button>
          <button className="button secondary" onClick={() => navigate(today)}>
            今天
          </button>
        </div>
        <div className="review-view-controls">
          <input
            type="date"
            aria-label="选择日期"
            value={selectedDate}
            onChange={(e) => {
              if (validReviewDate(e.target.value)) navigate(e.target.value);
            }}
          />
          <select
            aria-label="录音设备"
            value={node}
            onChange={(e) => navigate(selectedDate, view, e.target.value)}
          >
            <option value="">全部设备</option>
            {node && !page?.devices.some((device) => device.id === node) && (
              <option value={node}>{node}</option>
            )}
            {page?.devices.map((device) => (
              <option value={device.id} key={device.id}>
                {device.label}
              </option>
            ))}
          </select>
          <div
            className="review-view-switch"
            role="group"
            aria-label="日历视图"
          >
            <button
              aria-pressed={view === "day"}
              onClick={() => navigate(selectedDate, "day")}
            >
              日
            </button>
            <button
              aria-pressed={view === "week"}
              onClick={() => navigate(selectedDate, "week")}
            >
              周
            </button>
          </div>
        </div>
      </div>
      {error && (
        <div className="banner error" role="alert">
          {error}
          <button onClick={() => setRefresh((v) => v + 1)}>重试</button>
        </div>
      )}
      {page && !page.enabled && (
        <div className="banner">自动分析尚未启用。已有回顾仍可查看。</div>
      )}
      <div className="review-caption">
        <span>
          <CalendarDays size={15} />
          每小时一份回顾，次日生成每日总结
        </span>
        <span>识别结果晚到时自动更新 · 近 30 天</span>
      </div>
      {!page && !error && (
        <p role="status" className="app-note">
          正在加载回顾…
        </p>
      )}
      {page && page.items.length === 0 && (
        <div className="review-empty" role="status">
          这段时间还没有分析结果。小时结束约 10
          分钟后进入分析，等待识别的内容会稍后补充。
        </div>
      )}
      <div
        className={`review-calendar panel ${view}`}
        ref={scroller}
        aria-label={view === "week" ? "每周小时日历" : "每日小时日历"}
        aria-busy={busy}
      >
        <div className="review-calendar-inner">
          <div className="review-calendar-head">
            <div className="review-time-heading">时间</div>
            {days.map((day) => (
              <div
                className={`review-day-heading ${day === today ? "today" : ""}`}
                key={day}
              >
                <button
                  aria-label={`查看 ${day} 日视图`}
                  onClick={() => navigate(day, "day")}
                >
                  <span>{weekday(day)}</span>
                  <strong>{day.slice(5).replace("-", "/")}</strong>
                  {day === today && <small>今天</small>}
                </button>
                {(buckets.get(day) ?? [])
                  .filter(reviewHasContent)
                  .map((item) => (
                    <button
                      className="review-daily-link"
                      key={item.id}
                      onClick={() => setSelected(item.id)}
                      title={label(item.node_id)}
                    >
                      每日总结{item.stale ? " · 更新中" : ""}
                    </button>
                  ))}
              </div>
            ))}
          </div>
          {hours.map((hour) => (
            <div className="review-calendar-row" key={hour}>
              <div className="review-time-label">
                {String(hour).padStart(2, "0")}:00
              </div>
              {days.map((day) => (
                <div
                  className={`review-calendar-cell ${day === today ? "today" : ""}`}
                  key={day}
                >
                  {(
                    buckets.get(`${day}T${String(hour).padStart(2, "0")}`) ?? []
                  ).map((item) =>
                    !reviewHasContent(item) ? null : reviewHasCard(item) ? (
                      <button
                        key={item.id}
                        className={`review-slot ${item.status} ${item.result.items.length > 0 && item.result.items.every((entry) => entry.source === "media") ? "media" : ""}`}
                        aria-label={`${day} ${hour}点 ${item.result.title || reviewStatus(item)}`}
                        onClick={() => setSelected(item.id)}
                      >
                        <span className="review-slot-meta">
                          <span>{reviewStatus(item)}</span>
                          <span>{item.counts.nonempty} 段文字</span>
                        </span>
                        <strong>
                          {item.result.title ||
                            (item.status === "failed"
                              ? "暂未生成回顾"
                              : "正在整理这一小时")}
                        </strong>
                        {view === "day" && item.result.summary && (
                          <p>{item.result.summary}</p>
                        )}
                        {!node && (page?.devices.length ?? 0) > 1 && (
                          <small>{label(item.node_id)}</small>
                        )}
                      </button>
                    ) : (
                      <button
                        key={item.id}
                        className={`review-slot-count ${item.status}`}
                        aria-label={`${day} ${hour}点 ${item.counts.nonempty} 段文字`}
                        onClick={() => setSelected(item.id)}
                      >
                        {item.counts.nonempty} 段文字
                      </button>
                    ),
                  )}
                </div>
              ))}
            </div>
          ))}
        </div>
      </div>
      <p className="app-note review-footnote">
        回顾基于已收到的录音转写。对话归属、事项状态可能需要确认；录到媒体声音不代表本人参与了其中的活动。
      </p>
      {selected !== null && (
        <ReviewDetail
          key={selected}
          id={selected}
          version={`${selectedVersion?.updated_at}:${selectedVersion?.status}:${selectedVersion?.stale}`}
          label={label}
          onClose={() => setSelected(null)}
          onRetry={() => setRefresh((v) => v + 1)}
        />
      )}
    </>
  );
}

function ReviewDetail({
  id,
  version,
  label,
  onClose,
  onRetry,
}: {
  id: number;
  version: string;
  label: (id: string) => string;
  onClose: () => void;
  onRetry: () => void;
}) {
  const [review, setReview] = useState<Review | null>(null);
  const [error, setError] = useState("");
  const [retrying, setRetrying] = useState(false);
  const [reload, setReload] = useState(0);
  useEffect(() => {
    const controller = new AbortController();
    void api<Review>(`/reviews/${id}`, { signal: controller.signal })
      .then((result) => {
        if (!controller.signal.aborted) {
          setReview(result);
          setError("");
        }
      })
      .catch((e) => {
        if (!controller.signal.aborted) setError(errorText(e));
      });
    return () => controller.abort();
  }, [id, version, reload]);
  async function retry() {
    setRetrying(true);
    try {
      await api(`/reviews/${id}/retry`, { method: "POST" });
      onRetry();
      setReload((v) => v + 1);
    } catch {
      setError("暂时无法重试，请稍后刷新。");
    } finally {
      setRetrying(false);
    }
  }
  const c = review?.counts;
  return (
    <Modal title={review?.result.title || "时段回顾"} onClose={onClose}>
      <div className="review-detail">
        {error && (
          <div className="banner error" role="alert">
            {error}
          </div>
        )}
        {!review && !error && <p role="status">正在加载详情…</p>}
        {review && c && (
          <>
            <div className="review-detail-meta">
              <span>
                {review.kind === "day"
                  ? review.start
                  : `${shanghaiDate(new Date(review.start))} ${timeLabel(review.start)}—${timeLabel(new Date(Date.parse(review.start) + 3600_000).toISOString())}`}
              </span>
              <Badge
                tone={
                  review.status === "failed" || review.stale
                    ? "warn"
                    : "neutral"
                }
              >
                {reviewStatus(review)}
              </Badge>
              <span>{label(review.node_id)}</span>
            </div>
            <div className="review-coverage">
              <strong>
                {c.total ? Math.round((c.completed / c.total) * 100) : 0}%
              </strong>
              <span>已知片段识别完成率</span>
              <span>
                {c.completed} / {c.total} 段 · {c.nonempty} 段有文字
              </span>
            </div>
            {c.pending + c.failed + c.lost > 0 && (
              <p className="app-note">
                {c.pending} 段等待识别，{c.failed} 段识别失败，{c.lost}{" "}
                段在识别前已清理。当前结果不代表完整记录。
              </p>
            )}
            {review.stale && (
              <div className="banner">
                有新的转写等待分析，下面暂时显示上一版回顾。
              </div>
            )}
            {review.status === "failed" && (
              <div className="banner">
                本次分析未成功，系统将自动重试。
                <button disabled={retrying} onClick={() => void retry()}>
                  {retrying ? "正在提交…" : "重新分析"}
                </button>
              </div>
            )}
            {review.result.summary && (
              <p className="review-summary">{review.result.summary}</p>
            )}
            <div className="review-items">
              {review.result.items.map((item, index) => (
                <article className="review-item" key={index}>
                  <div className="review-item-labels">
                    <Badge>{reviewKind[item.kind]}</Badge>
                    <span>{reviewSource[item.source]}</span>
                    <span>{reviewItemStatus[item.status]}</span>
                  </div>
                  <p>{item.text}</p>
                  {item.owner && item.owner !== "未确认" && (
                    <p className="app-note">对话中提及：{item.owner}</p>
                  )}
                  {item.uncertainty && (
                    <p className="review-uncertainty">
                      待确认：{item.uncertainty}
                    </p>
                  )}
                  <details>
                    <summary>
                      查看 {item.evidence_ids.length} 条原文依据
                    </summary>
                    <div className="review-evidence">
                      {item.evidence_ids.map((evidenceID) => {
                        const evidence = review.result.evidence?.find(
                          (entry) => entry.id === evidenceID,
                        );
                        return (
                          <blockquote key={evidenceID}>
                            <time>
                              {evidence
                                ? timeLabel(evidence.time)
                                : "原文不可用"}
                            </time>
                            <p>{evidence?.text ?? "这条证据已不可用。"}</p>
                          </blockquote>
                        );
                      })}
                    </div>
                  </details>
                </article>
              ))}
            </div>
            {review.updated_at && (
              <p className="app-note">
                更新于 {shanghaiDate(new Date(review.updated_at))}{" "}
                {timeLabel(review.updated_at)} ·
                事项均为录音中的陈述或候选，未自动创建待办。
              </p>
            )}
          </>
        )}
      </div>
    </Modal>
  );
}
