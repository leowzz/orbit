import assert from "node:assert/strict";
import test from "node:test";
import { AutoSave } from "./autoSave.ts";

function deferred<T>() {
  let resolve!: (value: T) => void;
  let reject!: (error: unknown) => void;
  const promise = new Promise<T>((yes, no) => {
    resolve = yes;
    reject = no;
  });
  return { promise, resolve, reject };
}

test("coalesces continuous slider edits into one persisted value", async (t) => {
  t.mock.timers.enable({ apis: ["setTimeout"] });
  const writes: number[] = [];
  const queue = new AutoSave(100, async (value) => {
    writes.push(value);
    return value;
  });
  queue.set(120);
  queue.set(140);
  queue.set(160);
  t.mock.timers.tick(349);
  assert.deepEqual(writes, []);
  t.mock.timers.tick(1);
  await queue.flush();
  assert.deepEqual(writes, [160]);
  assert.equal(queue.getSnapshot().dirty, false);
});

test("serializes writes and uses the successful revision for newer edits", async () => {
  const first = deferred<{ revision: number; value: number }>();
  const writes: { value: number; revision: number }[] = [];
  const queue = new AutoSave(
    { revision: 4, value: 0 },
    async (value, saved) => {
      writes.push({ value: value.value, revision: saved!.revision });
      if (writes.length === 1) return first.promise;
      return { value: value.value, revision: saved!.revision + 1 };
    },
    (a, b) => a?.value === b?.value,
  );
  queue.set({ revision: 4, value: 1 });
  const saving = queue.flush();
  await Promise.resolve();
  queue.set({ revision: 4, value: 2 });
  queue.set({ revision: 4, value: 3 });
  assert.equal(writes.length, 1);
  assert.equal(queue.getSnapshot().value!.value, 3);
  first.resolve({ revision: 5, value: 1 });
  await saving;
  await queue.flush();
  assert.deepEqual(writes, [
    { value: 1, revision: 4 },
    { value: 3, revision: 5 },
  ]);
  assert.deepEqual(queue.getSnapshot().value, { revision: 6, value: 3 });
  assert.equal(queue.getSnapshot().dirty, false);
});

test("reverting a switch during an in-flight write persists the reverted value", async () => {
  const first = deferred<boolean>();
  const writes: boolean[] = [];
  const queue = new AutoSave(false, async (value) => {
    writes.push(value);
    return writes.length === 1 ? first.promise : value;
  });
  queue.set(true);
  const saving = queue.flush();
  await Promise.resolve();
  queue.set(false);
  first.resolve(true);
  await saving;
  await queue.flush();
  assert.deepEqual(writes, [true, false]);
  assert.equal(queue.getSnapshot().value, false);
  assert.equal(queue.getSnapshot().dirty, false);
});

test("failed writes keep edits and stop until an explicit retry", async (t) => {
  t.mock.timers.enable({ apis: ["setTimeout"] });
  let calls = 0;
  const failure = new Error("offline");
  const queue = new AutoSave(0, async (value) => {
    if (++calls === 1) throw failure;
    return value;
  });
  queue.set(2);
  await queue.flush();
  t.mock.timers.tick(10000);
  await queue.flush();
  assert.equal(calls, 1);
  assert.equal(queue.getSnapshot().value, 2);
  assert.equal(queue.getSnapshot().dirty, true);
  assert.equal(queue.getSnapshot().error, failure);
  await queue.retry();
  assert.equal(calls, 2);
  assert.equal(queue.getSnapshot().dirty, false);
  assert.equal(queue.getSnapshot().error, null);
});

test("a conflict preserves the latest edit without resubmitting older state", async () => {
  const first = deferred<number>();
  let calls = 0;
  const queue = new AutoSave(0, async () => {
    calls++;
    return first.promise;
  });
  queue.set(1);
  const saving = queue.flush();
  await Promise.resolve();
  queue.set(2);
  first.reject(new Error("conflict"));
  await saving;
  await queue.flush();
  assert.equal(calls, 1);
  assert.equal(queue.getSnapshot().value, 2);
  assert.equal(queue.getSnapshot().dirty, true);
});

test("loading server state cancels unsent edits and does not write it back", async (t) => {
  t.mock.timers.enable({ apis: ["setTimeout"] });
  let calls = 0;
  const queue = new AutoSave(0, async (value) => {
    calls++;
    return value;
  });
  queue.set(1);
  queue.reset(5);
  t.mock.timers.tick(1000);
  await queue.flush();
  assert.equal(calls, 0);
  assert.equal(queue.getSnapshot().value, 5);
});

test("an uncertain response still retries a switch reverted to its original value", async () => {
  const first = deferred<boolean>();
  const writes: boolean[] = [];
  const queue = new AutoSave(false, async (value) => {
    writes.push(value);
    return writes.length === 1 ? first.promise : value;
  });
  queue.set(true);
  const saving = queue.flush();
  await Promise.resolve();
  queue.set(false);
  first.reject(new Error("response lost after server accepted the write"));
  await saving;
  assert.equal(queue.getSnapshot().dirty, true);
  await queue.retry();
  assert.deepEqual(writes, [true, false]);
  assert.equal(queue.getSnapshot().dirty, false);
});

test("leaving the editor can flush the last change before its debounce expires", async () => {
  const writes: number[] = [];
  const queue = new AutoSave(0, async (value) => {
    writes.push(value);
    return value;
  });
  queue.set(9);
  await queue.flush();
  assert.deepEqual(writes, [9]);
});

test("the first explicit choice can create a device route even when it matches defaults", async () => {
  const writes: number[] = [];
  const queue = new AutoSave(0, async (value) => {
    writes.push(value);
    return value;
  });
  queue.set(0, 350, true);
  await queue.flush();
  assert.deepEqual(writes, [0]);
  assert.equal(queue.getSnapshot().dirty, false);
});
