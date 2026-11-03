import { useState } from "react";
import type { FormEvent } from "react";
import { ArrowRight, Eye, EyeOff } from "lucide-react";
import { api, APIError } from "../api";
import { Mark } from "../components";
import { Button } from "../components/ui/button";
import { Input } from "../components/ui/input";
import { Card, CardContent, CardHeader } from "../components/ui/card";
import ThemeToggle from "../ThemeToggle";
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
    <div className="flex min-h-svh items-center justify-center bg-muted/40 p-4">
      <Card className="w-full max-w-sm gap-5">
        <CardHeader>
          <div className="mb-5 flex items-center gap-3 text-lg font-semibold">
            <Mark small />
            Orbit
            <div className="ml-auto">
              <ThemeToggle />
            </div>
          </div>
          <h1 className="text-xl font-semibold tracking-tight">登录工作台</h1>
        </CardHeader>
        <CardContent>
          <form onSubmit={submit} className="space-y-4">
            <label htmlFor="password" className="block text-sm font-medium">
              工作台密码
            </label>
            <div className="relative">
              <Input
                className="pr-10"
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
              <Button
                variant="ghost"
                size="icon"
                className="absolute right-0 top-0"
                type="button"
                aria-label={visible ? "隐藏密码" : "显示密码"}
                onClick={() => setVisible(!visible)}
              >
                {visible ? <EyeOff size={17} /> : <Eye size={17} />}
              </Button>
            </div>
            {error && (
              <div className="form-error" role="alert">
                {error}
              </div>
            )}
            <Button className="w-full" disabled={busy || !password}>
              {busy ? "正在登录…" : "进入工作台"}
              <ArrowRight size={17} />
            </Button>
          </form>
        </CardContent>
      </Card>
    </div>
  );
}
