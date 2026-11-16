import assert from "node:assert/strict";
import test from "node:test";
import { OLED_HEIGHT, OLED_WIDTH, renderOledBuffer } from "./oledPixels.ts";

function litCount(
  buf: Uint8Array,
  x0 = 0,
  y0 = 0,
  x1 = OLED_WIDTH,
  y1 = OLED_HEIGHT,
) {
  let count = 0;
  for (let y = y0; y < y1; y++) {
    for (let x = x0; x < x1; x++) {
      count += buf[y * OLED_WIDTH + x];
    }
  }
  return count;
}

test("renders 128x32 OLED buffer with centered primary and right-aligned telemetry", () => {
  const buf = renderOledBuffer({
    primary: "3.11",
    secondary: "C42% M78%",
    footer: "24.8/32G",
    stale: false,
  });
  assert.equal(buf.length, OLED_WIDTH * OLED_HEIGHT);
  // Primary renders on the left half; secondary & footer sit on the right half.
  assert.ok(litCount(buf, 0, 6, 70, 26) > 40);
  assert.ok(litCount(buf, 74, 4, 128, 12) > 30);
  assert.ok(litCount(buf, 74, 20, 128, 28) > 30);
  // Fresh views do not draw the 2x2 stale marker at (124, 2).
  assert.equal(litCount(buf, 124, 2, 126, 4), 0);
});

test("draws 2x2 stale dot and countdown left column when stale weekly view is active", () => {
  const buf = renderOledBuffer({
    primary: "50%",
    secondary: "12 11/16",
    footer: "8  14:30",
    stale: true,
  });
  assert.equal(litCount(buf, 124, 2, 126, 4), 4);
  // Countdown left column draws days and hours at x=0..11.
  assert.ok(litCount(buf, 0, 4, 12, 12) > 8);
  assert.ok(litCount(buf, 0, 20, 12, 28) > 4);
});
