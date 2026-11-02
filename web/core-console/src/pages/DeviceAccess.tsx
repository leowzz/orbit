import { useCallback, useEffect, useRef, useState } from "react";
import { QRCodeSVG } from "qrcode.react";
import { RefreshCw, Smartphone, Copy } from "lucide-react";
import { api, APIError, date } from "../api";
import type { AppDevice, RouteDocument } from "../api";
import { Empty } from "../components";
import DeviceStatus from "../DeviceStatus";
import Modal from "../Modal";
function message(e: unknown): string {
  if (
    e instanceof APIError &&
    e.status === 503 &&
    e.message.includes('"mqtt_unavailable"')
  )
    return "MQTT 账号清理未完成，设备授权尚未变更。请检查 Core 的 MQTT 管理连接后重试。";
  return "操作未完成，请检查连接后重试。";
}
export default function DeviceAccess({
  deviceId,
  onChanged,
  onRoutesChange,
}: {
  deviceId: string;
  onChanged?: () => void;
  onRoutesChange?: (d: RouteDocument) => void;
}) {
  const [devices, setDevices] = useState<AppDevice[] | null>(null),
    [enabled, setEnabled] = useState(true),
    [error, setError] = useState(""),
    [busy, setBusy] = useState(false),
    [secret, setSecret] = useState<{ id: string; token: string } | null>(null);
  const devicesRequest = useRef(0);
  const refreshDevices = useCallback(async () => {
    const request = ++devicesRequest.current;
    const d = await api<{ enabled: boolean; devices: AppDevice[] }>(
      "/app/devices",
    );
    if (request !== devicesRequest.current) return;
    setEnabled(d.enabled);
    setDevices(d.devices ?? []);
  }, []);
  useEffect(() => {
    let active = true;
    let timer: ReturnType<typeof setTimeout>;
    async function poll() {
      try {
        await refreshDevices();
        if (active) setError("");
      } catch (e) {
        if (active) setError(message(e));
      }
      if (active) timer = setTimeout(poll, document.hidden ? 30000 : 5000);
    }
    void poll();
    return () => {
      active = false;
      clearTimeout(timer);
      devicesRequest.current++;
    };
  }, [refreshDevices]);
  async function refresh() {
    setBusy(true);
    setError("");
    try {
      await refreshDevices();
      onChanged?.();
    } catch (e) {
      setError(message(e));
    } finally {
      setBusy(false);
    }
  }
  async function deviceAction(
    d: AppDevice,
    action: "revoke" | "rotate" | "delete",
  ) {
    if (
      !window.confirm(
        action === "delete"
          ? `删除设备「${d.label || d.id}」？此设备将立即失去访问权限，数据配置与快捷配对同时移除，已有消息会保留。再次使用需重新添加设备。`
          : action === "revoke"
            ? `撤销「${d.label}」的访问？此设备将立即断开，已有收件箱内容会保留。`
            : `为「${d.label}」生成新令牌？旧令牌将立即失效。`,
      )
    )
      return;
    setBusy(true);
    setError("");
    let contentRemoved = false;
    try {
      if (action === "delete") {
        const latest = await api<RouteDocument>("/routes");
        if (latest.routes[d.id]) {
          const remaining = { ...latest.routes };
          delete remaining[d.id];
          const saved = await api<RouteDocument>("/routes", {
            method: "PUT",
            body: JSON.stringify({
              revision: latest.revision,
              routes: remaining,
            }),
          });
          contentRemoved = true;
          onRoutesChange?.(saved);
        }
      }
      const result = await api<{ id: string; token: string }>(
        `/app/devices/${encodeURIComponent(d.id)}${action === "delete" ? "" : `/${action}`}`,
        { method: action === "delete" ? "DELETE" : "POST" },
      );
      if (result.token) setSecret(result);
      await refreshDevices();
      onChanged?.();
    } catch (e) {
      setError(
        contentRemoved
          ? "数据配置已移除，未能确认设备删除结果，请刷新后检查。"
          : message(e),
      );
    } finally {
      setBusy(false);
    }
  }

  return (
    <>
      <div className="app-toolbar">
        <button className="button secondary" disabled={busy} onClick={refresh}>
          <RefreshCw size={15} />
          刷新
        </button>
      </div>
      {error && (
        <div className="banner error" role="alert">
          {error}
          <button onClick={refresh}>重试</button>
        </div>
      )}
      {!enabled ? (
        <Empty title="App 服务尚未启用">
          请先启用 App 服务，再回来添加设备。
        </Empty>
      ) : (
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
              {devices
                .filter((d) => !deviceId || d.id === deviceId)
                .map((d) => (
                  <article className="app-device" key={d.id}>
                    <div className="app-card-head">
                      <Smartphone size={21} />
                      <div>
                        <h2>{d.label || d.id}</h2>
                        <span>{d.platform}</span>
                      </div>
                      <DeviceStatus app={d} />
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
                      <button
                        className="button secondary danger"
                        disabled={busy}
                        onClick={() => deviceAction(d, "delete")}
                      >
                        删除设备
                      </button>
                    </div>
                  </article>
                ))}
            </div>
          )}
        </>
      )}
      {secret && (
        <TokenDialog secret={secret} onClose={() => setSecret(null)} />
      )}
    </>
  );
}
export function DeviceForm({
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
            <option value="web">浏览器</option>
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
export function TokenDialog({
  secret,
  onClose,
}: {
  secret: { id: string; token: string };
  onClose: () => void;
}) {
  const [copied, setCopied] = useState("");
  return (
    <Modal title="连接 App" onClose={onClose}>
      <p>
        在 Orbit 中粘贴连接信息或扫码即可。浏览器请打开工作台的 /box
        页面并使用工作台密码登录。
      </p>
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
              await navigator.clipboard.writeText(
                JSON.stringify({
                  type: "orbit-app",
                  version: 1,
                  server: window.location.origin,
                  token: secret.token,
                }),
              );
              setCopied("连接信息已复制");
            } catch {
              setCopied("未能复制，请选中上方令牌手动复制");
            }
          }}
        >
          <Copy size={16} />
          复制连接信息
        </button>
        <button className="button primary" onClick={onClose}>
          完成
        </button>
      </div>
      <p role="status">{copied}</p>
    </Modal>
  );
}
