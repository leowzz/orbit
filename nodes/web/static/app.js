const elements = {
  fullscreenToggle: document.querySelector("#fullscreen-toggle"),
  connection: document.querySelector("#connection"),
  connectionLabel: document.querySelector("#connection-label"),
  viewFreshness: document.querySelector("#view-freshness"),
  codexFreshness: document.querySelector("#codex-freshness"),
  updatedAt: document.querySelector("#updated-at"),
  cost: document.querySelector("#cost"),
  currency: document.querySelector("#currency"),
  tokens: document.querySelector("#tokens"),
  tpm: document.querySelector("#tpm"),
  runningCount: document.querySelector("#running-count"),
  totalCount: document.querySelector("#total-count"),
  sessionList: document.querySelector("#session-list"),
  nodeID: document.querySelector("#node-id"),
  revision: document.querySelector("#revision"),
};

const freshnessLabels = {
  fresh: "数据新鲜",
  stale: "数据已过期",
  offline: "来源离线",
  unknown: "等待数据",
};

const statusLabels = {
  running: "运行中",
  completed: "已完成",
  failed: "失败",
  interrupted: "已中断",
  cancelled: "已取消",
  unknown: "未知",
};

const integerFormat = new Intl.NumberFormat("zh-CN");
const compactFormat = new Intl.NumberFormat("zh-CN", {
  minimumFractionDigits: 0,
  maximumFractionDigits: 2,
});
let latestSnapshot = null;

function fullscreenElement() {
  return document.fullscreenElement || document.webkitFullscreenElement;
}

function syncFullscreenState() {
  const active = Boolean(fullscreenElement());
  elements.fullscreenToggle.setAttribute("aria-pressed", String(active));
  elements.fullscreenToggle.title = active ? "退出全屏" : "进入全屏";
}

function invokeFullscreen(method, target) {
  try {
    const result = method.call(target);
    if (result && typeof result.catch === "function") {
      result.catch(syncFullscreenState);
    }
  } catch (_) {
    syncFullscreenState();
  }
}

function toggleFullscreen() {
  if (fullscreenElement()) {
    const exit = document.exitFullscreen || document.webkitExitFullscreen;
    if (exit) invokeFullscreen(exit, document);
    return;
  }
  const root = document.documentElement;
  const enter = root.requestFullscreen || root.webkitRequestFullscreen;
  if (enter) invokeFullscreen(enter, root);
}

function setConnection(state, label) {
  elements.connection.dataset.state = state;
  elements.connectionLabel.textContent = label;
}

function setFreshness(element, state) {
  const normalized = freshnessLabels[state] ? state : "unknown";
  element.dataset.state = normalized;
  element.textContent = freshnessLabels[normalized];
}

function formatCost(micros, currency) {
  return new Intl.NumberFormat("zh-CN", {
    style: "currency",
    currency: currency || "USD",
    minimumFractionDigits: 2,
    maximumFractionDigits: 2,
  }).format(micros / 1000000);
}

function formatCompactMetric(value) {
  const units = [
    { threshold: 1000000000, suffix: "B" },
    { threshold: 1000000, suffix: "M" },
    { threshold: 1000, suffix: "K" },
  ];
  for (const unit of units) {
    if (value >= unit.threshold) {
      return `${compactFormat.format(value / unit.threshold)}${unit.suffix}`;
    }
  }
  return integerFormat.format(value);
}

function formatDate(value) {
  if (!value) return "--";
  return new Intl.DateTimeFormat("zh-CN", {
    month: "2-digit",
    day: "2-digit",
    hour: "2-digit",
    minute: "2-digit",
    second: "2-digit",
  }).format(new Date(value));
}

function fitMetric(element) {
  element.style.fontSize = "";
  let size = Number.parseFloat(getComputedStyle(element).fontSize);
  while (element.scrollWidth > element.clientWidth && size > 16) {
    size -= 1;
    element.style.fontSize = `${size}px`;
  }
}

function fitMetrics() {
  document.querySelectorAll(".metric strong").forEach(fitMetric);
}

function relativeTime(value) {
  if (!value) return "时间未知";
  const seconds = Math.round((new Date(value).getTime() - Date.now()) / 1000);
  const absolute = Math.abs(seconds);
  const formatter = new Intl.RelativeTimeFormat("zh-CN", { numeric: "auto" });
  if (absolute < 60) return formatter.format(seconds, "second");
  if (absolute < 3600) return formatter.format(Math.round(seconds / 60), "minute");
  if (absolute < 86400) return formatter.format(Math.round(seconds / 3600), "hour");
  return formatter.format(Math.round(seconds / 86400), "day");
}

function appendText(parent, className, text) {
  const element = document.createElement("div");
  element.className = className;
  element.textContent = text;
  parent.append(element);
  return element;
}

function renderSessions(codex) {
  elements.sessionList.textContent = "";
  if (!codex || !codex.sessions || codex.sessions.length === 0) {
    const empty = document.createElement("div");
    empty.className = "empty-state";
    const orbit = document.createElement("span");
    orbit.className = "empty-orbit";
    orbit.setAttribute("aria-hidden", "true");
    const text = document.createElement("p");
    text.textContent = codex ? "暂无最近任务" : "等待 Codex 数据";
    empty.append(orbit, text);
    elements.sessionList.append(empty);
    return;
  }

  for (const session of codex.sessions) {
    const row = document.createElement("article");
    row.className = "session-row";

    const main = document.createElement("div");
    main.className = "session-main";
    appendText(main, "session-name", session.display_name || `会话 ${session.id.slice(0, 8)}`);
    appendText(main, "session-id", session.id);

    appendText(row, "session-project", session.project_name || "项目未公开");
    appendText(row, "session-model", session.model || "模型未知");

    const meta = document.createElement("div");
    meta.className = "session-meta";
    const status = appendText(meta, "status", statusLabels[session.status] || statusLabels.unknown);
    status.dataset.state = session.status || "unknown";
    if (session.process_alive) status.title = "本地进程存活";
    const updated = appendText(meta, "session-time", relativeTime(session.updated_at));
    updated.title = formatDate(session.updated_at);

    row.prepend(main);
    row.append(meta);
    elements.sessionList.append(row);
  }
}

function render(snapshot) {
  if (latestSnapshot) {
    const sameEpoch = latestSnapshot.core_epoch === snapshot.core_epoch;
    const currentProducedAt = Date.parse(latestSnapshot.produced_at || latestSnapshot.received_at);
    const nextProducedAt = Date.parse(snapshot.produced_at || snapshot.received_at);
    if (
      (sameEpoch && latestSnapshot.revision >= snapshot.revision) ||
      (!sameEpoch && nextProducedAt <= currentProducedAt)
    ) {
      return;
    }
  }
  latestSnapshot = snapshot;
  setFreshness(elements.viewFreshness, snapshot.freshness);
  elements.updatedAt.textContent = formatDate(snapshot.produced_at || snapshot.received_at);
  elements.nodeID.textContent = `Node ${snapshot.node_id}`;
  elements.revision.textContent = `Revision ${snapshot.revision}`;

  const usage = snapshot.usage;
  elements.cost.textContent = usage ? formatCost(usage.actual_cost_micros, usage.currency_code) : "--";
  elements.currency.textContent = usage && usage.currency_code ? usage.currency_code : "USD";
  elements.tokens.textContent = usage ? formatCompactMetric(usage.token_count) : "--";
  elements.tpm.textContent = usage ? integerFormat.format(usage.tpm) : "--";

  const codex = snapshot.codex;
  elements.runningCount.textContent = codex ? integerFormat.format(codex.running_count) : "--";
  elements.totalCount.textContent = codex ? `共 ${integerFormat.format(codex.total_count)} 个` : "共 -- 个";
  setFreshness(elements.codexFreshness, codex && codex.freshness ? codex.freshness : "unknown");
  renderSessions(codex);
  requestAnimationFrame(fitMetrics);
}

async function loadInitialState() {
  const response = await fetch("/api/state", { cache: "no-store" });
  if (response.ok && response.status !== 204) render(await response.json());
}

function connectEvents() {
  const events = new EventSource("/api/events");
  events.addEventListener("open", () => setConnection("live", "实时连接"));
  events.addEventListener("message", (event) => render(JSON.parse(event.data)));
  events.addEventListener("error", () => setConnection("retrying", "正在重连"));
}

loadInitialState().catch(() => setConnection("retrying", "正在重连"));
connectEvents();
elements.fullscreenToggle.addEventListener("click", toggleFullscreen);
document.addEventListener("fullscreenchange", syncFullscreenState);
document.addEventListener("webkitfullscreenchange", syncFullscreenState);
syncFullscreenState();
setInterval(() => {
  if (latestSnapshot && latestSnapshot.codex) renderSessions(latestSnapshot.codex);
}, 30000);
window.addEventListener("resize", fitMetrics);
