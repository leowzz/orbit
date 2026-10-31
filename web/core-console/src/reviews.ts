export interface ReviewCounts {
  total: number;
  completed: number;
  pending: number;
  failed: number;
  lost: number;
  nonempty: number;
}
export interface ReviewItem {
  kind: "requirement" | "decision" | "idea" | "todo" | "progress" | "reference";
  text: string;
  source: "conversation" | "media" | "unknown";
  owner: string;
  status:
    "proposed" | "in_progress" | "reported_done" | "cancelled" | "unknown";
  evidence_ids: string[];
  uncertainty: string;
}
export interface Review {
  id: number;
  kind: "hour" | "day";
  node_id: string;
  start: string;
  status: "queued" | "running" | "completed" | "failed";
  counts: ReviewCounts;
  result: {
    title: string;
    summary: string;
    items: ReviewItem[];
    evidence?: { id: string; time: string; text: string }[];
    model: string;
  };
  stale: boolean;
  updated_at: string;
}
export interface ReviewPage {
  items: Review[];
  enabled: boolean;
  timezone: string;
  devices: { id: string; label: string }[];
}
export function shanghaiDate(value: Date = new Date()): string {
  return new Date(value.getTime() + 8 * 3600_000).toISOString().slice(0, 10);
}
export function validReviewDate(value: string | null): value is string {
  if (!value || !/^\d{4}-\d{2}-\d{2}$/.test(value)) return false;
  const stamp = new Date(value + "T00:00:00Z");
  return (
    Number.isFinite(stamp.getTime()) &&
    stamp.toISOString().slice(0, 10) === value
  );
}
export function shiftReviewDate(value: string, days: number): string {
  const date = new Date(value + "T00:00:00Z");
  date.setUTCDate(date.getUTCDate() + days);
  return date.toISOString().slice(0, 10);
}
export function reviewWeekStart(value: string): string {
  const day = new Date(value + "T00:00:00Z").getUTCDay();
  return shiftReviewDate(value, -(day + 6) % 7);
}
export function reviewCellKey(start: string): string {
  return new Date(Date.parse(start) + 8 * 3600_000).toISOString().slice(0, 13);
}
export function reviewStatus(review: Review): string {
  if (review.status === "failed") return "分析失败";
  if (review.stale) return "正在更新";
  if (review.status === "running") return "分析中";
  if (review.status === "queued") return "等待分析";
  if (review.counts.pending > 0) return "暂定结果";
  if (review.counts.failed + review.counts.lost > 0) return "有数据缺口";
  return "已分析";
}
export function reviewHasContent(review: Review): boolean {
  return review.counts.nonempty > 0;
}
export function reviewHasCard(review: Review): boolean {
  return reviewHasContent(review) && review.result.items.length > 0;
}
export const reviewKind = {
  requirement: "需求",
  decision: "决定",
  idea: "想法",
  todo: "待办候选",
  progress: "进展",
  reference: "参考内容",
};
export const reviewSource = {
  conversation: "对话候选",
  media: "播放内容",
  unknown: "来源不明",
};
export const reviewItemStatus = {
  proposed: "已提出",
  in_progress: "进行中",
  reported_done: "口述已完成",
  cancelled: "已取消",
  unknown: "状态未知",
};
