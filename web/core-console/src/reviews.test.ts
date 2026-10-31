import assert from "node:assert/strict";
import test from "node:test";
import {
  reviewWeekStart,
  shiftReviewDate,
  shanghaiDate,
  reviewCellKey,
  validReviewDate,
  reviewStatus,
  reviewHasCard,
  reviewHasContent,
  type Review,
} from "./reviews.ts";

const reviewWith = (nonempty: number, items: Review["result"]["items"] = []) =>
  ({ counts: { nonempty }, result: { items } }) as Review;

test("calendar uses Shanghai independently of the browser timezone", () => {
  assert.equal(shanghaiDate(new Date("2026-10-24T16:01:00Z")), "2026-10-25");
  assert.equal(reviewCellKey("2026-10-24T16:01:00Z"), "2026-10-25T00");
  assert.equal(reviewCellKey("2026-10-24T15:59:59Z"), "2026-10-24T23");
});
test("Monday week navigation handles Sunday, year changes and leap days", () => {
  assert.equal(reviewWeekStart("2026-11-01"), "2026-10-26");
  assert.equal(reviewWeekStart("2026-10-26"), "2026-10-26");
  assert.equal(reviewWeekStart("2027-01-01"), "2026-12-28");
  assert.equal(shiftReviewDate("2024-02-28", 1), "2024-02-29");
  assert.equal(shiftReviewDate("2026-12-31", 1), "2027-01-01");
  assert.equal(shiftReviewDate("2026-11-01", -7), "2026-10-25");
});
test("invalid URL dates are rejected", () => {
  for (const value of [null, "bad", "2026-02-29", "2026-13-01", "2026-1-01"])
    assert.equal(validReviewDate(value), false);
  assert.equal(validReviewDate("2024-02-29"), true);
});
test("stale and incomplete results stay visible as provisional", () => {
  const review = {
    status: "completed",
    stale: false,
    counts: { pending: 1, failed: 0, lost: 0 },
  } as Review;
  assert.equal(reviewStatus(review), "暂定结果");
  assert.equal(reviewStatus({ ...review, stale: true }), "正在更新");
  assert.equal(reviewStatus({ ...review, status: "failed" }), "分析失败");
  assert.equal(
    reviewStatus({
      ...review,
      counts: { ...review.counts, pending: 0, lost: 1 },
    }),
    "有数据缺口",
  );
});
test("empty windows are hidden while text-only windows show a count", () => {
  assert.equal(reviewHasContent(reviewWith(0)), false);
  assert.equal(reviewHasCard(reviewWith(0)), false);
  assert.equal(reviewHasContent(reviewWith(2)), true);
  assert.equal(reviewHasCard(reviewWith(2)), false);
  assert.equal(
    reviewHasCard(
      reviewWith(2, [
        { source: "conversation" } as Review["result"]["items"][number],
      ]),
    ),
    true,
  );
});
