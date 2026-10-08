import { useState } from "react";
import type { FormEvent } from "react";
import { ArrowRight, Eye, EyeOff } from "lucide-react";
import { api, APIError } from "../api";
import { Mark } from "../components";
export default function Login({ onLogin }: { onLogin: () => void }) {
  const [password, setPassword] = useState(""),
    [visible, setVisible] = useState(false),
    [busy, setBusy] = useState(false),
    [error, setError] = useState("");
  async function submit(e: FormEvent) {
    e.preventDefault();
    setBusy(true);
    setError("");
    try {
      await api("/auth/login", {
        method: "POST",
        body: JSON.stringify({ password }),
      });
      setPassword("");
      onLogin();
    } catch (e) {
      setError(
        e instanceof APIError && e.status === 401
          ? "密码不正确，请重新输入。"
          : e instanceof APIError && e.status === 429
            ? "尝试次数较多，请稍后再试。"
            : "暂时无法登录，请检查服务连接。",
      );
    } finally {
      setBusy(false);
    }
  }
  return (
    <div className="login">
      <section className="login-panel">
        <div className="login-form">
          <div className="wordmark">
            <Mark small />
            Orbit
          </div>
          <h1>登录控制台</h1>
          <p>管理设备、转发规则与收件箱。</p>
          <form onSubmit={submit}>
            <label htmlFor="password">控制台密码</label>
            <div className="password-field">
              <input
                id="password"
                type={visible ? "text" : "password"}
                autoComplete="current-password"
                autoFocus
                required
                value={password}
                onChange={(e) => setPassword(e.target.value)}
                placeholder="输入访问密码"
                disabled={busy}
              />
              <button
                type="button"
                aria-label={visible ? "隐藏密码" : "显示密码"}
                onClick={() => setVisible(!visible)}
              >
                {visible ? <EyeOff size={17} /> : <Eye size={17} />}
              </button>
            </div>
            {error && (
              <div className="form-error" role="alert">
                {error}
              </div>
            )}
            <button
              className="button primary login-submit"
              disabled={busy || !password}
            >
              {busy ? "正在登录…" : "进入控制台"}
              <ArrowRight size={17} />
            </button>
          </form>
          <p className="login-note">仅限授权访问 · 会话有效期由管理员配置</p>
        </div>
      </section>
    </div>
  );
}
