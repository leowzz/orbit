import { useEffect, useRef, useState, type PointerEvent } from "react";
import { createPortal } from "react-dom";
import { isTauri } from "@tauri-apps/api/core";
import { getCurrentWebview } from "@tauri-apps/api/webview";
import { open } from "@tauri-apps/plugin-dialog";
import {
  AppWindow,
  Globe,
  Plus,
  Trash2,
  Pencil,
  ImagePlus,
  X,
  RefreshCw,
} from "lucide-react";
import { call } from "./bridge";
import type { Shortcut } from "./types";
import { mergeShortcuts, moveShortcut } from "./shortcut-model";

type Recommendations = {
  supported: boolean;
  items: { app: Shortcut; lastUsed: number }[];
};
function AppIcon({ item }: { item: Shortcut }) {
  return item.icon ? (
    <img src={`data:image/png;base64,${item.icon}`} alt="" draggable={false} />
  ) : item.kind === "url" ? (
    <Globe aria-hidden="true" />
  ) : (
    <AppWindow aria-hidden="true" />
  );
}
export function Shortcuts({
  items,
  onChange,
  showTitles,
  onShowTitles,
}: {
  items: Shortcut[];
  showTitles: boolean;
  onShowTitles: (value: boolean) => void;
  onChange: (items: Shortcut[]) => void;
}) {
  const current = useRef(items);
  current.current = items;
  const update = useRef(onChange);
  update.current = onChange;
  const [error, setError] = useState("");
  const [busy, setBusy] = useState(false);
  const [externalDrag, setExternalDrag] = useState(false);
  const [recommendations, setRecommendations] =
    useState<Recommendations | null>(null);
  const [recommendationError, setRecommendationError] = useState("");
  const [loading, setLoading] = useState(true);
  const [editing, setEditing] = useState<Shortcut | null>(null);
  const editor = useRef(editing);
  editor.current = editing;
  const [menu, setMenu] = useState<{ id: string; x: number; y: number } | null>(
    null,
  );
  const menuRef = useRef<HTMLDivElement>(null);
  const [drag, setDrag] = useState<{ id: string; over: string } | null>(null);
  const gesture = useRef<{
    id: string;
    x: number;
    y: number;
    active: boolean;
    over: string;
  } | null>(null);
  const lastDrag = useRef(0);
  const [opening, setOpening] = useState<string | null>(null);
  const dock = useRef<HTMLDivElement>(null);
  const alive = useRef(true);
  const refreshBusy = useRef(false);

  async function refresh() {
    if (refreshBusy.current) return;
    refreshBusy.current = true;
    try {
      const result = await call<Recommendations>("shortcut_recommendations", {
        excluded: current.current
          .filter((i) => i.kind === "app")
          .map((i) => i.target),
      });
      if (alive.current) {
        setRecommendations(result);
        setRecommendationError("");
      }
    } catch (e) {
      if (alive.current) setRecommendationError(String(e));
    } finally {
      refreshBusy.current = false;
      if (alive.current) setLoading(false);
    }
  }
  useEffect(() => {
    alive.current = true;
    void refresh();
    const timer = setInterval(() => void refresh(), 5000);
    return () => {
      alive.current = false;
      clearInterval(timer);
    };
  }, []);
  useEffect(() => {
    void refresh();
  }, [items]);
  useEffect(() => {
    if (!menu) return;
    menuRef.current?.querySelector<HTMLButtonElement>("button")?.focus();
    const dismiss = (e: globalThis.PointerEvent) => {
      if (!menuRef.current?.contains(e.target as Node)) setMenu(null);
    };
    const key = (e: KeyboardEvent) => {
      if (e.key === "Escape") setMenu(null);
    };
    const close = () => setMenu(null);
    document.addEventListener("pointerdown", dismiss);
    document.addEventListener("keydown", key);
    window.addEventListener("resize", close);
    return () => {
      document.removeEventListener("pointerdown", dismiss);
      document.removeEventListener("keydown", key);
      window.removeEventListener("resize", close);
    };
  }, [menu]);
  function add(entries: Shortcut[]) {
    update.current(mergeShortcuts(current.current, entries));
  }
  function patchIcon(id: string, icon: string) {
    if (editor.current?.id === id)
      setEditing((i) => (i && i.id === id ? { ...i, icon } : i));
    else
      update.current(
        current.current.map((i) => (i.id === id ? { ...i, icon } : i)),
      );
  }
  async function importPaths(paths: string[], iconId?: string) {
    setBusy(true);
    setError("");
    try {
      if (iconId && paths.length === 1 && !/\.(app|exe|lnk)$/i.test(paths[0])) {
        patchIcon(
          iconId,
          await call<string>("shortcut_icon", { path: paths[0] }),
        );
      } else {
        const imported: Shortcut[] = [];
        for (const path of paths)
          imported.push(await call<Shortcut>("shortcut_import", { path }));
        add(imported);
      }
    } catch (e) {
      setError(String(e));
    } finally {
      setBusy(false);
    }
  }
  const importer = useRef(importPaths);
  importer.current = importPaths;
  useEffect(() => {
    if (!isTauri()) return;
    let disposed = false;
    let stop: (() => void) | undefined;
    void getCurrentWebview()
      .onDragDropEvent(({ payload }) => {
        setExternalDrag(payload.type === "enter" || payload.type === "over");
        if (payload.type === "drop") {
          const el = document.elementFromPoint(
            payload.position.x / window.devicePixelRatio,
            payload.position.y / window.devicePixelRatio,
          );
          const id = el?.closest<HTMLElement>("[data-shortcut-icon]")?.dataset
            .shortcutIcon;
          void importer.current(payload.paths, id);
        }
      })
      .then((fn) => {
        if (disposed) fn();
        else stop = fn;
      })
      .catch((e) => setError(String(e)));
    return () => {
      disposed = true;
      stop?.();
    };
  }, []);
  async function chooseApp() {
    try {
      const paths = await open({
        multiple: true,
        filters: [{ name: "应用", extensions: ["app", "exe", "lnk"] }],
      });
      if (paths) await importPaths(Array.isArray(paths) ? paths : [paths]);
    } catch (e) {
      setError(String(e));
    }
  }
  async function chooseIcon(id: string) {
    try {
      const path = await open({
        multiple: false,
        filters: [
          { name: "图片", extensions: ["png", "jpg", "jpeg", "icns", "ico"] },
        ],
      });
      if (typeof path === "string") await importPaths([path], id);
    } catch (e) {
      setError(String(e));
    }
  }
  async function launch(item: Shortcut) {
    if (opening || performance.now() - lastDrag.current < 400) return;
    setOpening(item.id);
    setError("");
    try {
      await call("shortcut_preview", { item });
    } catch (e) {
      setError(String(e));
    } finally {
      setOpening(null);
    }
  }
  function startDrag(e: PointerEvent<HTMLButtonElement>, id: string) {
    if (e.button !== 0) return;
    gesture.current = {
      id,
      x: e.clientX,
      y: e.clientY,
      active: false,
      over: id,
    };
    e.currentTarget.setPointerCapture(e.pointerId);
  }
  function moveDrag(e: PointerEvent<HTMLButtonElement>) {
    const g = gesture.current;
    if (!g) return;
    if (!g.active && Math.hypot(e.clientX - g.x, e.clientY - g.y) < 7) return;
    g.active = true;
    const bounds = dock.current?.getBoundingClientRect();
    if (bounds && dock.current) {
      if (e.clientX > bounds.right - 28) dock.current.scrollLeft += 16;
      if (e.clientX < bounds.left + 28) dock.current.scrollLeft -= 16;
    }
    const target = document
      .elementFromPoint(e.clientX, e.clientY)
      ?.closest<HTMLElement>("[data-dock-id]");
    if (target?.dataset.dockId) g.over = target.dataset.dockId;
    setDrag({ id: g.id, over: g.over });
  }
  function endDrag(e: PointerEvent<HTMLButtonElement>, cancel = false) {
    const g = gesture.current;
    gesture.current = null;
    setDrag(null);
    if (e.currentTarget.hasPointerCapture(e.pointerId))
      e.currentTarget.releasePointerCapture(e.pointerId);
    if (!g?.active) return;
    lastDrag.current = performance.now();
    if (!cancel) update.current(moveShortcut(current.current, g.id, g.over));
  }
  function context(id: string, x: number, y: number) {
    setMenu({
      id,
      x: Math.max(8, Math.min(x, window.innerWidth - 178)),
      y: Math.max(8, Math.min(y, window.innerHeight - 104)),
    });
  }
  const available =
    recommendations?.items.filter(
      ({ app }) =>
        !items.some((i) => i.kind === app.kind && i.target === app.target),
    ) ?? [];
  return (
    <section className="shortcuts-section">
      <div className="shortcut-heading">
        <h2>底栏预览</h2>
        <div className="shortcut-toolbar">
          <button
            type="button"
            disabled={busy}
            onClick={() => void chooseApp()}
          >
            <Plus size={15} />
            添加应用
          </button>
          <button
            type="button"
            disabled={busy || items.length >= 24}
            onClick={() =>
              setEditing({
                id: crypto.randomUUID(),
                title: "",
                kind: "url",
                target: "",
                icon: "",
              })
            }
          >
            <Globe size={15} />
            添加网址
          </button>
        </div>
      </div>
      <p className="shortcut-help">
        拖拽调整顺序 · 双击打开 · 右键编辑或删除，也可直接拖入应用
      </p>
      {error && (
        <div role="alert" className="alert error">
          {error}
        </div>
      )}
      <div
        ref={dock}
        className={`shortcut-dock ${externalDrag ? "dragging" : ""}`}
        aria-label="工作台底栏预览"
      >
        <div className="shortcut-dock-row">
          {items.length === 0 && (
            <span className="shortcut-empty">
              从下方推荐中添加，或将应用拖到这里
            </span>
          )}
          {items.map((item) => (
            <button
              type="button"
              key={item.id}
              data-dock-id={item.id}
              data-shortcut-icon={item.id}
              className={`dock-item ${showTitles ? "with-title" : ""} ${drag?.id === item.id ? "is-dragging" : ""} ${drag?.over === item.id && drag.id !== item.id ? "drop-target" : ""}`}
              title={`${item.title} · 双击打开，右键编辑`}
              aria-label={item.title}
              aria-busy={opening === item.id}
              onDoubleClick={() => void launch(item)}
              onContextMenu={(e) => {
                e.preventDefault();
                context(item.id, e.clientX, e.clientY);
              }}
              onPointerDown={(e) => startDrag(e, item.id)}
              onPointerMove={moveDrag}
              onPointerUp={(e) => endDrag(e)}
              onPointerCancel={(e) => endDrag(e, true)}
              onKeyDown={(e) => {
                if (e.key === "Enter") {
                  e.preventDefault();
                  void launch(item);
                }
                if (
                  (e.shiftKey && e.key === "F10") ||
                  e.key === "ContextMenu"
                ) {
                  e.preventDefault();
                  const r = e.currentTarget.getBoundingClientRect();
                  context(item.id, r.left, r.bottom);
                }
                if (e.altKey && ["ArrowLeft", "ArrowRight"].includes(e.key)) {
                  e.preventDefault();
                  const index = items.findIndex((i) => i.id === item.id);
                  const other = items[index + (e.key === "ArrowLeft" ? -1 : 1)];
                  if (other)
                    update.current(
                      moveShortcut(current.current, item.id, other.id),
                    );
                }
              }}
            >
              <AppIcon item={item} />
              {showTitles && <span>{item.title}</span>}
            </button>
          ))}
        </div>
      </div>
      <label className="toggle-row shortcut-title-toggle">
        <span>
          <strong>在工作台显示标题</strong>
        </span>
        <input
          type="checkbox"
          role="switch"
          checked={showTitles}
          onChange={(e) => onShowTitles(e.target.checked)}
        />
      </label>
      <div className="shortcut-heading">
        <h2>最近使用 · 点击添加</h2>
        <button
          type="button"
          className="icon-button"
          aria-label="刷新最近应用"
          disabled={loading}
          onClick={() => void refresh()}
        >
          <RefreshCw size={16} />
        </button>
      </div>
      <p className="shortcut-help">
        记录 Agent 运行期间打开或切换使用的应用，仅保存在这台电脑上。
      </p>
      {recommendationError ? (
        <div className="alert error" role="alert">
          {recommendationError}
          <button type="button" onClick={() => void refresh()}>
            重试
          </button>
        </div>
      ) : loading ? (
        <p className="shortcut-empty">正在读取最近应用…</p>
      ) : recommendations?.supported === false ? (
        <p className="shortcut-empty">
          最近应用推荐目前支持 macOS，仍可手动添加应用。
        </p>
      ) : available.length === 0 ? (
        <p className="shortcut-empty">
          暂无可添加的推荐，打开或切换到其他应用后会出现在这里。
        </p>
      ) : (
        <div className="shortcut-recommendations">
          {available.map(({ app }) => (
            <button
              type="button"
              className="recommendation"
              key={app.target}
              disabled={busy || items.length >= 24}
              title={`添加 ${app.title}`}
              onClick={() => {
                try {
                  add([{ ...app, id: crypto.randomUUID() }]);
                  setError("");
                } catch (e) {
                  setError(String(e));
                }
              }}
            >
              <AppIcon item={app} />
              <span>{app.title}</span>
              <Plus size={14} />
            </button>
          ))}
        </div>
      )}
      <p className="shortcut-help">
        保存并应用后同步到工作台。仅已添加并保存的入口允许被工作台打开。
      </p>
      {menu &&
        createPortal(
          <div
            ref={menuRef}
            className="shortcut-menu"
            role="menu"
            aria-label="入口操作"
            style={{ left: menu.x, top: menu.y }}
            onKeyDown={(e) => {
              if (["ArrowDown", "ArrowUp"].includes(e.key)) {
                e.preventDefault();
                const buttons = [
                  ...menuRef.current!.querySelectorAll("button"),
                ];
                const index = buttons.indexOf(
                  document.activeElement as HTMLButtonElement,
                );
                buttons[
                  (index + (e.key === "ArrowDown" ? 1 : buttons.length - 1)) %
                    buttons.length
                ].focus();
              }
            }}
          >
            <button
              type="button"
              role="menuitem"
              onClick={() => {
                const item = items.find((i) => i.id === menu.id);
                if (item) setEditing({ ...item });
                setMenu(null);
              }}
            >
              <Pencil size={16} />
              编辑
            </button>
            <button
              type="button"
              role="menuitem"
              onClick={() => {
                update.current(current.current.filter((i) => i.id !== menu.id));
                setMenu(null);
              }}
            >
              <Trash2 size={16} />
              删除
            </button>
          </div>,
          document.body,
        )}
      {editing && (
        <ShortcutEditor
          item={editing}
          onChange={setEditing}
          busy={busy}
          onIcon={() => void chooseIcon(editing.id)}
          onClose={() => setEditing(null)}
          onApply={(item) => {
            if (items.some((i) => i.id === item.id))
              onChange(items.map((i) => (i.id === item.id ? item : i)));
            else add([item]);
            setEditing(null);
          }}
        />
      )}
    </section>
  );
}

function ShortcutEditor({
  item,
  onChange,
  onIcon,
  onClose,
  onApply,
  busy,
}: {
  item: Shortcut;
  onChange: (item: Shortcut) => void;
  onIcon: () => void;
  onClose: () => void;
  onApply: (item: Shortcut) => void;
  busy: boolean;
}) {
  const ref = useRef<HTMLDialogElement>(null);
  const [error, setError] = useState("");
  useEffect(() => {
    ref.current?.showModal();
  }, []);
  async function choose() {
    try {
      const path = await open({
        multiple: false,
        filters: [{ name: "应用", extensions: ["app", "exe", "lnk"] }],
      });
      if (typeof path === "string") {
        const next = await call<Shortcut>("shortcut_import", { path });
        onChange({ ...next, id: item.id });
      }
    } catch (e) {
      setError(String(e));
    }
  }
  return createPortal(
    <dialog
      ref={ref}
      className="shortcut-dialog"
      onCancel={onClose}
      onClick={(e) => {
        if (e.target === e.currentTarget) {
          const r = e.currentTarget.getBoundingClientRect();
          if (
            e.clientX < r.left ||
            e.clientX > r.right ||
            e.clientY < r.top ||
            e.clientY > r.bottom
          )
            onClose();
        }
      }}
      aria-labelledby="shortcut-editor-title"
    >
      <form
        onSubmit={(e) => {
          e.preventDefault();
          const next = {
            ...item,
            title: item.title.trim(),
            target: item.target.trim(),
          };
          if (
            !next.title ||
            new TextEncoder().encode(next.title).length > 160
          ) {
            setError("请填写名称，过长时请缩短后重试");
            return;
          }
          if (next.kind === "url") {
            try {
              const url = new URL(next.target);
              if (
                !["http:", "https:"].includes(url.protocol) ||
                !url.hostname ||
                url.username ||
                url.password
              )
                throw Error();
            } catch {
              setError("请填写有效的 http/https 网址");
              return;
            }
          }
          try {
            onApply(next);
          } catch (e) {
            setError(String(e));
          }
        }}
      >
        <div className="shortcut-heading">
          <h2 id="shortcut-editor-title">
            {item.kind === "url" ? "网址配置" : "应用配置"}
          </h2>
          <button type="button" aria-label="关闭配置" onClick={onClose}>
            <X size={17} />
          </button>
        </div>
        {error && (
          <div className="alert error" role="alert">
            {error}
          </div>
        )}
        <div className="shortcut-editor-fields">
          <button
            type="button"
            className="shortcut-icon"
            data-shortcut-icon={item.id}
            aria-label="更换图标"
            title="选择图片，或将图片拖到这里"
            onClick={onIcon}
            disabled={busy}
          >
            <AppIcon item={item} />
            <ImagePlus size={12} className="icon-edit" />
          </button>
          <label className="field">
            <span>名称</span>
            <input
              autoFocus
              value={item.title}
              maxLength={80}
              onChange={(e) => onChange({ ...item, title: e.target.value })}
            />
          </label>
        </div>
        {item.kind === "url" ? (
          <label className="field">
            <span>网址</span>
            <input
              value={item.target}
              placeholder="https://example.com"
              onChange={(e) => onChange({ ...item, target: e.target.value })}
            />
          </label>
        ) : (
          <div className="shortcut-path-row">
            <span className="shortcut-path">{item.target}</span>
            <button type="button" onClick={() => void choose()}>
              更换应用
            </button>
          </div>
        )}
        <div className="shortcut-dialog-actions">
          <button type="button" onClick={onClose}>
            取消
          </button>
          <button type="submit" className="primary" disabled={busy}>
            完成
          </button>
        </div>
      </form>
    </dialog>,
    document.body,
  );
}
