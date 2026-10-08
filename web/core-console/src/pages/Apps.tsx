import { useCallback, useEffect, useRef, useState } from "react";
import { Link } from "react-router-dom";
import { QRCodeSVG } from "qrcode.react";
import {
  Plus,
  RefreshCw,
  Smartphone,
  Copy,
  X,
  Image as ImageIcon,
} from "lucide-react";
import { api, APIError, date } from "../api";
import type { AppDevice } from "../api";
import { Badge, Empty, PageHead } from "../components";

type Item = {
  id: string;
  kind: string;
  body: string;
  completed: boolean;
  revision: string;
  attachment_id?: string;
  updated_at: string;
  deleted_at?: string;
};
type Items = { items: Item[]; next: string };
type Operation = Record<string, unknown>;
function message(e: unknown): string {
  if (e instanceof APIError) {
    if (e.status === 409) return "内容已被其他设备修改，请刷新列表后重新编辑。";
    if (e.status === 413) return "图片过大，请选择不超过 10 MB 的图片。";
    if (e.status === 400)
      return "请检查填写内容；图片需为 JPEG、PNG 或 GIF，不超过 10 MB、2000 万像素。";
    if (e.status === 404) return "内容已不可用，请刷新后重试。";
  }
  return "操作未完成，请检查连接后重试。";
}
export default function Apps() {
  const [tab, setTab] = useState("devices"),
    [devices, setDevices] = useState<AppDevice[] | null>(null),
    [enabled, setEnabled] = useState(true),
    [error, setError] = useState("");
  const [items, setItems] = useState<Item[] | null>(null),
    [next, setNext] = useState(""),
    [kind, setKind] = useState(""),
    [deleted, setDeleted] = useState(false),
    [query, setQuery] = useState(""),
    [filter, setFilter] = useState("");
  const [busy, setBusy] = useState(false),
    [create, setCreate] = useState(false),
    [secret, setSecret] = useState<{ id: string; token: string } | null>(null),
    [editor, setEditor] = useState<{ item?: Item } | null>(null);
  const [deviceError, setDeviceError] = useState("");
  const itemsRequest = useRef(0);
  const refreshDevices = useCallback(async () => {
    const d = await api<{ enabled: boolean; devices: AppDevice[] }>(
      "/app/devices",
    );
    setEnabled(d.enabled);
    setDevices(d.devices ?? []);
  }, []);
  const refreshItems = useCallback(
    async (after = "") => {
      const request = ++itemsRequest.current;
      const d = await api<Items>(
        "/app/items?" +
          new URLSearchParams({
            kind,
            q: filter,
            after,
            deleted: String(deleted),
          }),
      );
      if (request !== itemsRequest.current) return;
      setItems((old) => (after ? [...(old ?? []), ...d.items] : d.items));
      setNext(d.next);
    },
    [kind, filter, deleted],
  );
  useEffect(() => {
    let active = true;
    let timer: ReturnType<typeof setTimeout>;
    async function poll() {
      try {
        await refreshDevices();
        if (active) setDeviceError("");
      } catch (e) {
        if (active) setDeviceError(message(e));
      }
      if (active) timer = setTimeout(poll, 5000);
    }
    void poll();
    return () => {
      active = false;
      clearTimeout(timer);
    };
  }, [refreshDevices]);
  useEffect(() => {
    if (tab !== "inbox" || !enabled) return;
    setItems(null);
    setNext("");
    setError("");
    void refreshItems().catch((e) => setError(message(e)));
  }, [tab, enabled, refreshItems]);
  async function refresh() {
    setBusy(true);
    setError("");
    try {
      await refreshDevices();
      if (tab === "inbox") await refreshItems();
    } catch (e) {
      setError(message(e));
    } finally {
      setBusy(false);
    }
  }
  async function deviceAction(d: AppDevice, action: string) {
    if (
      !window.confirm(
        action === "revoke"
          ? `撤销「${d.label}」的访问？此设备将立即断开，已有收件箱内容会保留。`
          : `为「${d.label}」生成新令牌？旧令牌将立即失效。`,
      )
    )
      return;
    setBusy(true);
    setError("");
    try {
      const result = await api<{ id: string; token: string }>(
        `/app/devices/${d.id}/${action}`,
        { method: "POST" },
      );
      if (result.token) setSecret(result);
      await refreshDevices();
    } catch (e) {
      setError(message(e));
    } finally {
      setBusy(false);
    }
  }
  async function changeItem(item: Item, type: string) {
    if (
      type === "delete" &&
      !window.confirm("删除这条内容？所有设备都会隐藏，可在「已删除」中恢复。")
    )
      return;
    setBusy(true);
    setError("");
    try {
      await api("/app/operations", {
        method: "POST",
        body: JSON.stringify({
          operation_id: crypto.randomUUID(),
          item_id: item.id,
          type,
          expected_revision: item.revision,
          ...(type === "set_completed" ? { completed: !item.completed } : {}),
        }),
      });
      await refreshItems();
    } catch (e) {
      setError(message(e));
    } finally {
      setBusy(false);
    }
  }
  return (
    <>
      <PageHead
        title="App 与收件箱"
        description="连接你的设备，在管理台与 App 之间共享内容。"
        action={
          <button
            className="button primary"
            disabled={!enabled || busy}
            onClick={() =>
              tab === "devices" ? setCreate(true) : setEditor({})
            }
          >
            <Plus size={16} />
            {tab === "devices" ? "添加设备" : "新增内容"}
          </button>
        }
      />
      <div className="app-toolbar">
        <div className="app-tabs" role="tablist" aria-label="App 管理">
          <button
            role="tab"
            aria-selected={tab === "devices"}
            onClick={() => {
              setTab("devices");
              setError("");
            }}
          >
            设备
          </button>
          <button
            role="tab"
            aria-selected={tab === "inbox"}
            onClick={() => {
              setTab("inbox");
              setError("");
            }}
          >
            收件箱
          </button>
        </div>
        <button className="button secondary" disabled={busy} onClick={refresh}>
          <RefreshCw size={15} />
          刷新
        </button>
      </div>
      {(error || deviceError) && (
        <div className="banner error" role="alert">
          {error || deviceError}
          <button onClick={refresh}>重试</button>
        </div>
      )}
      {!enabled ? (
        <Empty title="App 服务尚未启用">
          请先在 Core 部署配置中启用 App 服务，再回来添加设备。
        </Empty>
      ) : tab === "devices" ? (
        <>
          <p className="app-note">
            连接和访问记录仅反映本次服务运行。最近同步请求表示已返回数据，不代表设备已保存完成。
          </p>
          {devices === null ? (
            <p role="status">正在加载设备…</p>
          ) : devices.length === 0 ? (
            <Empty title="连接第一台设备">
              添加设备后，将生成的令牌填入 App 即可开始同步。
            </Empty>
          ) : (
            <div className="app-device-grid">
              {devices.map((d) => (
                <article className="app-device" key={d.id}>
                  <div className="app-card-head">
                    <Smartphone size={21} />
                    <div>
                      <h2>{d.label || d.id}</h2>
                      <span>{d.platform}</span>
                    </div>
                    <Badge
                      tone={
                        d.revoked
                          ? "warn"
                          : d.activity.connections > 0
                            ? "good"
                            : "neutral"
                      }
                    >
                      {d.revoked
                        ? "已撤销"
                        : d.activity.connections > 0
                          ? "已连接"
                          : "未连接"}
                    </Badge>
                  </div>
                  <dl>
                    <div>
                      <dt>最近访问</dt>
                      <dd>{date(d.activity.last_seen)}</dd>
                    </div>
                    <div>
                      <dt>同步请求</dt>
                      <dd>{date(d.activity.last_sync)}</dd>
                    </div>
                  </dl>
                  <details>
                    <summary>状态摘要设置</summary>
                    <p>配置这台设备显示的用量与会话来源。</p>
                    <code>{d.id}</code>
                    <Link to={`/routes?node=${encodeURIComponent(d.id)}`}>
                      配置转发规则
                    </Link>
                  </details>
                  <div className="app-actions">
                    <button
                      className="button secondary"
                      disabled={busy}
                      onClick={() => deviceAction(d, "rotate")}
                    >
                      {d.revoked ? "重新授权" : "更换令牌"}
                    </button>
                    {!d.revoked && (
                      <button
                        className="button secondary danger"
                        disabled={busy}
                        onClick={() => deviceAction(d, "revoke")}
                      >
                        撤销访问
                      </button>
                    )}
                  </div>
                </article>
              ))}
            </div>
          )}
        </>
      ) : (
        <>
          <form
            className="app-filters"
            onSubmit={(e) => {
              e.preventDefault();
              setFilter(query);
            }}
          >
            <label>
              状态
              <select
                value={deleted ? "deleted" : "active"}
                disabled={busy}
                onChange={(e) => setDeleted(e.target.value === "deleted")}
              >
                <option value="active">未删除</option>
                <option value="deleted">已删除</option>
              </select>
            </label>
            <label>
              类型
              <select
                value={kind}
                disabled={busy}
                onChange={(e) => setKind(e.target.value)}
              >
                <option value="">全部内容</option>
                <option value="text">文本</option>
                <option value="todo">待办</option>
                <option value="image">图片</option>
              </select>
            </label>
            <label className="app-search">
              搜索
              <input
                value={query}
                maxLength={200}
                onChange={(e) => setQuery(e.target.value)}
                placeholder="搜索正文"
              />
            </label>
            <button className="button secondary" disabled={busy}>
              搜索
            </button>
          </form>
          {items === null ? (
            <p role="status">正在加载内容…</p>
          ) : items.length === 0 ? (
            <Empty title="没有匹配的内容">
              {deleted
                ? "已删除的内容会保留在这里，可逐条恢复。"
                : "在这里或 App 中新增文本、待办和图片。"}
            </Empty>
          ) : (
            <div className="app-items">
              {items.map((item) => (
                <article className="app-item" key={item.id}>
                  <div className="app-card-head">
                    <Badge>
                      {{ text: "文本", todo: "待办", image: "图片" }[
                        item.kind
                      ] ?? item.kind}
                    </Badge>
                    {item.deleted_at && <Badge>已删除</Badge>}
                    <time>{date(item.deleted_at || item.updated_at)}</time>
                  </div>
                  {item.attachment_id && (
                    <a
                      href={`/api/app/attachments/${item.attachment_id}`}
                      target="_blank"
                      rel="noreferrer"
                    >
                      <img
                        className="app-image"
                        src={`/api/app/attachments/${item.attachment_id}?thumbnail=1`}
                        alt={item.body || "收件箱图片"}
                        loading="lazy"
                      />
                    </a>
                  )}
                  <p
                    className={
                      item.completed ? "app-item-body done" : "app-item-body"
                    }
                  >
                    {item.body}
                  </p>
                  <div className="app-actions">
                    {item.deleted_at ? (
                      <button
                        className="button secondary"
                        disabled={busy}
                        onClick={() => changeItem(item, "restore")}
                      >
                        恢复
                      </button>
                    ) : (
                      <>
                        {item.kind === "todo" && (
                          <button
                            className="button secondary"
                            disabled={busy}
                            onClick={() => changeItem(item, "set_completed")}
                          >
                            {item.completed ? "标为未完成" : "完成待办"}
                          </button>
                        )}
                        <button
                          className="button secondary"
                          disabled={busy}
                          onClick={() => setEditor({ item })}
                        >
                          编辑
                        </button>
                        <button
                          className="button secondary danger"
                          disabled={busy}
                          onClick={() => changeItem(item, "delete")}
                        >
                          删除
                        </button>
                      </>
                    )}
                  </div>
                </article>
              ))}
            </div>
          )}
          {next && (
            <button
              className="button secondary"
              disabled={busy}
              onClick={async () => {
                setBusy(true);
                try {
                  await refreshItems(next);
                } catch (e) {
                  setError(message(e));
                } finally {
                  setBusy(false);
                }
              }}
            >
              加载更多
            </button>
          )}
        </>
      )}
      {create && (
        <DeviceForm
          onClose={() => setCreate(false)}
          onCreated={(s) => {
            setSecret(s);
            setCreate(false);
            void refreshDevices().catch((e) => setError(message(e)));
          }}
        />
      )}
      {secret && (
        <TokenDialog secret={secret} onClose={() => setSecret(null)} />
      )}
      {editor && (
        <ItemForm
          item={editor.item}
          onClose={() => setEditor(null)}
          onSaved={() => {
            setEditor(null);
            void refreshItems().catch((e) => setError(message(e)));
          }}
        />
      )}
    </>
  );
}
function Modal({
  title,
  onClose,
  children,
}: {
  title: string;
  onClose: () => void;
  children: React.ReactNode;
}) {
  const ref = useRef<HTMLDialogElement>(null);
  useEffect(() => {
    ref.current?.showModal();
  }, []);
  return (
    <dialog
      ref={ref}
      className="app-dialog"
      onCancel={(e) => {
        e.preventDefault();
        onClose();
      }}
    >
      <header>
        <h2>{title}</h2>
        <button className="icon-button" aria-label="关闭" onClick={onClose}>
          <X size={20} />
        </button>
      </header>
      {children}
    </dialog>
  );
}
function DeviceForm({
  onClose,
  onCreated,
}: {
  onClose: () => void;
  onCreated: (s: { id: string; token: string }) => void;
}) {
  const [label, setLabel] = useState(""),
    [platform, setPlatform] = useState("android"),
    [busy, setBusy] = useState(false),
    [error, setError] = useState("");
  return (
    <Modal
      title="添加设备"
      onClose={() => {
        if (!busy) onClose();
      }}
    >
      <form
        onSubmit={async (e) => {
          e.preventDefault();
          setBusy(true);
          setError("");
          try {
            onCreated(
              await api("/app/devices", {
                method: "POST",
                body: JSON.stringify({ label, platform }),
              }),
            );
          } catch (e) {
            setError(message(e));
          } finally {
            setBusy(false);
          }
        }}
      >
        <label>
          设备名称
          <input
            autoFocus
            required
            maxLength={40}
            value={label}
            onChange={(e) => setLabel(e.target.value)}
            placeholder="例如：我的手机"
          />
        </label>
        <label>
          系统
          <select
            value={platform}
            onChange={(e) => setPlatform(e.target.value)}
          >
            <option value="android">Android</option>
            <option value="macos">macOS</option>
            <option value="windows">Windows</option>
          </select>
        </label>
        <p>每台设备使用独立令牌，可访问共享收件箱。</p>
        {error && (
          <p className="banner error" role="alert">
            {error}
          </p>
        )}
        <button className="button primary" disabled={busy}>
          {busy ? "正在添加…" : "添加并生成令牌"}
        </button>
      </form>
    </Modal>
  );
}
function TokenDialog({
  secret,
  onClose,
}: {
  secret: { id: string; token: string };
  onClose: () => void;
}) {
  const [copied, setCopied] = useState("");
  return (
    <Modal title="连接 App" onClose={onClose}>
      <p>在 Orbit 的连接页点击「扫码填写」，即可填入服务地址和设备令牌。</p>
      <div className="app-pairing-qr">
        <QRCodeSVG
          value={JSON.stringify({
            type: "orbit-app",
            version: 1,
            server: window.location.origin,
            token: secret.token,
          })}
          size={240}
          marginSize={4}
          level="M"
          title="Orbit 连接二维码"
        />
      </div>
      <p>二维码和令牌仅展示这一次，请勿分享。也可手动填写以下信息。</p>
      <label>
        服务地址
        <input readOnly value={window.location.origin} />
      </label>
      <label>
        设备令牌
        <textarea readOnly value={secret.token} />
      </label>
      <div className="app-actions">
        <button
          className="button secondary"
          onClick={async () => {
            try {
              await navigator.clipboard.writeText(secret.token);
              setCopied("令牌已复制");
            } catch {
              setCopied("未能复制，请选中上方令牌手动复制");
            }
          }}
        >
          <Copy size={16} />
          复制令牌
        </button>
        <button className="button primary" onClick={onClose}>
          完成
        </button>
      </div>
      <p role="status">{copied}</p>
    </Modal>
  );
}
function ItemForm({
  item,
  onClose,
  onSaved,
}: {
  item?: Item;
  onClose: () => void;
  onSaved: () => void;
}) {
  const [kind, setKind] = useState(item?.kind ?? "text"),
    [body, setBody] = useState(item?.body ?? ""),
    [file, setFile] = useState<File | null>(null),
    [busy, setBusy] = useState(false),
    [error, setError] = useState("");
  const operation = useRef<Operation | null>(null),
    attachment = useRef("");
  async function save(e: React.FormEvent) {
    e.preventDefault();
    setBusy(true);
    setError("");
    try {
      if (new TextEncoder().encode(body).length > 16000) {
        setError("正文过长，请缩短后保存。");
        return;
      }
      if (!operation.current) {
        if (!item && kind === "image" && !attachment.current) {
          if (!file || file.size > 10 * 1024 * 1024) {
            setError("请选择一张不超过 10 MB 的图片。");
            return;
          }
          const r = await fetch("/api/app/attachments", {
            method: "POST",
            body: file,
            credentials: "same-origin",
          });
          if (r.status === 401)
            window.dispatchEvent(new Event("orbit:unauthorized"));
          if (!r.ok) throw new APIError(r.status, "");
          attachment.current = (await r.json()).id;
        }
        operation.current = {
          operation_id: crypto.randomUUID(),
          item_id: item?.id ?? crypto.randomUUID(),
          type: item ? "update" : "create",
          expected_revision: item?.revision ?? "0",
          body,
          ...(!item
            ? {
                kind,
                ...(attachment.current
                  ? { attachment_id: attachment.current }
                  : {}),
              }
            : {}),
        };
      }
      await api("/app/operations", {
        method: "POST",
        body: JSON.stringify(operation.current),
      });
      onSaved();
    } catch (e) {
      if (e instanceof APIError && e.status === 400) operation.current = null;
      setError(message(e));
    } finally {
      setBusy(false);
    }
  }
  return (
    <Modal
      title={item ? "编辑内容" : "新增内容"}
      onClose={() => {
        if (!busy) onClose();
      }}
    >
      <form onSubmit={save}>
        <label>
          类型
          <select
            disabled={!!item || busy || !!operation.current}
            value={kind}
            onChange={(e) => setKind(e.target.value)}
          >
            <option value="text">文本</option>
            <option value="todo">待办</option>
            <option value="image">图片</option>
          </select>
        </label>
        {kind === "image" && !item && (
          <label>
            <ImageIcon size={16} />
            选择图片
            <input
              type="file"
              accept="image/jpeg,image/png,image/gif"
              disabled={busy || !!operation.current}
              onChange={(e) => {
                setFile(e.target.files?.[0] ?? null);
                attachment.current = "";
              }}
            />
          </label>
        )}
        <label>
          {kind === "image" ? "图片说明" : "正文"}
          <textarea
            rows={6}
            required={kind !== "image"}
            maxLength={16000}
            disabled={busy || !!operation.current}
            value={body}
            onChange={(e) => setBody(e.target.value)}
            autoFocus
          />
        </label>
        {error && (
          <p className="banner error" role="alert">
            {error}
          </p>
        )}
        <button className="button primary" disabled={busy}>
          {busy ? "正在保存…" : operation.current ? "重试保存" : "保存"}
        </button>
      </form>
    </Modal>
  );
}
