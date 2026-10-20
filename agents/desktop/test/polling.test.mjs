import assert from "node:assert/strict";
import { test } from "node:test";
import { startPolling } from "../src/polling.ts";

const flush = async () => {
  for (let i = 0; i < 5; i++) await Promise.resolve();
};

test("hidden polling stops, drops pending results, and resumes immediately", async (t) => {
  t.mock.timers.enable({ apis: ["setTimeout"] });
  let resolve;
  let reads = 0;
  const values = [];
  const read = () => {
    reads++;
    return new Promise((done) => {
      resolve = done;
    });
  };
  const stop = startPolling(read, (value) => values.push(value), 1000);
  assert.equal(reads, 1);
  t.mock.timers.tick(10000);
  assert.equal(reads, 1, "slow IPC must not overlap");
  stop();
  resolve("stale");
  await flush();
  t.mock.timers.tick(60000);
  assert.equal(reads, 1);
  assert.deepEqual(values, []);
  const stopAgain = startPolling(read, (value) => values.push(value), 1000);
  assert.equal(reads, 2, "showing the window refreshes immediately");
  resolve("fresh");
  await flush();
  assert.deepEqual(values, ["fresh"]);
  t.mock.timers.tick(1000);
  assert.equal(reads, 3);
  stopAgain();
  resolve("hidden again");
  await flush();
});

test("polling recovers from errors and clears scheduled work on hide", async (t) => {
  t.mock.timers.enable({ apis: ["setTimeout"] });
  let reads = 0;
  const values = [];
  const errors = [];
  const stop = startPolling(
    async () => {
      if (++reads === 1) throw new Error("offline");
      return reads;
    },
    (value) => values.push(value),
    1000,
    (error) => errors.push(error.message),
  );
  await flush();
  assert.deepEqual(errors, ["offline"]);
  t.mock.timers.tick(1000);
  await flush();
  assert.deepEqual(values, [2]);
  stop();
  t.mock.timers.tick(60000);
  assert.equal(reads, 2);
});
