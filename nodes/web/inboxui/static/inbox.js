/* Personal inbox: cookie authorization, atomic local cache and durable operations. */
"use strict";
const $ = (id) => document.getElementById(id);
const emptyState = () => ({
  items: [],
  outbox: [],
  draft: "",
  kind: "text",
  photo: null,
  generation: null,
  cursor: "0",
});
let state = emptyState(),
  db,
  mode = "device",
  node = "",
  syncing = false,
  closed = true,
  timer,
  events,
  limit = 100,
  editing,
  lastRender = "",
  toastTimer;
const pictures = new Map();
const pendingCommits = new Set();
let releaseLock, lockedNode;
async function claimDevice(id) {
  if (lockedNode === id) return;
  releaseLock?.();
  if (!navigator.locks) throw { code: "storage_unavailable" };
  await new Promise((resolve, reject) => {
    navigator.locks
      .request("orbit-inbox-" + id, { ifAvailable: true }, async (lock) => {
        if (!lock) {
          reject({ code: "inbox_in_use" });
          return;
        }
        lockedNode = id;
        await new Promise((release) => {
          releaseLock = () => {
            lockedNode = null;
            release();
          };
          resolve();
        });
      })
      .catch(reject);
  });
}
function note(text) {
  $("toast").textContent = text;
  $("toast").hidden = false;
  clearTimeout(toastTimer);
  toastTimer = setTimeout(() => ($("toast").hidden = true), 3000);
}
function reason(error) {
  return (
    {
      inbox_in_use:
        "此设备的收件箱已在另一个标签页打开，请切换到该标签页，或关闭后刷新这里。",
      storage_unavailable:
        "此浏览器无法安全保存离线内容，请使用支持本地存储的新版浏览器。",
      unauthenticated: "连接已失效，请重新连接。",
      device_revoked: "这台设备的访问已被撤销，请在管理台重新授权。",
      conflict: "另一台设备已修改这条消息。你的修改已保留，可复制后重新编辑。",
      invalid_fields: "内容格式不正确，请检查后重试。",
      invalid_image: "请选择 JPEG、PNG 或 GIF 图片。",
      image_too_large: "图片不能超过 10 MB。",
      attachment_unavailable: "图片已不可用。",
    }[error.code] || "暂时无法同步，内容已保存在此浏览器，恢复连接后会重试。"
  );
}
async function api(path, options = {}) {
  const response = await fetch("/api/v1/" + path, {
    ...options,
    credentials: "same-origin",
    signal: AbortSignal.timeout(45000),
  });
  if (!response.ok) {
    let data = {};
    try {
      data = await response.json();
    } catch {}
    throw Object.assign(new Error(data.code || "unavailable"), data, {
      status: response.status,
    });
  }
  return response.status === 204 ? null : response.json();
}
function json(body) {
  return {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify(body),
  };
}
function openDB(id) {
  return new Promise((resolve, reject) => {
    const request = indexedDB.open("orbit-inbox-" + id, 1);
    request.onupgradeneeded = () => request.result.createObjectStore("state");
    request.onerror = () => reject(request.error);
    request.onsuccess = () => resolve(request.result);
  });
}
function load() {
  return new Promise((resolve, reject) => {
    const request = db.transaction("state").objectStore("state").get("inbox");
    request.onsuccess = () => resolve(request.result || emptyState());
    request.onerror = () => reject(request.error);
  });
}
function save() {
  const snapshot = structuredClone(state);
  return new Promise((resolve, reject) => {
    const tx = db.transaction("state", "readwrite");
    tx.objectStore("state").put(snapshot, "inbox");
    tx.oncomplete = resolve;
    tx.onerror = () => reject(tx.error);
    tx.onabort = () => reject(tx.error);
  });
}
function storeError() {
  $("error").textContent =
    "无法保存到此浏览器。请检查存储空间或浏览器权限；未发送内容仍保留在输入框。";
  $("error").hidden = false;
}
function urls(body) {
  return [
    ...new Set(
      (body.match(/https?:\/\/[^\s<>"\u3000]+/gi) || []).map((x) =>
        x.replace(/[，。！？、；：）》」』]+$/g, ""),
      ),
    ),
  ].filter((x) => {
    try {
      const u = new URL(x);
      return !u.username && !u.password;
    } catch {
      return false;
    }
  });
}
function button(text, action) {
  const b = document.createElement("button");
  b.type = "button";
  b.textContent = text;
  b.onclick = action;
  return b;
}
function textElement(tag, text, className) {
  const el = document.createElement(tag);
  el.textContent = text;
  if (className) el.className = className;
  return el;
}
function draftChanged() {
  state.draft = $("body").value;
  state.kind = $("todo").checked ? "todo" : "text";
  void save().catch(storeError);
}
function composer() {
  $("body").value = state.draft;
  $("todo").checked = state.kind === "todo";
  $("attachment").hidden = !state.photo;
  $("attachment-name").textContent = state.photo?.name || "";
}
function projected() {
  const items = new Map(state.items.map((i) => [i.id, { ...i }]));
  for (const entry of state.outbox) {
    const op = entry.op;
    if (entry.error) continue;
    let item = items.get(op.item_id);
    if (op.type === "create") {
      item = {
        id: op.item_id,
        kind: op.kind,
        body: op.body,
        completed: false,
        created_at: entry.created,
        revision: "0",
        attachment_id: op.attachment_id,
        photo: entry.photo,
      };
      items.set(item.id, item);
    }
    if (item) {
      if (op.type === "update") item.body = op.body;
      if (op.type === "set_completed") item.completed = op.completed;
      if (op.type === "set_kind") {
        item.kind = op.kind;
        item.completed = false;
      }
      item.pending = true;
    }
  }
  return [...items.values()]
    .filter((i) => !i.deleted_at)
    .sort(
      (a, b) =>
        b.created_at.localeCompare(a.created_at) || b.id.localeCompare(a.id),
    );
}
async function picture(id, full = false) {
  const key = id + (full ? "" : "?thumbnail=1");
  if (pictures.has(key)) return pictures.get(key);
  const response = await fetch("/api/v1/attachments/" + key, {
    credentials: "same-origin",
    signal: AbortSignal.timeout(30000),
  });
  if (!response.ok) throw new Error("image");
  const url = URL.createObjectURL(await response.blob());
  pictures.set(key, url);
  return url;
}
function render() {
  const query = $("search").value.trim().toLocaleLowerCase(),
    filter = $("filter").value;
  const all = projected().filter(
    (i) =>
      i.body.toLocaleLowerCase().includes(query) &&
      (filter === "all" ||
        (filter === "links" && urls(i.body).length) ||
        (filter === "todo" && i.kind === "todo" && !i.completed) ||
        (filter === "completed" && i.kind === "todo" && i.completed) ||
        (filter === "image" && i.kind === "image")),
  );
  const signature = JSON.stringify([
    all,
    state.outbox.map((e) => [e.op, e.error]),
    query,
    filter,
    limit,
  ]);
  if (signature === lastRender) return;
  lastRender = signature;
  const list = $("messages");
  list.replaceChildren();
  for (const entry of state.outbox.filter((e) => e.error)) {
    const panel = textElement("article", "", "message draft-error");
    panel.append(
      textElement("p", reason({ code: entry.error })),
      textElement(
        "p",
        entry.op.body || "这条消息的修改未完成。",
        "message-body",
      ),
    );
    panel.append(
      button("复制修改", () => copy(entry.op.body || "")),
      button("重试", async () => {
        delete entry.error;
        await save();
        render();
        void sync();
      }),
      button("放弃修改", async () => {
        if (!confirm("放弃这次未同步的修改？")) return;
        state.outbox = state.outbox.filter((e) => e !== entry);
        await save();
        render();
      }),
    );
    list.append(panel);
  }
  let day = "";
  for (const item of all.slice(0, limit)) {
    const date = new Date(item.created_at),
      heading = date.toLocaleDateString("zh-CN", {
        year: "numeric",
        month: "long",
        day: "numeric",
      });
    if (day !== heading) {
      list.append(textElement("h2", heading, "date-heading"));
      day = heading;
    }
    const card = textElement(
      "article",
      "",
      "message" + (item.completed ? " completed" : ""),
    );
    const head = textElement("div", "", "message-head");
    const stamp = textElement(
      "time",
      date.toLocaleTimeString("zh-CN", { hour: "2-digit", minute: "2-digit" }) +
        (item.pending ? " · 待同步" : ""),
    );
    stamp.dateTime = item.created_at;
    head.append(stamp);
    if (item.body) head.append(button("复制", () => copy(item.body)));
    const menu = document.createElement("details");
    menu.append(textElement("summary", "···"));
    menu.firstChild.setAttribute("aria-label", "更多操作");
    const choices = textElement("div", "", "menu");
    const busy = state.outbox.some((e) => e.op.item_id === item.id);
    const edit = button("编辑", () => {
      editing = item;
      $("edit-body").value = item.body;
      $("editor").showModal();
    });
    edit.disabled = busy;
    choices.append(edit);
    if (item.kind !== "image") {
      const kind = button(item.kind === "todo" ? "改为文本" : "设为待办", () =>
        queue("set_kind", item, {
          kind: item.kind === "todo" ? "text" : "todo",
        }),
      );
      kind.disabled = busy;
      choices.append(kind);
    }
    const remove = button("删除", () => {
      if (
        confirm(
          "删除这条消息？所有设备都会隐藏，可在管理台的「已删除」中恢复。",
        )
      )
        void queue("delete", item);
    });
    remove.disabled = busy;
    choices.append(remove);
    menu.append(choices);
    head.append(menu);
    card.append(head);
    if (item.kind === "image") {
      const photo = button("正在读取图片…", async () => {
        try {
          $("full-image").src = item.photo
            ? URL.createObjectURL(item.photo)
            : await picture(item.attachment_id, true);
          $("image-view").showModal();
        } catch {
          note("图片未能加载，请稍后重试");
        }
      });
      photo.className = "photo-button";
      const show = (url) => {
        const img = document.createElement("img");
        img.src = url;
        img.alt = item.body || "保存的图片";
        img.loading = "lazy";
        photo.replaceChildren(img);
      };
      if (item.photo) show(URL.createObjectURL(item.photo));
      else if (item.attachment_id)
        void picture(item.attachment_id)
          .then(show)
          .catch(() => {
            photo.textContent = "图片未加载 · 点击重试";
          });
      card.append(photo);
    }
    const row = textElement("div", "", "body-row");
    if (item.kind === "todo") {
      const checkbox = document.createElement("input");
      checkbox.type = "checkbox";
      checkbox.checked = item.completed;
      checkbox.disabled = busy;
      checkbox.setAttribute(
        "aria-label",
        item.completed ? "撤销完成" : "完成待办",
      );
      checkbox.onchange = () =>
        queue("set_completed", item, { completed: checkbox.checked });
      row.append(checkbox);
    }
    const content = textElement("div", "", "body-content");
    const body = textElement("p", item.body, "message-body");
    content.append(body);
    if (item.body.length > 500 || item.body.split("\n").length > 8) {
      body.classList.add("collapsed");
      const expand = button("展开", () => {
        const collapsed = body.classList.toggle("collapsed");
        expand.textContent = collapsed ? "展开" : "收起";
      });
      content.append(expand);
    }
    const links = urls(item.body);
    if (links.length) {
      const linksEl = textElement("div", "", "links");
      for (const link of links) {
        const a = document.createElement("a");
        a.href = link;
        a.target = "_blank";
        a.rel = "noopener noreferrer";
        a.textContent = "↗ " + new URL(link).hostname;
        a.title = link;
        linksEl.append(a);
      }
      content.append(linksEl);
    }
    row.append(content);
    card.append(row);
    list.append(card);
  }
  if (!all.length) {
    const empty = textElement("div", "", "empty");
    empty.append(
      textElement(
        "strong",
        query
          ? "没有找到匹配的消息"
          : filter === "all"
            ? "给之后的自己留点东西"
            : "这里暂时没有消息",
      ),
      textElement(
        "p",
        query ? "换个关键词试试。" : "链接、想法或截图，随手发到这里。",
      ),
    );
    list.append(empty);
  }
  $("more").hidden = all.length <= limit;
}
async function copy(body) {
  try {
    await navigator.clipboard.writeText(body);
    note("已复制");
  } catch {
    note("无法访问剪贴板，请选择正文复制");
  }
}
async function queue(type, item, fields = {}) {
  if (item && state.outbox.some((entry) => entry.op.item_id === item.id)) {
    note("这条消息还有修改待同步");
    return false;
  }
  const op = {
    operation_id: crypto.randomUUID(),
    item_id: item?.id || crypto.randomUUID(),
    expected_revision: item?.revision || "0",
    type,
    ...fields,
  };
  const entry = { op, created: new Date().toISOString() };
  state.outbox.push(entry);
  pendingCommits.add(op.operation_id);
  try {
    await save();
    pendingCommits.delete(op.operation_id);
    render();
    void sync();
    return true;
  } catch {
    state.outbox = state.outbox.filter((e) => e !== entry);
    pendingCommits.delete(op.operation_id);
    storeError();
    return false;
  }
}
function merge(items) {
  const map = new Map(state.items.map((i) => [i.id, i]));
  for (const item of items) {
    const old = map.get(item.id);
    if (!old || BigInt(item.revision) > BigInt(old.revision))
      map.set(item.id, item);
  }
  state.items = [...map.values()];
}
async function catchUp() {
  if (state.generation) {
    try {
      while (true) {
        const page = await api(
          "changes?" +
            new URLSearchParams({
              generation: state.generation,
              after: state.cursor,
            }),
        );
        merge(page.changes.map((c) => c.item));
        state.cursor = page.cursor;
        await save();
        if (!page.has_more) return;
      }
    } catch (e) {
      if (e.code !== "reset_required") throw e;
    }
  }
  let page = await api("sync/snapshot"),
    all = [...page.items];
  while (page.has_more) {
    page = await api(
      "sync/snapshot?" +
        new URLSearchParams({
          generation: page.generation,
          at: page.cursor,
          after_id: page.next_id,
        }),
    );
    all.push(...page.items);
  }
  state.items = all;
  state.generation = page.generation;
  state.cursor = page.cursor;
  await save();
}
async function sync() {
  if (syncing || closed) return;
  syncing = true;
  $("sync-state").textContent = "正在同步…";
  try {
    await catchUp();
    for (const entry of [...state.outbox]) {
      if (entry.error || pendingCommits.has(entry.op.operation_id)) continue;
      try {
        if (entry.photo && !entry.op.attachment_id) {
          const attachment = await api("attachments", {
            method: "POST",
            body: entry.photo,
          });
          entry.op.attachment_id = attachment.id;
          await save();
        }
        const item = await api("operations", json(entry.op));
        merge([item]);
        state.outbox = state.outbox.filter((e) => e !== entry);
        await save();
      } catch (e) {
        if (
          e.status === 400 ||
          e.status === 409 ||
          e.status === 404 ||
          e.status === 413
        ) {
          entry.error = e.code;
          await save();
        } else throw e;
      }
    }
    await catchUp();
    $("error").hidden = true;
    $("sync-state").textContent = state.outbox.length
      ? "有修改待处理"
      : "已同步";
  } catch (e) {
    $("sync-state").textContent = "等待同步";
    $("error").textContent = reason(e);
    $("error").hidden = false;
    if (e.status === 401 || e.code === "device_revoked") {
      lock(reason(e));
    }
  } finally {
    syncing = false;
    render();
  }
}
function lock(message) {
  closed = true;
  events?.close();
  clearInterval(timer);
  $("workspace").hidden = true;
  $("login").hidden = false;
  $("login-error").textContent = message;
}
async function start() {
  const status = await api("status");
  node = status.node_id;
  await claimDevice(node);
  if (db) db.close();
  try {
    db = await openDB(node);
    state = await load();
  } catch {
    releaseLock?.();
    throw { code: "storage_unavailable" };
  }
  closed = false;
  lastRender = "";
  composer();
  render();
  $("login").hidden = true;
  $("workspace").hidden = false;
  clearInterval(timer);
  events?.close();
  events = new EventSource("/api/v1/events");
  events.addEventListener("sync", () => void sync());
  timer = setInterval(() => void sync(), 15000);
  void sync();
}
$("login-form").onsubmit = async (event) => {
  event.preventDefault();
  $("connect").disabled = true;
  $("login-error").textContent = "";
  try {
    const value =
      mode === "gateway"
        ? $("gateway-password").value
        : $("credential").value.trim();
    if (mode === "gateway") {
      const response = await fetch(
        "/api/auth/login",
        json({ password: value }),
      );
      if (!response.ok) throw { code: "unauthenticated" };
    } else {
      let token = value;
      if (value.startsWith("{")) {
        const config = JSON.parse(value);
        if (
          config.type !== "orbit-app" ||
          config.version !== 1 ||
          new URL(config.server).origin !== location.origin
        )
          throw new Error("wrong_server");
        token = config.token;
      }
      await api("session", json({ token }));
    }
    $("credential").value = "";
    $("gateway-password").value = "";
    await start();
  } catch (e) {
    $("login-error").textContent =
      e.message === "wrong_server"
        ? "连接信息属于另一个服务，请打开对应服务的 /inbox/ 页面。"
        : e.status === 401 || e.code === "unauthenticated"
          ? "连接信息无效，请检查后重试。"
          : e.code
            ? reason(e)
            : "连接未完成，请检查服务和授权信息后重试。";
  } finally {
    $("connect").disabled = false;
  }
};
$("composer").onsubmit = async (event) => {
  event.preventDefault();
  if ($("send").disabled) return;
  const body = $("body").value.trim();
  if (!body && !state.photo) return;
  const old = { draft: state.draft, photo: state.photo, kind: state.kind };
  const entry = {
    created: new Date().toISOString(),
    photo: state.photo,
    op: {
      operation_id: crypto.randomUUID(),
      item_id: crypto.randomUUID(),
      type: "create",
      expected_revision: "0",
      kind: state.photo ? "image" : state.kind,
      body,
    },
  };
  state.outbox.push(entry);
  pendingCommits.add(entry.op.operation_id);
  state.draft = "";
  state.photo = null;
  $("send").disabled = true;
  $("body").disabled = true;
  try {
    await save();
    pendingCommits.delete(entry.op.operation_id);
    composer();
    $("search").value = "";
    $("filter").value = "all";
    limit = 100;
    render();
    window.scrollTo({ top: 0, behavior: "smooth" });
    void sync();
  } catch {
    state.outbox = state.outbox.filter((e) => e !== entry);
    Object.assign(state, old);
    pendingCommits.delete(entry.op.operation_id);
    storeError();
  } finally {
    $("send").disabled = false;
    $("body").disabled = false;
  }
};
$("body").oninput = draftChanged;
$("todo").onchange = draftChanged;
$("body").onkeydown = (event) => {
  if (
    (event.metaKey || event.ctrlKey) &&
    event.key === "Enter" &&
    !event.isComposing
  ) {
    event.preventDefault();
    $("composer").requestSubmit();
  }
};
$("choose-photo").onclick = () => $("photo").click();
async function selectPhoto(file) {
  if (!file) return;
  if (file.size > 10 * 1024 * 1024) {
    note("图片不能超过 10 MB");
    return;
  }
  if (!["image/png", "image/jpeg", "image/gif"].includes(file.type)) {
    note("请选择 JPEG、PNG 或 GIF 图片");
    return;
  }
  const previous = state.photo;
  state.photo = file;
  try {
    await save();
    composer();
  } catch {
    state.photo = previous;
    storeError();
  }
}
$("photo").onchange = () => {
  const file = $("photo").files[0];
  $("photo").value = "";
  void selectPhoto(file);
};
$("body").addEventListener("paste", (event) => {
  const file = event.clipboardData?.files[0];
  if (file) {
    event.preventDefault();
    void selectPhoto(file);
  }
});
$("composer").addEventListener("dragover", (event) => {
  if (event.dataTransfer.types.includes("Files")) event.preventDefault();
});
$("composer").addEventListener("drop", (event) => {
  const file = event.dataTransfer?.files[0];
  if (file) {
    event.preventDefault();
    void selectPhoto(file);
  }
});
$("remove-photo").onclick = async () => {
  const previous = state.photo;
  state.photo = null;
  try {
    await save();
    composer();
  } catch {
    state.photo = previous;
    storeError();
  }
};
$("search").oninput = () => {
  limit = 100;
  render();
};
$("filter").onchange = () => {
  limit = 100;
  render();
};
$("more").onclick = () => {
  limit += 100;
  render();
};
$("refresh").onclick = () => void sync();
$("edit-form").onsubmit = async (event) => {
  event.preventDefault();
  const body = $("edit-body").value.trim();
  if (!body && editing.kind !== "image") return;
  if (await queue("update", editing, { body })) $("editor").close();
};
$("cancel-edit").onclick = () => $("editor").close();
$("close-image").onclick = () => $("image-view").close();
$("logout").onclick = async () => {
  if (
    (state.outbox.length || state.draft || state.photo) &&
    !confirm(
      "此浏览器仍有草稿或未同步的修改。退出会清除这些本机内容，确定退出？",
    )
  )
    return;
  if (syncing) {
    note("正在同步，请稍后退出");
    return;
  }
  try {
    if (mode === "gateway") {
      const r = await fetch("/api/auth/logout", { method: "POST" });
      if (!r.ok) throw new Error();
    } else await api("session", { method: "DELETE" });
    closed = true;
    events?.close();
    clearInterval(timer);
    state = emptyState();
    await save();
    db.close();
    db = null;
    releaseLock?.();
    for (const url of pictures.values()) URL.revokeObjectURL(url);
    pictures.clear();
    $("messages").replaceChildren();
    composer();
    lock("");
  } catch {
    note("未能退出，请重试");
  }
};
window.addEventListener("online", () => void sync());
document.addEventListener("visibilitychange", () => {
  if (!document.hidden) void sync();
});
(async () => {
  try {
    mode = (await api("browser")).mode;
    if (mode === "gateway") {
      $("credential-label").textContent = "Web Node 访问密码";
      $("credential-label").htmlFor = "gateway-password";
      $("credential").hidden = true;
      $("credential").disabled = true;
      $("gateway-password").hidden = false;
      $("gateway-password").disabled = false;
      $("gateway-password").required = true;
      $("login-help").textContent =
        "使用此 Web Node 的访问密码，连接信息由服务端管理。";
      $("connect").textContent = "进入收件箱";
    }
    if (mode === "unavailable") {
      lock(
        "此网页尚未连接收件箱。请打开 Orbit 服务提供的收件箱网址，或联系管理员完成连接。",
      );
      $("connect").disabled = true;
      return;
    }
    await start();
  } catch (e) {
    lock(
      e.status === 401
        ? ""
        : e.code
          ? reason(e)
          : "服务暂时无法访问，请检查网络后刷新。",
    );
  }
})();
