import assert from "node:assert/strict";
import { test } from "node:test";
import {
  knownIcons,
  mergeRecommendations,
} from "../src/recommendation-model.ts";

const response = (
  icon,
  iconKey = "hash-a",
  target = "/Applications/A.app",
) => ({
  supported: true,
  items: [
    {
      app: { id: target, target, kind: "app", title: "A", icon },
      iconKey,
      lastUsed: 123,
    },
  ],
});

test("cached icons survive omitted payloads and unchanged lists keep their identity", () => {
  const first = mergeRecommendations(null, response("base64-png"));
  assert.deepEqual(knownIcons(first), { "/Applications/A.app": "hash-a" });
  const next = mergeRecommendations(first, response(""));
  assert.equal(next, first);
  assert.equal(next.items[0].app.icon, "base64-png");
});

test("changed, removed, and newly appearing icons cannot reuse stale cache entries", () => {
  const first = mergeRecommendations(null, response("old-png"));
  const changed = mergeRecommendations(first, response("new-png", "hash-b"));
  assert.equal(changed.items[0].app.icon, "new-png");
  const removed = mergeRecommendations(changed, response("", "hash-empty"));
  assert.equal(removed.items[0].app.icon, "");
  const different = mergeRecommendations(
    first,
    response("other-png", "hash-a", "/Applications/B.app"),
  );
  assert.equal(different.items[0].app.icon, "other-png");
  assert.deepEqual(knownIcons(different), { "/Applications/B.app": "hash-a" });
  const reordered = mergeRecommendations(first, { ...first, items: [] });
  assert.deepEqual(knownIcons(reordered), {});
});
