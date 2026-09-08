const assert = require("node:assert/strict");
const { readFileSync } = require("node:fs");
const { test } = require("node:test");
const vm = require("node:vm");

function client() {
  const timers = [];
  const sources = [];
  const requests = [];
  const rendered = [];
  const element = {
    setAttribute() {}, addEventListener() {}, focus() {}, dataset: {},
  };
  const context = vm.createContext({
    document: {
      querySelector: () => element,
      documentElement: element,
      addEventListener() {},
    },
    window: {
      localStorage: { getItem() {}, removeItem() {} },
      setTimeout: (callback) => timers.push(callback),
      clearTimeout() {}, addEventListener() {},
    },
    setInterval() {},
    fetch: () => new Promise(() => {}), // Leave unrelated auth bootstrap pending.
    EventSource: class {
      constructor() { this.listeners = {}; sources.push(this); }
      addEventListener(name, callback) { this.listeners[name] = callback; }
      close() { this.closed = true; }
      emit(name, revision) {
        this.listeners[name]({ data: JSON.stringify({ revision }) });
      }
    },
    rendered,
    fetchState: () => new Promise((resolve) => requests.push(resolve)),
  });
  vm.runInContext(readFileSync(`${__dirname}/static/app.js`, "utf8"), context);
  vm.runInContext("render = snapshot => rendered.push(snapshot.revision); fetchAPI = fetchState; connectEvents();", context);
  return {
    timers, sources, requests, rendered, context,
    tick: () => timers.shift()(),
    respond: (index, revision) => requests[index]({
      ok: true, status: 200, json: async () => ({ revision }),
    }),
  };
}

test("a buffered burst skips all historical snapshots and fetches the current state once", async () => {
  const c = client();
  for (let revision = 1; revision <= 1000; revision++) c.sources[0].emit("message", revision);
  assert.equal(c.timers.length, 1);
  assert.deepEqual(c.rendered, []);
  const refresh = c.tick();
  assert.equal(c.requests.length, 1);
  c.respond(0, 1500);
  await refresh;
  assert.deepEqual(c.rendered, [1500]);
  assert.equal(c.timers.length, 0);
});

test("updates during a fetch schedule only one subsequent fetch", async () => {
  const c = client();
  c.sources[0].emit("open");
  const first = c.tick();
  for (let revision = 1; revision <= 50; revision++) c.sources[0].emit("message", revision);
  assert.equal(c.requests.length, 1);
  assert.equal(c.timers.length, 0);
  c.respond(0, 100);
  await first;
  assert.equal(c.timers.length, 1);
  const next = c.tick();
  c.respond(1, 200);
  await next;
  assert.deepEqual(c.rendered, [100, 200]);
});

test("replaced connections cannot render an in-flight response or schedule stale events", async () => {
  const c = client();
  c.sources[0].emit("message", 1);
  const oldRefresh = c.tick();
  vm.runInContext("connectEvents()", c.context);
  assert.equal(c.sources[0].closed, true);
  c.sources[0].emit("message", 2);
  assert.equal(c.timers.length, 0);
  c.respond(0, 100);
  await oldRefresh;
  assert.deepEqual(c.rendered, []);
  c.sources[1].emit("open");
  const refresh = c.tick();
  c.respond(1, 200);
  await refresh;
  assert.deepEqual(c.rendered, [200]);
});

test("auth expiry discards scheduled and in-flight state updates", async () => {
  const c = client();
  c.sources[0].emit("message", 1);
  const refresh = c.tick();
  vm.runInContext("clearAuth()", c.context);
  c.respond(0, 100);
  await refresh;
  assert.deepEqual(c.rendered, []);
  c.sources[0].emit("message", 2);
  assert.equal(c.timers.length, 0);
});

test("failed reads retry even without a new event, and empty state is not rendered", async () => {
  const c = client();
  c.sources[0].emit("open");
  const first = c.tick();
  c.requests[0]({ ok: false, status: 503 });
  await first;
  assert.equal(c.timers.length, 1);
  const retry = c.tick();
  c.requests[1]({ ok: true, status: 204 });
  await retry;
  assert.deepEqual(c.rendered, []);
  assert.equal(c.timers.length, 0);
});
