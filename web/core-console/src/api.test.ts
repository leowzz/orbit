import assert from "node:assert/strict";
import { test } from "node:test";
import { agentSource, appConnectionLabel, visibleDeviceRoutes } from "./api.ts";
import type { Agent, Source, NetworkState, RouteDocument } from "./api.ts";

function agent(sources?: Source[]): Agent {
  return {
    id: "desktop",
    usage_fresh: true,
    codex_fresh: true,
    state: {
      agentId: "desktop",
      hostLabel: "电脑",
      agentVersion: "1.0",
      agentEpoch: "test",
      sources,
    },
  };
}

test("HTTP polling activity is distinct from a persistent connection", () => {
  const now = Date.parse("2026-10-15T12:00:00Z");
  assert.equal(appConnectionLabel({ connections: 1 }, now), "已连接");
  assert.equal(
    appConnectionLabel(
      { connections: 0, last_seen: "2026-10-15T11:59:45Z" },
      now,
    ),
    "最近活跃",
  );
  for (const last_seen of [
    undefined,
    "invalid",
    "2026-10-15T11:59:00Z",
    "2026-10-15T13:00:00Z",
  ])
    assert.equal(
      appConnectionLabel({ connections: 0, last_seen }, now),
      "未连接",
    );
});

test("recognizes the protobuf JSON source types returned by Core", () => {
  const sources: Source[] = JSON.parse(`[
    {"observationType":"OBSERVATION_TYPE_USAGE","enabled":true,"health":"SOURCE_HEALTH_HEALTHY"},
    {"observationType":"OBSERVATION_TYPE_CODEX","enabled":true,"health":"SOURCE_HEALTH_HEALTHY"}
  ]`);
  assert.equal(agentSource(agent(sources), "usage"), sources[0]);
  assert.equal(agentSource(agent(sources), "codex"), sources[1]);
  assert.equal(agentSource(agent(sources), "usage")?.enabled, true);
  assert.equal(agentSource(agent(sources), "codex")?.enabled, true);
});

test("a reported disabled source stays distinct from an unreported source", () => {
  const desktop = agent([{ observationType: "OBSERVATION_TYPE_USAGE" }]);
  assert.ok(agentSource(desktop, "usage"));
  assert.equal(Boolean(agentSource(desktop, "usage")?.enabled), false);
  assert.equal(agentSource(desktop, "codex"), undefined);
  assert.equal(agentSource(agent(), "usage"), undefined);
});

test("does not treat an unrelated source or data freshness as an enabled source", () => {
  const desktop = agent([
    { observationType: "OBSERVATION_TYPE_UNSPECIFIED", enabled: true },
  ]);
  assert.equal(agentSource(desktop, "usage"), undefined);
  assert.equal(agentSource(desktop, "codex"), undefined);
});

test("archive visibility filters the view without changing saved routes", () => {
  const state: NetworkState = {
    core_id: "test",
    core_epoch: "test",
    now: "",
    agents: [],
    nodes: [],
    archived_nodes: ["old-device"],
  };
  const route = {
    profile: "overview-web",
    inputs: [{ agent_id: "desktop", observation_type: "usage" as const }],
  };
  const document: RouteDocument = {
    revision: 12,
    publish_pending: true,
    routes: { "old-device": route, "active-device": route },
  };
  const visible = visibleDeviceRoutes(document, state);
  assert.deepEqual(Object.keys(visible.routes), ["active-device"]);
  assert.equal(visible.revision, 12);
  assert.equal(visible.publish_pending, true);
  assert.equal(Object.keys(document.routes).length, 2);
  assert.deepEqual(
    visibleDeviceRoutes(document, { ...state, archived_nodes: [] }).routes,
    document.routes,
  );
});
