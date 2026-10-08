const assert = require("node:assert/strict");
const { readFileSync } = require("node:fs");
const { test } = require("node:test");
const vm = require("node:vm");
const { randomUUID } = require("node:crypto");
function client() {
  const elements = new Map();
  const context = vm.createContext({
    document: {
      getElementById: (id) => {
        if (!elements.has(id))
          elements.set(id, {
            value: "",
            disabled: false,
            hidden: false,
            textContent: "",
            addEventListener() {},
          });
        return elements.get(id);
      },
      addEventListener() {},
    },
    window: { addEventListener() {} },
    fetch: () => new Promise(() => {}),
    AbortSignal,
    URL,
    structuredClone,
    crypto: { randomUUID },
    setTimeout() {},
    clearTimeout() {},
    clearInterval() {},
    console,
  });
  vm.runInContext(
    readFileSync(`${__dirname}/inboxui/static/inbox.js`, "utf8"),
    context,
  );
  vm.runInContext("render=()=>{};save=async()=>{};", context);
  return {
    context,
    run: (source) => vm.runInContext(source, context),
    elements,
  };
}
test("failed snapshot pagination keeps the previous complete cache", async () => {
  const c = client();
  c.run(
    `state.items=[{id:'old',revision:'2'}];let pages=0;api=async()=>{if(pages++)throw Error('network');return {items:[{id:'new'}],generation:'g2',cursor:'99',next_id:'new',has_more:true};};`,
  );
  await assert.rejects(c.run("catchUp()"));
  assert.equal(c.run("state.items[0].id"), "old");
  assert.equal(c.run("state.generation"), null);
});
test("lost response retries the same operation and merges the receipt once", async () => {
  const c = client();
  c.run(`closed=false;catchUp=async()=>{};let attempts=[];let lose=true;
 state.outbox=[{op:{operation_id:'same-operation',item_id:'one',type:'create',kind:'text',body:'saved'}}];
 api=async(path,options)=>{attempts.push(JSON.parse(options.body));if(lose)throw Error('network');return {id:'one',revision:'1',created_at:'2026-10-08',body:'saved'};};`);
  await c.run("sync()");
  assert.equal(c.run("state.outbox.length"), 1);
  c.run("lose=false");
  await c.run("sync()");
  assert.equal(c.run("state.outbox.length"), 0);
  assert.equal(c.run("state.items.length"), 1);
  assert.equal(
    c.run("JSON.stringify(attempts[0])===JSON.stringify(attempts[1])"),
    true,
  );
});
test("conflicts preserve the draft and newer authoritative content", async () => {
  const c = client();
  c.run(
    `closed=false;catchUp=async()=>{};state.items=[{id:'one',body:'remote',revision:'3'}];state.outbox=[{op:{item_id:'one',body:'local',type:'update',expected_revision:'1'}}];api=async()=>{throw {code:'conflict',status:409}};`,
  );
  await c.run("sync()");
  assert.equal(c.run("state.outbox[0].error"), "conflict");
  assert.equal(c.run("state.outbox[0].op.body"), "local");
  assert.equal(c.run("state.items[0].body"), "remote");
});
test("failed local commit keeps the composer and prevents submission", async () => {
  const c = client();
  c.elements.get("body").value = "do not lose this";
  c.run(
    `state.draft='do not lose this';save=async()=>{throw Error('quota')};let submitted=false;sync=()=>{submitted=true};`,
  );
  await c.elements.get("composer").onsubmit({ preventDefault() {} });
  assert.equal(c.run("state.outbox.length"), 0);
  assert.equal(c.run("state.draft"), "do not lose this");
  assert.equal(c.run("submitted"), false);
  assert.equal(c.elements.get("body").value, "do not lose this");
});
test("links never open credential-bearing or executable URLs", () => {
  const c = client();
  assert.equal(
    c.run(
      `JSON.stringify(urls('https://example.com/a。 https://user:pass@example.com javascript:alert(1)'))`,
    ),
    '["https://example.com/a"]',
  );
});

test("a periodic sync cannot publish an operation before its local commit", async () => {
  const c = client();
  c.run(
    `closed=false;catchUp=async()=>{};save=()=>new Promise(()=>{});let uploads=0;api=async()=>{uploads++;};void queue('create',null,{kind:'text',body:'durable first'});`,
  );
  await c.run("sync()");
  assert.equal(c.run("uploads"), 0);
});
