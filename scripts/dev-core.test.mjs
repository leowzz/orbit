import test from "node:test";
import assert from "node:assert/strict";
import { spawn } from "node:child_process";
import { mkdtemp, readFile, rm, writeFile } from "node:fs/promises";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { setTimeout as delay } from "node:timers/promises";

async function fixture(t, fail = false) {
  const dir = await mkdtemp(join(tmpdir(), "orbit-dev-test-"));
  const fake = join(dir, "make.mjs");
  await writeFile(fake, `#!${process.execPath}
import { spawn } from 'node:child_process';
import { writeFileSync } from 'node:fs';
const target = process.argv.at(-1);
const grandchild = spawn(process.execPath, ['-e', 'setInterval(() => {}, 1000)'], {stdio:'ignore'});
writeFileSync(process.env.FIXTURE_DIR + '/' + target, String(grandchild.pid));
console.log(target + ' ready');
process.on('SIGTERM', () => process.exit(0));
if (process.env.FIXTURE_FAIL === '1' && target === 'dev-console') setTimeout(() => process.exit(7), 150);
setInterval(() => {}, 1000);
`, { mode: 0o755 });
  const child = spawn(process.execPath, [new URL("./dev-core.mjs", import.meta.url).pathname], {
    env: { ...process.env, MAKE_BIN: fake, FIXTURE_DIR: dir, FIXTURE_FAIL: fail ? "1" : "0" },
    stdio: ["ignore", "pipe", "pipe"],
  });
  let output = "";
  child.stdout.on("data", data => { output += data; });
  child.stderr.on("data", data => { output += data; });
  const done = new Promise(resolve => child.on("close", code => resolve(code)));
  t.after(async () => {
    if (child.exitCode === null) child.kill("SIGTERM");
    await done;
    for (const name of ["dev-core-api", "dev-console"]) {
      try { process.kill(Number(await readFile(join(dir, name), "utf8")), "SIGKILL"); } catch {}
    }
    await rm(dir, { recursive: true, force: true });
  });
  async function assertDescendantsStopped() {
    for (const name of ["dev-core-api", "dev-console"]) {
      const pid = Number(await readFile(join(dir, name), "utf8"));
      let alive = true;
      for (let i = 0; i < 50 && alive; i++) {
        try { process.kill(pid, 0); await delay(20); } catch (error) { assert.equal(error.code, "ESRCH"); alive = false; }
      }
      assert.equal(alive, false, `${name} descendant ${pid} survived`);
    }
  }
  return {child, done, output: () => output, assertDescendantsStopped};
}

test("Ctrl-C stops both services and descendants; piped logs remain plain", {timeout:10000}, async t => {
  const run = await fixture(t);
  for (let i = 0; i < 100 && (!run.output().includes('[Vite] dev-console ready') || !run.output().includes('[Core] dev-core-api ready')); i++) await delay(20);
  assert.match(run.output(), /\[Core\] dev-core-api ready/);
  assert.match(run.output(), /http:\/\/127\.0\.0\.1:5173/);
  run.child.kill("SIGINT");
  assert.equal(await run.done, 130);
  assert.doesNotMatch(run.output(), /\x1b/);
  await run.assertDescendantsStopped();
});

test("a failing service stops its sibling and preserves the exit status", {timeout:10000}, async t => {
  const run = await fixture(t, true);
  assert.equal(await run.done, 7);
  assert.match(run.output(), /进程退出 \(7\)/);
  await run.assertDescendantsStopped();
});


test("log colors survive while terminal controls are removed", async () => {
  const { sanitizeLog } = await import("./dev-log.mjs");
  const styled = "\x1b[33m[W] 中文\x1b[0m \x1b[38;2;20;40;60mRGB\x1b[m";
  assert.equal(sanitizeLog(styled, true), styled);
  const controls = "\x1b[2J\x1b[H\x1b[?1049l\x1b]0;title\x07";
  assert.equal(sanitizeLog(controls + styled, true), styled);
  assert.equal(sanitizeLog(controls + styled, false), "[W] 中文 RGB");
});
