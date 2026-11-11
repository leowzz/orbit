// The inbox controller owns only the DOM inside the React page. Its durable
// queue and snapshot protocol are retained from the former browser client.
export function mountBox(root) {
  const $ = (id) => root.querySelector("#" + id);
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
    syncing = false,
    syncRequested = false,
    closed = true,
    timer,
    events,
    limit = 100,
    editing,
    editPhoto,
    starting = false,
    lastRender = "",
    toastTimer,
    disposed = false;
  const controller = new AbortController();
  const pictures = new Map();
  const channel =
    typeof BroadcastChannel === "function"
      ? new BroadcastChannel("orbit-workbench-box")
      : null;
  function note(text) {
    $("toast").textContent = text;
    $("toast").hidden = false;
    clearTimeout(toastTimer);
    toastTimer = setTimeout(() => ($("toast").hidden = true), 3000);
  }
  function reason(error) {
    return (
      {
        storage_unavailable:
          "此浏览器无法安全保存离线内容，请使用支持本地存储的新版浏览器。",
        unauthenticated: "登录已失效，请重新登录。",
        unavailable: "收件箱暂时不可用，请检查连接后重试。",
        rate_limited: "尝试次数过多，请稍后再试。",
        device_revoked: "这台设备的访问已被撤销，请在管理台重新授权。",
        conflict: "这条消息已有新修改。你的修改已保留，可复制后重新编辑。",
        invalid_fields: "内容格式不正确，请检查后重试。",
        invalid_image: "请选择 JPEG、PNG 或 GIF 图片。",
        image_too_large: "图片不能超过 10 MB。",
        attachment_unavailable: "图片已不可用。",
      }[error.code] || "暂时无法同步，内容已保存在此浏览器，恢复连接后会重试。"
    );
  }
  async function api(path, options = {}) {
    const response = await fetch("/api/app/" + path, {
      ...options,
      credentials: "same-origin",
      signal: AbortSignal.any([controller.signal, AbortSignal.timeout(45000)]),
    });
    if (!response.ok) {
      let data = {};
      try {
        data = await response.json();
      } catch {}
      if (response.status === 401)
        window.dispatchEvent(new Event("orbit:unauthorized"));
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
      const request = indexedDB.open("orbit-workbench-box-" + id, 1);
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
  // Read and patch the shared record in one transaction. A tab never writes
  // its in-memory copy of another tab's draft, cache or pending operations.
  function save(change) {
    return new Promise((resolve, reject) => {
      const tx = db.transaction("state", "readwrite");
      const store = tx.objectStore("state");
      const request = store.get("inbox");
      let current;
      request.onsuccess = () => {
        current = request.result || emptyState();
        if (change(current) !== false) store.put(current, "inbox");
      };
      tx.oncomplete = () => resolve(current);
      tx.onerror = () => reject(tx.error);
      tx.onabort = () => reject(tx.error);
    });
  }
  function storeError() {
    const error = $("editor").open ? $("edit-error") : $("error");
    error.textContent = "无法保存到此浏览器，请检查存储空间或权限后重试。";
    error.hidden = false;
  }
  async function saveDraft() {
    const draft = {
      draft: $("body").value,
      kind: $("todo").checked ? "todo" : "text",
      photo: state.photo,
      draftVersion: crypto.randomUUID(),
    };
    Object.assign(state, draft);
    await save((current) => Object.assign(current, draft));
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
    if (!closed) void saveDraft().catch(storeError);
  }
  function composer() {
    $("body").value = state.draft;
    $("todo").checked = state.kind === "todo";
    showComposerPhoto();
  }
  function showComposerPhoto() {
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
        if (op.type === "update") {
          item.body = op.body;
          item.photo = entry.photo;
          item.attachment_id = op.attachment_id || item.attachment_id;
        }
        if (op.type === "set_completed") item.completed = op.completed;
        if (op.type === "update" || op.type === "set_kind") {
          const kind = op.kind || item.kind;
          const nextKind =
            kind !== "todo" && (item.attachment_id || item.photo)
              ? "image"
              : kind;
          if (nextKind !== item.kind) item.completed = false;
          item.kind = nextKind;
        }
        if (op.type === "delete") item.deleted_at = entry.created;
        if (op.type === "restore") delete item.deleted_at;
        item.pending = op.operation_id;
      }
    }
    return [...items.values()]
      .filter((i) =>
        $("filter").value === "deleted" ? !!i.deleted_at : !i.deleted_at,
      )
      .sort(
        (a, b) =>
          b.created_at.localeCompare(a.created_at) || b.id.localeCompare(a.id),
      );
  }
  function localPicture(item) {
    const key = "local:" + item.id + ":" + item.pending;
    if (!pictures.has(key)) pictures.set(key, URL.createObjectURL(item.photo));
    return pictures.get(key);
  }
  async function picture(id, full = false) {
    const key = id + (full ? "" : "?thumbnail=1");
    if (pictures.has(key)) return pictures.get(key);
    const response = await fetch("/api/app/attachments/" + key, {
      credentials: "same-origin",
      signal: AbortSignal.any([controller.signal, AbortSignal.timeout(30000)]),
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
          filter === "deleted" ||
          (filter === "links" && urls(i.body).length) ||
          (filter === "todo" && i.kind === "todo" && !i.completed) ||
          (filter === "completed" && i.kind === "todo" && i.completed) ||
          (filter === "image" && (i.attachment_id || i.photo))),
    );
    const signature = JSON.stringify([
      state.generation,
      $("error").hidden,
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
      if (entry.photo) {
        const preview = document.createElement("img");
        preview.className = "draft-photo";
        preview.alt = entry.photo.name;
        preview.src = localPicture({
          id: entry.op.item_id,
          photo: entry.photo,
          pending: entry.op.operation_id,
        });
        panel.append(preview);
      }
      if (entry.op.body)
        panel.append(button("复制修改", () => copy(entry.op.body)));
      if (entry.error !== "conflict") {
        panel.append(
          button("重试", () =>
            changePending(entry, (pending) => {
              delete pending.error;
              return pending;
            }),
          ),
        );
      }
      panel.append(
        button("放弃修改", () => {
          if (confirm("放弃这次未同步的修改？"))
            void changePending(entry, () => null);
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
        date.toLocaleTimeString("zh-CN", {
          hour: "2-digit",
          minute: "2-digit",
        }) + (item.pending ? " · 待同步" : ""),
      );
      stamp.dateTime = item.created_at;
      head.append(stamp);
      if (item.body) head.append(button("复制", () => copy(item.body)));
      const menu = document.createElement("details");
      menu.setAttribute("name", "inbox-message-actions");
      menu.append(textElement("summary", "···"));
      menu.firstChild.setAttribute("aria-label", "更多操作");
      const choices = textElement("div", "", "menu");
      const busy = state.outbox.some((e) => e.op.item_id === item.id);
      const edit = button("编辑", () => {
        menu.open = false;
        openEditor(item);
      });
      edit.disabled = busy;
      if (!item.deleted_at) choices.append(edit);
      if (!item.deleted_at) {
        const kind = button(
          item.kind === "todo"
            ? item.attachment_id
              ? "改为图片"
              : "改为文本"
            : "设为待办",
          () =>
            queue("set_kind", item, {
              kind: item.kind === "todo" ? "text" : "todo",
            }),
        );
        kind.disabled = busy;
        choices.append(kind);
      }
      const remove = button("删除", () => {
        if (confirm("删除这条消息？所有设备都会隐藏，可在「已删除」中恢复。"))
          void queue("delete", item);
      });
      remove.disabled = busy;
      if (item.deleted_at) {
        const restore = button("恢复", () => queue("restore", item));
        restore.disabled = busy;
        choices.append(restore);
      } else choices.append(remove);
      menu.append(choices);
      head.append(menu);
      card.append(head);
      if (item.attachment_id || item.photo) {
        const photo = button("正在读取图片…", async () => {
          try {
            $("full-image").src = item.photo
              ? localPicture(item)
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
        if (item.photo) show(localPicture(item));
        else if (item.attachment_id) {
          // Native image loading defers offscreen thumbnails without fetching
          // the entire image list into Blob URLs on every sync.
          show("/api/app/attachments/" + item.attachment_id + "?thumbnail=1");
          photo.querySelector("img").onerror = () => {
            photo.textContent = "图片未加载 · 点击重试";
          };
        }
        card.append(photo);
      }
      const row = textElement("div", "", "body-row");
      if (item.kind === "todo" && !item.deleted_at) {
        const checkbox = document.createElement("input");
        checkbox.type = "checkbox";
        checkbox.checked = item.completed;
        checkbox.disabled = busy;
        checkbox.setAttribute(
          "aria-label",
          item.completed ? "撤销完成" : "完成待办",
        );
        checkbox.onchange = async () => {
          checkbox.disabled = true;
          if (
            !(await queue("set_completed", item, {
              completed: checkbox.checked,
            }))
          ) {
            checkbox.checked = item.completed;
            checkbox.disabled = false;
          }
        };
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
    if (!all.length && !state.generation) {
      if ($("error").hidden) {
        const loading = textElement("p", "正在读取收件箱…", "box-empty");
        loading.setAttribute("role", "status");
        list.append(loading);
      }
      $("more").hidden = true;
      return;
    }
    if (!all.length) {
      const empty = textElement("div", "", "box-empty");
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
  async function queue(type, item, fields = {}, photo = null, draftVersion) {
    if (closed) return false;
    const op = {
      operation_id: crypto.randomUUID(),
      item_id: item?.id || crypto.randomUUID(),
      expected_revision: item?.revision || "0",
      type,
      ...fields,
    };
    const entry = { op, photo, created: new Date().toISOString() };
    let added = false;
    try {
      const current = await save((current) => {
        if (
          item &&
          current.outbox.some((pending) => pending.op.item_id === item.id)
        )
          return false;
        current.outbox.push(entry);
        if (type === "create" && current.draftVersion === draftVersion) {
          current.draft = "";
          current.photo = null;
          delete current.draftVersion;
        }
        added = true;
      });
      state.outbox = current.outbox;
      if (!added) {
        note("这条消息还有修改待同步");
        render();
        return false;
      }
      render();
      channel?.postMessage("queue");
      requestSync();
      return true;
    } catch {
      storeError();
      return false;
    }
  }
  async function updatePending(entry, change) {
    let updated;
    const current = await save((current) => {
      const index = current.outbox.findIndex(
        (pending) => pending.op.operation_id === entry.op.operation_id,
      );
      if (index === -1) return false;
      updated = change(current.outbox[index]);
      if (updated) current.outbox[index] = updated;
      else current.outbox.splice(index, 1);
    });
    state.outbox = current.outbox;
    return updated;
  }
  async function changePending(entry, change) {
    try {
      await updatePending(entry, change);
      render();
      channel?.postMessage("queue");
      requestSync();
    } catch {
      storeError();
    }
  }
  async function saveCache(generation, cursor) {
    await save((current) => {
      if (current.generation !== generation || current.cursor !== cursor)
        return false;
      current.items = state.items;
      current.generation = state.generation;
      current.cursor = state.cursor;
    });
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
          const cursor = state.cursor;
          merge(page.changes.map((c) => c.item));
          state.cursor = page.cursor;
          if (page.cursor !== cursor) await saveCache(state.generation, cursor);
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
    const { generation, cursor } = state;
    state.items = all;
    state.generation = page.generation;
    state.cursor = page.cursor;
    await saveCache(generation, cursor);
  }
  function requestSync() {
    if (syncing) syncRequested = true;
    else void sync();
  }
  async function sync() {
    if (syncing || closed) return;
    syncing = true;
    $("sync-state").textContent = "正在同步…";
    try {
      const current = await load();
      state.outbox = current.outbox;
      state.items = current.items;
      state.generation = current.generation;
      state.cursor = current.cursor;
      render();
      await catchUp();
      let sent = false;
      for (let entry of [...state.outbox]) {
        if (disposed) return;
        if (entry.error) continue;
        try {
          if (entry.photo && !entry.op.attachment_id) {
            const attachment = await api("attachments", {
              method: "POST",
              body: entry.photo,
            });
            // Multiple tabs may upload concurrently. All retries must submit
            // the same attachment ID for this operation's server receipt.
            entry = await updatePending(entry, (pending) => {
              pending.op.attachment_id ||= attachment.id;
              return pending;
            });
            if (!entry || entry.error) continue;
          }
          const item = await api("operations", json(entry.op));
          sent = true;
          merge([item]);
          await updatePending(entry, () => null);
        } catch (e) {
          if (
            e.status === 400 ||
            e.status === 409 ||
            e.status === 404 ||
            e.status === 413
          ) {
            await updatePending(entry, (pending) => {
              pending.error = e.code;
              return pending;
            });
          } else throw e;
        }
      }
      if (sent) await catchUp();
      $("error").hidden = true;
      $("sync-state").textContent = state.outbox.length
        ? "有修改待处理"
        : "已同步";
    } catch (e) {
      if (disposed) return;
      $("sync-state").textContent = "等待同步";
      $("error").textContent = reason(e);
      $("error").hidden = false;
      if (e.status === 401 || e.code === "device_revoked") lock(reason(e));
    } finally {
      syncing = false;
      if (!disposed) render();
      if (syncRequested) {
        syncRequested = false;
        void sync();
      }
    }
  }
  function lock(message) {
    closed = true;
    $("send").disabled = true;
    events?.close();
    clearInterval(timer);
    $("error").textContent = message;
    $("error").hidden = false;
  }
  async function start() {
    if (starting || disposed) return;
    starting = true;
    $("send").disabled = true;
    db?.close();
    try {
      db = await openDB("console");
      state = await load();
      if (disposed) {
        db.close();
        return;
      }
    } catch {
      throw { code: "storage_unavailable" };
    } finally {
      starting = false;
    }
    if (disposed) return;
    closed = false;
    lastRender = "";
    composer();
    render();
    $("send").disabled = false;

    clearInterval(timer);
    events?.close();
    events = new EventSource("/api/app/events");
    events.addEventListener("sync", (event) => {
      try {
        const watermark = JSON.parse(event.data);
        if (
          watermark.generation !== state.generation ||
          watermark.cursor !== state.cursor ||
          state.outbox.some((e) => !e.error)
        )
          void sync();
      } catch {
        void sync();
      }
    });
    timer = setInterval(() => {
      if (!document.hidden) void sync();
    }, 30000);
    void sync();
  }
  $("composer").onsubmit = async (event) => {
    event.preventDefault();
    if ($("send").disabled || closed) return;
    const body = $("body").value.trim();
    if (!body && !state.photo) return;
    if (new TextEncoder().encode(body).length > 16000) {
      note("正文过长，请缩短后发送");
      return;
    }
    for (const control of $("composer").elements) control.disabled = true;
    try {
      if (
        await queue(
          "create",
          null,
          {
            kind: $("todo").checked ? "todo" : state.photo ? "image" : "text",
            body,
          },
          state.photo,
          state.draftVersion,
        )
      ) {
        state.draft = "";
        state.photo = null;
        delete state.draftVersion;
        composer();
        $("search").value = "";
        $("filter").value = "all";
        limit = 100;
        render();
        window.scrollTo({ top: 0, behavior: "smooth" });
      }
    } finally {
      for (const control of $("composer").elements) control.disabled = closed;
    }
  };
  $("body").oninput = draftChanged;
  $("todo").onchange = draftChanged;
  function bindComposer(form, body, input, choose, select) {
    $(choose).onclick = () => $(input).click();
    $(input).onchange = () => {
      select($(input).files[0]);
      $(input).value = "";
    };
    $(body).onkeydown = (event) => {
      if (
        (event.metaKey || event.ctrlKey) &&
        event.key === "Enter" &&
        !event.isComposing
      ) {
        event.preventDefault();
        $(form).requestSubmit();
      }
    };
    $(body).addEventListener("paste", (event) => {
      const file = event.clipboardData?.files[0];
      if (file) {
        event.preventDefault();
        select(file);
      }
    });
    $(form).addEventListener("dragover", (event) => {
      if (event.dataTransfer.types.includes("Files")) event.preventDefault();
    });
    $(form).addEventListener("drop", (event) => {
      const file = event.dataTransfer?.files[0];
      if (file) {
        event.preventDefault();
        select(file);
      }
    });
  }
  function photoError(file) {
    if (file.size > 10 * 1024 * 1024) return "图片不能超过 10 MB";
    if (!["image/png", "image/jpeg", "image/gif"].includes(file.type))
      return "请选择 JPEG、PNG 或 GIF 图片";
    return "";
  }
  function selectPhoto(file) {
    if (closed || $("send").disabled || !file) return;
    const error = photoError(file);
    if (error) return note(error);
    state.photo = file;
    showComposerPhoto();
    draftChanged();
  }
  bindComposer("composer", "body", "photo", "choose-photo", selectPhoto);
  $("remove-photo").onclick = () => {
    if (closed || $("send").disabled) return;
    state.photo = null;
    showComposerPhoto();
    draftChanged();
  };
  function toggleSearch(open) {
    $("search-field").hidden = !open;
    $("toggle-search").setAttribute("aria-expanded", String(open));
    $("toggle-search").setAttribute(
      "aria-label",
      open ? "关闭搜索" : "搜索消息",
    );
    $("toggle-search").title = open ? "关闭搜索" : "搜索消息";
    $("workspace").querySelector("header").classList.toggle("searching", open);
    if (open) {
      $("search").focus();
    } else {
      $("search").value = "";
      limit = 100;
      render();
      $("toggle-search").focus();
    }
  }
  $("toggle-search").onclick = () => toggleSearch($("search-field").hidden);
  $("search").onkeydown = (event) => {
    if (event.key === "Escape") {
      event.preventDefault();
      toggleSearch(false);
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
  $("refresh").onclick = () => {
    if (closed) void start().catch((e) => lock(reason(e)));
    else void sync();
  };
  function editError(message) {
    $("edit-error").textContent = message;
    $("edit-error").hidden = !message;
  }
  function showEditPhoto() {
    const existing = editing.attachment_id;
    const preview = $("edit-preview");
    if (preview.dataset.local) URL.revokeObjectURL(preview.dataset.local);
    delete preview.dataset.local;
    if (editPhoto) preview.dataset.local = URL.createObjectURL(editPhoto);
    const src =
      preview.dataset.local ||
      (existing ? "/api/app/attachments/" + existing + "?thumbnail=1" : "");
    $("edit-attachment").hidden = !src;
    if (src) preview.src = src;
    else preview.removeAttribute("src");
    $("edit-remove-photo").hidden = !editPhoto;
    $("edit-attachment-name").textContent = editPhoto?.name || "";
    $("edit-choose-photo").textContent = src ? "更换图片" : "添加图片";
  }
  function openEditor(item) {
    editing = item;
    editPhoto = null;
    $("edit-body").value = item.body;
    $("edit-todo").checked = item.kind === "todo";
    showEditPhoto();
    editError("");
    $("editor").showModal();
  }
  bindComposer(
    "edit-form",
    "edit-body",
    "edit-photo",
    "edit-choose-photo",
    (file) => {
      if (!file || $("save-edit").disabled) return;
      const error = photoError(file);
      editError(error);
      if (error) return;
      editPhoto = file;
      showEditPhoto();
    },
  );
  $("edit-remove-photo").onclick = () => {
    editPhoto = null;
    showEditPhoto();
    editError("");
  };
  $("edit-form").onsubmit = async (event) => {
    event.preventDefault();
    if (!editing || $("save-edit").disabled) return;
    const body = $("edit-body").value.trim();
    const kind = $("edit-todo").checked ? "todo" : "text";
    if (new TextEncoder().encode(body).length > 16000)
      return editError("正文过长，请缩短后保存");
    if (!body && !editing.attachment_id && !editPhoto)
      return editError("请填写正文或添加图片");
    if (
      body === editing.body &&
      (kind === "todo") === (editing.kind === "todo") &&
      !editPhoto
    ) {
      $("editor").close();
      return;
    }
    editError("");
    for (const control of $("edit-form").elements) control.disabled = true;
    try {
      if (await queue("update", editing, { body, kind }, editPhoto))
        $("editor").close();
      else if ($("edit-error").hidden)
        editError("这条消息还有修改待同步，请稍后再试。");
    } finally {
      for (const control of $("edit-form").elements) control.disabled = false;
    }
  };
  $("editor").oncancel = (event) => {
    if ($("save-edit").disabled) event.preventDefault();
  };
  $("editor").onclose = () => {
    editPhoto = null;
    showEditPhoto();
    editing = null;
  };
  $("cancel-edit").onclick = () => $("editor").close();
  $("close-image").onclick = () => $("image-view").close();
  const online = () => {
    if (!closed) void sync();
  };
  const visible = () => {
    if (!document.hidden && !closed) void sync();
  };
  const beforeLogout = (event) => {
    if (syncing) {
      note("正在同步，请稍后退出");
      event.preventDefault();
    }
  };
  const stop = () => {
    disposed = true;
    closed = true;
    controller.abort();
    events?.close();
    clearInterval(timer);
    clearTimeout(toastTimer);
    channel?.close();
    const preview = $("edit-preview");
    if (preview.dataset.local) URL.revokeObjectURL(preview.dataset.local);
    db?.close();
    for (const url of pictures.values()) URL.revokeObjectURL(url);
    pictures.clear();
    window.removeEventListener("online", online);
    document.removeEventListener("visibilitychange", visible);
    window.removeEventListener("orbit:logout", stop);
    window.removeEventListener("orbit:beforelogout", beforeLogout);
  };
  if (channel)
    channel.onmessage = (event) => {
      if (event.data === "logout") {
        stop();
        window.dispatchEvent(new Event("orbit:unauthorized"));
      } else if (event.data === "queue") requestSync();
    };
  window.addEventListener("online", online);
  document.addEventListener("visibilitychange", visible);
  window.addEventListener("orbit:logout", stop);
  window.addEventListener("orbit:beforelogout", beforeLogout);
  void start().catch((e) => {
    if (!disposed) lock(reason(e));
  });
  // React releases all page-owned resources on navigation.
  return stop;
}

// Loaded only when signing out, including when the inbox page is not mounted.
export async function confirmBoxLogout() {
  const db = await openLogoutCache();
  try {
    const value = await new Promise((resolve, reject) => {
      const request = db.transaction("state").objectStore("state").get("inbox");
      request.onsuccess = () => resolve(request.result);
      request.onerror = () => reject(request.error);
    });
    return (
      !value ||
      !(value.draft || value.photo || value.outbox?.length) ||
      confirm(
        "此浏览器仍有草稿或未同步的修改。退出会清除这些本机内容，确定退出？",
      )
    );
  } finally {
    db.close();
  }
}
export async function clearBoxCache() {
  if (typeof BroadcastChannel === "function") {
    const channel = new BroadcastChannel("orbit-workbench-box");
    channel.postMessage("logout");
    channel.close();
  }
  const db = await openLogoutCache();
  try {
    await new Promise((resolve, reject) => {
      const tx = db.transaction("state", "readwrite");
      tx.objectStore("state").clear();
      tx.oncomplete = resolve;
      tx.onerror = () => reject(tx.error);
      tx.onabort = () => reject(tx.error);
    });
  } finally {
    db.close();
  }
}
function openLogoutCache() {
  return new Promise((resolve, reject) => {
    const request = indexedDB.open("orbit-workbench-box-console", 1);
    request.onupgradeneeded = () => request.result.createObjectStore("state");
    request.onsuccess = () => resolve(request.result);
    request.onerror = () => reject(request.error);
  });
}
