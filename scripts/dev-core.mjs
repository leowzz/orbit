#!/usr/bin/env node
import { spawn } from "node:child_process";
import { createInterface } from "node:readline";
import { stripVTControlCharacters } from "node:util";

const output = process.stdout;
const interactive = output.isTTY && process.env.TERM !== "dumb" && process.env.ORBIT_DEV_PLAIN !== "1";
const children = [];
const lines = [];
let stopping = false;
let exitCode = 0;
let repaint;
let deadline;
const statuses = { Core: "启动中", Vite: "启动中" };
const address = "http://127.0.0.1:5173";

function draw() {
  if (!interactive) return;
  const width = Math.max(1, output.columns || 80);
  const height = Math.max(1, output.rows || 24);
  const header = [
    ` Orbit Core · 前端 ${address}`,
    ` Core: ${statuses.Core}  |  Vite: ${statuses.Vite}  |  Ctrl-C 停止全部`,
    "─".repeat(width),
  ];
  // Wrapping is disabled; position and clear each physical row explicitly.
  const visible = [...header, ...lines.slice(-Math.max(0, height - header.length))].slice(0, height);
  output.write("\x1b[H" + visible.map((line, index) =>
    `\x1b[${index + 1};1H\x1b[2K${line.replace(/[\r\n]/g, " ")}`,
  ).join("") + "\x1b[J");
}

function log(name, value) {
  // Child output must not clear or reposition the terminal's fixed header.
  const line = `[${name}] ${stripVTControlCharacters(value).replace(/[\x00-\x08\x0b-\x1f\x7f]/g, "")}`;
  if (!interactive) { output.write(line + "\n"); return; }
  lines.push(line);
  if (lines.length > 500) lines.shift();
  if (!repaint) repaint = setTimeout(() => { repaint = undefined; draw(); }, 33);
}

function signalGroup(child, signal) {
  if (!child.pid) return;
  try { process.kill(process.platform === "win32" ? child.pid : -child.pid, signal); }
  catch (error) { if (error.code !== "ESRCH") log("dev", error.message); }
}

function finish() {
  clearTimeout(deadline);
  clearTimeout(repaint);
  // A shell/go-run parent can exit before its descendants: clean up its group too.
  children.forEach(({ child }) => signalGroup(child, "SIGKILL"));
  if (interactive) output.write("\x1b[?7h\x1b[?25h\x1b[?1049l");
  console.log(`Orbit 开发进程已停止。前端地址：${address}`);
  process.exit(exitCode);
}

function stop(code) {
  if (stopping) return;
  stopping = true;
  exitCode = code;
  children.forEach(({ child }) => signalGroup(child, "SIGTERM"));
  deadline = setTimeout(finish, 2500);
  if (children.every(entry => entry.closed)) finish();
}

function start(name, target) {
  const child = spawn(process.env.MAKE_BIN || "make", ["--no-print-directory", target], {
    env: { ...process.env, NO_COLOR: "1", FORCE_COLOR: "0" },
    detached: process.platform !== "win32",
    stdio: ["ignore", "pipe", "pipe"],
  });
  const entry = { child, closed: false };
  children.push(entry);
  statuses[name] = "运行中";
  for (const stream of [child.stdout, child.stderr]) {
    createInterface({ input: stream }).on("line", line => log(name, line));
  }
  child.on("error", error => { log(name, error.message); stop(1); });
  child.on("close", (code, signal) => {
    entry.closed = true;
    statuses[name] = "已停止";
    if (!stopping) {
      log(name, `进程退出 (${signal || code})，停止其他开发进程。`);
      stop(code || 1);
    }
    if (stopping && children.every(item => item.closed)) finish();
  });
}

process.on("SIGINT", () => stop(130));
process.on("SIGTERM", () => stop(143));
output.on("resize", draw);
if (interactive) {
  // Alternate screen + disabled wrapping keep logs inside their physical rows.
  output.write("\x1b[?1049h\x1b[?25l\x1b[?7l\x1b[2J");
} else {
  console.log(`Orbit Core · 前端 ${address} · Ctrl-C 停止全部`);
}
start("Core", "dev-core-api");
start("Vite", "dev-console");
draw();
