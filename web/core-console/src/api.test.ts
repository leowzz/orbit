import assert from "node:assert/strict";
import { test } from "node:test";
import { agentSource } from "./api.ts";
import type { Agent, Source } from "./api.ts";

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
