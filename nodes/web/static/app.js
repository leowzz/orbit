const elements = {
  authGate: document.querySelector("#auth-gate"),
  authForm: document.querySelector("#auth-form"),
  authPassword: document.querySelector("#auth-password"),
  authSubmit: document.querySelector("#auth-submit"),
  authError: document.querySelector("#auth-error"),
  fullscreenToggle: document.querySelector("#fullscreen-toggle"),
  themeToggle: document.querySelector("#theme-toggle"),
  colorScheme: document.querySelector('meta[name="color-scheme"]'),
  connection: document.querySelector("#connection"),
  connectionLabel: document.querySelector("#connection-label"),
  updatedAt: document.querySelector("#updated-at"),
  cost: document.querySelector("#cost"),
  currency: document.querySelector("#currency"),
  tokens: document.querySelector("#tokens"),
  tpm: document.querySelector("#tpm"),
  runningCount: document.querySelector("#running-count"),
  totalCount: document.querySelector("#total-count"),
  sessionList: document.querySelector("#session-list"),
  actionStatus: document.querySelector("#action-status"),
  nodeID: document.querySelector("#node-id"),
  revision: document.querySelector("#revision"),
};

const authStorageKey = "orbit.web.auth";
const themeStorageKey = "orbit.web.theme";
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
let authToken = "";
let authExpiresAt = 0;
let authExpiryTimer = null;
let eventSource = null;

function storedTheme() {
  try {
    const theme = window.localStorage.getItem(themeStorageKey);
    return theme === "dark" ? "dark" : "light";
  } catch (_) {
    return "light";
  }
}

function setTheme(theme, persist) {
  const dark = theme === "dark";
  const nextLabel = dark ? "切换到白天模式" : "切换到黑夜模式";
  document.documentElement.setAttribute("data-theme", dark ? "dark" : "light");
  elements.colorScheme.setAttribute("content", dark ? "dark" : "light");
  elements.themeToggle.setAttribute("aria-pressed", String(dark));
  elements.themeToggle.setAttribute("aria-label", nextLabel);
  elements.themeToggle.title = nextLabel;
  if (!persist) return;
  try {
    window.localStorage.setItem(themeStorageKey, dark ? "dark" : "light");
  } catch (_) {
    // Keep the selected theme for this page when storage is unavailable.
  }
}

function toggleTheme() {
  const current = document.documentElement.getAttribute("data-theme");
  setTheme(current === "dark" ? "light" : "dark", true);
}

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

function setAuthGate(visible) {
  elements.authGate.hidden = !visible;
  if (visible) {
    elements.authError.hidden = true;
    elements.authPassword.focus();
  }
}

function readStoredAuth() {
  try {
    const raw = window.localStorage.getItem(authStorageKey);
    if (!raw) return null;
    const saved = JSON.parse(raw);
    if (!saved || typeof saved !== "object") return null;
    const expiresAt = Date.parse(saved.expires_at);
    if (typeof saved.token !== "string" || !saved.token || !Number.isFinite(expiresAt) || expiresAt <= Date.now()) {
      window.localStorage.removeItem(authStorageKey);
      return null;
    }
    return { token: saved.token, expiresAt };
  } catch (_) {
    return null;
  }
}

function persistAuth(token, expiresAt) {
  authToken = token;
  authExpiresAt = expiresAt;
  try {
    window.localStorage.setItem(authStorageKey, JSON.stringify({ token, expires_at: new Date(expiresAt).toISOString() }));
  } catch (_) {
    // The session remains usable until this page is closed when storage is unavailable.
  }
  scheduleAuthExpiry();
}

function clearAuth() {
  authToken = "";
  authExpiresAt = 0;
  if (authExpiryTimer) window.clearTimeout(authExpiryTimer);
  authExpiryTimer = null;
  if (eventSource) {
    eventSource.close();
    eventSource = null;
  }
  try {
    window.localStorage.removeItem(authStorageKey);
  } catch (_) {
    // Ignore unavailable browser storage.
  }
  try {
    fetch("/api/auth/logout", { method: "POST" });
  } catch (_) {
    // The cookie will naturally expire if logout cannot be sent.
  }
  setAuthGate(true);
  setConnection("locked", "需要认证");
}

function scheduleAuthExpiry() {
  if (authExpiryTimer) window.clearTimeout(authExpiryTimer);
  if (!authExpiresAt) return;
  const delay = Math.max(1000, authExpiresAt - Date.now());
  authExpiryTimer = window.setTimeout(clearAuth, delay);
}

async function fetchAPI(url, options) {
  const requestOptions = options || {};
  const headers = requestOptions.headers || {};
  if (authToken) headers.Authorization = `Bearer ${authToken}`;
  requestOptions.headers = headers;
  const response = await fetch(url, requestOptions);
  if (response.status === 401) {
    clearAuth();
    throw new Error("authentication required");
  }
  return response;
}

function formatCost(micros, currency) {
  return new Intl.NumberFormat("en-US", {
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
  const element = document.createElement("span");
  element.className = className;
  element.textContent = text;
  parent.append(element);
  return element;
}

async function openCodexSession(row, session) {
  if (row.disabled || !latestSnapshot) return;
  row.disabled = true;
  row.dataset.action = "pending";
  elements.actionStatus.textContent = `正在打开 ${session.display_name || session.id}`;
  try {
    const response = await fetchAPI(`/api/sessions/${encodeURIComponent(session.id)}/open`, {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ view_revision: latestSnapshot.revision }),
    });
    if (!response.ok) throw new Error(`request failed with status ${response.status}`);
    row.dataset.action = "sent";
    row.title = "打开请求已发送";
    elements.actionStatus.textContent = "打开请求已发送";
  } catch (_) {
    row.dataset.action = "failed";
    row.title = "打开请求失败";
    elements.actionStatus.textContent = "打开请求失败";
  }
  row.disabled = false;
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
    const row = document.createElement("button");
    row.type = "button";
    row.className = "session-row";
    row.title = "在 Codex 中打开";
    row.addEventListener("click", () => openCodexSession(row, session));

    const content = document.createElement("span");
    content.className = "session-row-content";

    const main = document.createElement("span");
    main.className = "session-main";
    appendText(main, "session-name", session.display_name || `会话 ${session.id.slice(0, 8)}`);

    const details = document.createElement("span");
    details.className = "session-details";
    appendText(details, "session-project", session.project_name || "项目未公开");
    appendText(details, "session-model", session.model || "模型未知");
    appendText(details, "session-id", session.id);
    main.append(details);

    const meta = document.createElement("span");
    meta.className = "session-meta";
    const status = appendText(meta, "status", statusLabels[session.status] || statusLabels.unknown);
    status.dataset.state = session.status || "unknown";
    row.dataset.state = session.status || "unknown";
    if (session.process_alive) status.title = "本地进程存活";
    const updated = appendText(meta, "session-time", relativeTime(session.updated_at));
    updated.title = formatDate(session.updated_at);

    content.prepend(main);
    content.append(meta);
    row.append(content);
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
  renderSessions(codex);
  requestAnimationFrame(fitMetrics);
}

async function loadInitialState() {
  const response = await fetchAPI("/api/state", { cache: "no-store" });
  if (!response.ok && response.status !== 204) throw new Error(`request failed with status ${response.status}`);
  if (response.ok && response.status !== 204) render(await response.json());
}

function connectEvents() {
  if (eventSource) eventSource.close();
  eventSource = new EventSource("/api/events");
  eventSource.addEventListener("open", () => setConnection("live", "实时连接"));
  eventSource.addEventListener("message", (event) => render(JSON.parse(event.data)));
  eventSource.addEventListener("reload", () => window.location.reload());
  eventSource.addEventListener("error", () => setConnection("retrying", "正在重连"));
}

async function submitAuth(event) {
  event.preventDefault();
  const password = elements.authPassword.value;
  elements.authSubmit.disabled = true;
  elements.authError.hidden = true;
  try {
    const response = await fetch("/api/auth/login", {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ password }),
    });
    if (!response.ok) throw new Error("invalid credentials");
    const payload = await response.json();
    const expiresAt = Date.parse(payload.expires_at);
    if (typeof payload.token !== "string" || !payload.token || !Number.isFinite(expiresAt) || expiresAt <= Date.now()) {
      throw new Error("invalid session");
    }
    persistAuth(payload.token, expiresAt);
    elements.authPassword.value = "";
    setAuthGate(false);
    await fetchAPI("/api/auth/session", { method: "POST" });
    await loadInitialState();
    connectEvents();
  } catch (_) {
    clearAuth();
    elements.authError.textContent = "密码错误或认证服务不可用";
    elements.authError.hidden = false;
  } finally {
    elements.authSubmit.disabled = false;
  }
}

async function bootstrap() {
  const configResponse = await fetch("/api/auth/config", { cache: "no-store" });
  if (!configResponse.ok) throw new Error("authentication configuration unavailable");
  const authConfig = await configResponse.json();
  if (!authConfig.required) {
    setAuthGate(false);
    await loadInitialState();
    connectEvents();
    return;
  }
  const saved = readStoredAuth();
  if (!saved) {
    setAuthGate(true);
    setConnection("locked", "需要认证");
    return;
  }
  authToken = saved.token;
  authExpiresAt = saved.expiresAt;
  scheduleAuthExpiry();
  setAuthGate(false);
  await fetchAPI("/api/auth/session", { method: "POST" });
  await loadInitialState();
  connectEvents();
}

elements.authForm.addEventListener("submit", submitAuth);
setTheme(storedTheme(), false);
bootstrap().catch(() => setConnection("retrying", "正在重连"));
elements.fullscreenToggle.addEventListener("click", toggleFullscreen);
elements.themeToggle.addEventListener("click", toggleTheme);
elements.connection.addEventListener("click", () => window.location.reload());
document.addEventListener("fullscreenchange", syncFullscreenState);
document.addEventListener("webkitfullscreenchange", syncFullscreenState);
syncFullscreenState();
setInterval(() => {
  if (latestSnapshot && latestSnapshot.codex) renderSessions(latestSnapshot.codex);
}, 30000);
window.addEventListener("resize", fitMetrics);
