import { useCallback, useEffect, useState } from "react";
import { Copy, Eye, EyeOff, Plus, RefreshCw } from "lucide-react";
import { api } from "../api";
import { Badge, Empty } from "../components";
import { Modal } from "./Apps";

type User = {
  username: string;
  is_superuser: boolean;
  protected: boolean;
  revoked: boolean;
  clients: string[];
};
type Users = { enabled: boolean; users: User[]; broker_url?: string };
type Result = { username: string; password?: string; warning?: string };
type Secret = Result & {
  participant_id?: string;
  kind?: string;
  retry?: "revoke" | "disconnect";
};
const message = (e: unknown) =>
  e instanceof Error ? e.message : "操作未完成，请重试";

export default function MqttUsers() {
  const [data, setData] = useState<Users | null>(null);
  const [loading, setLoading] = useState(true);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");
  const [notice, setNotice] = useState("");
  const [create, setCreate] = useState(false);
  const [secret, setSecret] = useState<Secret | null>(null);
  const load = useCallback(async () => {
    setLoading(true);
    try {
      setData(await api<Users>("/mqtt/users"));
      setError("");
    } catch (e) {
      setError(message(e));
    } finally {
      setLoading(false);
    }
  }, []);
  useEffect(() => {
    void load();
  }, [load]);

  async function change(
    username: string,
    action: "rotate" | "revoke" | "disconnect",
    retry = false,
  ) {
    if (
      !retry &&
      !window.confirm(
        action === "rotate"
          ? `重置「${username}」的密码并断开现有连接？使用此账号的设备需要更新密码。`
          : `吊销「${username}」？将删除登录凭据和用户名权限规则，并断开使用此账号的所有连接。`,
      )
    )
      return;
    setBusy(true);
    setError("");
    setNotice("");
    try {
      const result = await api<Result>(
        `/mqtt/users/${encodeURIComponent(username)}/${action}`,
        { method: "POST" },
      );
      if (action === "disconnect") {
        setSecret((current) =>
          current ? { ...current, warning: undefined, retry: undefined } : null,
        );
      } else if (result.password || result.warning) {
        setSecret({
          ...result,
          retry: result.warning
            ? action === "rotate"
              ? "disconnect"
              : "revoke"
            : undefined,
        });
      } else {
        setSecret(null);
        setNotice(`已吊销「${username}」`);
      }
      await load();
    } catch (e) {
      setError(message(e));
    } finally {
      setBusy(false);
    }
  }

  return (
    <>
      <div className="page-head">
        <h1>MQTT 账号</h1>
        <div className="app-actions">
          <button
            className="button secondary"
            disabled={busy || loading}
            onClick={load}
          >
            <RefreshCw size={16} />
            刷新
          </button>
          {data?.enabled && (
            <button
              className="button primary"
              disabled={busy || loading}
              onClick={() => setCreate(true)}
            >
              <Plus size={16} />
              创建账号
            </button>
          )}
        </div>
      </div>
      {error && (
        <div className="banner error" role="alert">
          {error}
        </div>
      )}
      {notice && (
        <div className="banner success" role="status">
          {notice}
        </div>
      )}
      {!data && loading && <p role="status">正在读取 MQTT 账号…</p>}
      {data && !data.enabled && (
        <Empty title="尚未配置 MQTT 管理">
          在 Core 配置文件中填写 mqtt_admin 的地址、API Key 和 Secret，重启 Core
          后即可使用。
        </Empty>
      )}
      {data?.enabled && (
        <section
          className="panel mqtt-users"
          aria-label="MQTT 账号列表"
          aria-busy={loading}
        >
          {data.users.length === 0 && (
            <Empty title="还没有 MQTT 账号">创建账号后即可配置连接设备。</Empty>
          )}
          {data.users.map((user) => (
            <article className="mqtt-user" key={user.username}>
              <div className="mqtt-user-info">
                <div className="mqtt-user-title">
                  <strong>{user.username}</strong>
                  {user.protected ? (
                    <Badge>Core 正在使用</Badge>
                  ) : user.revoked ? (
                    <Badge tone="warn">凭据已删除 · 连接待清理</Badge>
                  ) : (
                    <Badge tone={user.clients.length ? "good" : "neutral"}>
                      {user.clients.length ? "已连接" : "未连接"}
                    </Badge>
                  )}
                  {user.is_superuser && <Badge tone="warn">超级用户</Badge>}
                </div>
                {user.clients.length > 0 && (
                  <div className="mqtt-client-ids mono">
                    {user.clients.join(" · ")}
                  </div>
                )}
              </div>
              <div className="app-actions">
                {!user.revoked && (
                  <button
                    className="button secondary"
                    disabled={busy || loading || user.protected}
                    onClick={() => change(user.username, "rotate")}
                  >
                    重置密码
                  </button>
                )}
                <button
                  className="button secondary danger"
                  disabled={busy || loading || user.protected}
                  onClick={() => change(user.username, "revoke")}
                >
                  {user.revoked ? "重试吊销" : "吊销"}
                </button>
              </div>
            </article>
          ))}
        </section>
      )}
      {create && (
        <CreateDialog
          onClose={() => setCreate(false)}
          onCreated={(result) => {
            setCreate(false);
            setSecret(result);
            void load();
          }}
        />
      )}
      {secret && (
        <CredentialDialog
          secret={secret}
          broker={data?.broker_url ?? ""}
          busy={busy}
          error={error}
          onClose={() => {
            if (!busy) setSecret(null);
          }}
          onRetry={() => {
            if (secret.retry) void change(secret.username, secret.retry, true);
          }}
        />
      )}
    </>
  );
}

function CreateDialog({
  onClose,
  onCreated,
}: {
  onClose: () => void;
  onCreated: (s: Secret) => void;
}) {
  const [kind, setKind] = useState("node");
  const [id, setId] = useState("");
  const [username, setUsername] = useState<string | null>(null);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");
  const name = username ?? (id ? `orbit-${kind}-${id}` : "");
  return (
    <Modal
      title="创建 MQTT 账号"
      onClose={() => {
        if (!busy) onClose();
      }}
    >
      <form
        onSubmit={async (e) => {
          e.preventDefault();
          if (busy) return;
          setBusy(true);
          setError("");
          try {
            const result = await api<Result>("/mqtt/users", {
              method: "POST",
              body: JSON.stringify({
                username: name,
                kind,
                participant_id: id,
              }),
            });
            onCreated({ ...result, kind, participant_id: id });
          } catch (e) {
            setError(message(e));
          } finally {
            setBusy(false);
          }
        }}
      >
        <label>
          连接类型
          <select
            value={kind}
            disabled={busy}
            onChange={(e) => setKind(e.target.value)}
          >
            <option value="node">设备（Node）</option>
            <option value="audio-node">录音设备（Android Node）</option>
            <option value="agent">电脑（Agent）</option>
            <option value="worker">语音识别（Worker Agent）</option>
          </select>
        </label>
        <label>
          {kind.endsWith("node") ? "Node ID" : "Agent ID"}
          <input
            name="participant_id"
            autoFocus
            required
            pattern={"[a-z0-9][a-z0-9_\\-]{0,63}"}
            maxLength={64}
            value={id}
            disabled={busy}
            onChange={(e) => setId(e.target.value)}
            placeholder={
              kind.endsWith("node") ? "例如：android-phone" : "例如：asr-sdt"
            }
          />
        </label>
        <label>
          用户名
          <input
            name="username"
            required
            pattern={"[A-Za-z0-9][A-Za-z0-9_.@\\-]{0,127}"}
            maxLength={128}
            value={name}
            disabled={busy}
            onChange={(e) => setUsername(e.target.value)}
          />
        </label>
        <p>
          新账号仅能发布和订阅此 ID 所需的 Orbit Topic。请在连接端使用相同 ID。
        </p>
        {error && (
          <p className="banner error" role="alert">
            {error}
          </p>
        )}
        <button className="button primary" disabled={busy}>
          {busy ? "正在创建…" : "创建并生成密码"}
        </button>
      </form>
    </Modal>
  );
}

function CredentialDialog({
  secret,
  broker,
  busy,
  error,
  onClose,
  onRetry,
}: {
  secret: Secret;
  broker: string;
  busy: boolean;
  error: string;
  onClose: () => void;
  onRetry: () => void;
}) {
  const [copied, setCopied] = useState("");
  const [visible, setVisible] = useState(false);
  return (
    <Modal
      title={secret.password ? "MQTT 连接信息" : "吊销尚未完成"}
      onClose={onClose}
    >
      {secret.password && (
        <>
          <p>密码仅展示这一次，请保存到连接设备。</p>
          <label>
            服务地址
            <input readOnly value={broker} />
          </label>
          <label>
            用户名
            <input readOnly value={secret.username} />
          </label>
          {secret.participant_id && (
            <label>
              {secret.kind?.endsWith("node") ? "Node ID" : "Agent ID"}
              <input readOnly value={secret.participant_id} />
            </label>
          )}
          <label>
            密码
            <div className="mqtt-password">
              <input
                type={visible ? "text" : "password"}
                name="mqtt_password"
                autoComplete="new-password"
                readOnly
                value={secret.password}
              />
              <button
                className="icon-button"
                type="button"
                aria-label={visible ? "隐藏密码" : "显示密码"}
                onClick={() => setVisible(!visible)}
              >
                {visible ? <EyeOff size={18} /> : <Eye size={18} />}
              </button>
            </div>
          </label>
        </>
      )}
      {secret.warning && (
        <p className="banner" role="alert">
          {secret.warning}
        </p>
      )}
      {error && (
        <p className="banner error" role="alert">
          {error}
        </p>
      )}
      <div className="app-actions">
        {secret.password && (
          <button
            className="button secondary"
            disabled={busy}
            onClick={async () => {
              try {
                await navigator.clipboard.writeText(
                  [
                    `服务地址: ${broker}`,
                    `用户名: ${secret.username}`,
                    ...(secret.participant_id
                      ? [
                          `${secret.kind?.endsWith("node") ? "Node" : "Agent"} ID: ${secret.participant_id}`,
                        ]
                      : []),
                    `密码: ${secret.password}`,
                  ].join("\n"),
                );
                setCopied("连接信息已复制");
              } catch {
                setCopied("未能复制，请选中字段手动复制");
              }
            }}
          >
            <Copy size={16} />
            复制连接信息
          </button>
        )}
        {secret.retry && (
          <button className="button primary" disabled={busy} onClick={onRetry}>
            {busy
              ? "正在处理…"
              : secret.retry === "disconnect"
                ? "重试断开连接"
                : "重试吊销"}
          </button>
        )}
        <button className="button secondary" disabled={busy} onClick={onClose}>
          关闭
        </button>
      </div>
      {copied && <p role="status">{copied}</p>}
    </Modal>
  );
}
