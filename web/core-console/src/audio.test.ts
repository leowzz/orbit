import assert from "node:assert/strict";
import test from "node:test";
import {
  workerStatus,
  workerDescription,
  workerTone,
  type AudioWorker,
} from "./audio.ts";

const worker: AudioWorker = {
  agent_id: "win-1",
  host_label: "Windows",
  model: "Qwen",
  device: "vulkan",
  status: "processing",
  online: true,
  received_at: "2026-10-31T00:00:00Z",
  updated_at: "2026-10-31T00:00:00Z",
};
test("processing workers remain online and positive", () => {
  assert.equal(workerStatus(worker), "正在识别");
  assert.equal(workerTone(worker), "good");
  assert.match(workerDescription(worker), /正在转写/);
});
test("connection evidence takes precedence over historical service status", () => {
  for (const [connection, label] of [
    ["stale", "心跳超时"],
    ["unconfirmed", "等待心跳"],
    ["offline", "已离线"],
  ] as const) {
    const value = { ...worker, online: false, connection_state: connection };
    assert.equal(workerStatus(value), label);
    assert.equal(workerTone(value), "neutral");
  }
  assert.equal(workerStatus({ ...worker, online: false }), "已离线");
});
test("connected service errors remain distinct from offline", () => {
  const value = { ...worker, status: "error" };
  assert.equal(workerStatus(value), "在线 · 服务异常");
  assert.equal(workerTone(value), "warn");
});
