import { useState } from "react";
import type { FormEvent } from "react";
import { ArrowRight, Eye, EyeOff, LockKeyhole } from "lucide-react";
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
      <section className="login-story">
        <div className="wordmark">
          <Mark />
          orbit<span> / core</span>
        </div>
        <div className="story-content">
          <div className="eyebrow">A SMALL NETWORK. YOUR WORLD.</div>
          <h1>
            让每一份状态，
            <br />
            抵达对的设备。
          </h1>
          <p>
            连接主机与设备，组织数据的去向。
            <br />
            一个安静、有序的个人设备网络。
          </p>
          <div className="network-art" aria-hidden>
            <div className="orbit-ring ring-one" />
            <div className="orbit-ring ring-two" />
            <div className="art-center">
              <Mark />
            </div>
            <span className="art-point point-one" />
            <span className="art-point point-two" />
            <span className="art-point point-three" />
            <span className="art-label label-one">OBSERVE</span>
            <span className="art-label label-two">CONNECT</span>
            <span className="art-label label-three">PROJECT</span>
          </div>
        </div>
        <div className="story-footer">
          <span>ORBIT NETWORK</span>
          <span>01 — CONTROL PLANE</span>
        </div>
      </section>
      <section className="login-panel">
        <div className="login-form">
          <span className="login-icon">
            <LockKeyhole size={22} strokeWidth={1.4} />
          </span>
          <div className="eyebrow">CORE CONSOLE</div>
          <h2>欢迎回来。</h2>
          <p>登录，查看你的网络。</p>
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
          <p className="login-note">仅限授权访问 · 登录会话有效期 24 小时</p>
        </div>
        <div className="login-bottom">你的网络，始终由你掌控。</div>
      </section>
    </div>
  );
}
