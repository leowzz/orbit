import React, { useEffect, useState, type ReactNode } from "react";
import { createRoot } from "react-dom/client";
import {
  Activity,
  Cable,
  Database,
  SlidersHorizontal,
  ArrowDownToLine,
  FolderOpen,
  Play,
  Pause,
  RefreshCw,
  Check,
  Orbit,
  Mic2,
} from "lucide-react";
import { open } from "@tauri-apps/plugin-dialog";
import { call, preview } from "./bridge";
import type { InferenceModel, Settings, Status, Source } from "./types";
import "./style.css";
import { Shortcuts } from "./Shortcuts";
import { SoftwareUpdate, useSoftwareUpdate } from "./SoftwareUpdate";
import { useVisiblePolling, useWindowVisible } from "./ui-polling";
const pages = [
  { id: "status", name: "运行状态", icon: Activity },
  { id: "connection", name: "连接", icon: Cable },
  { id: "sources", name: "数据来源", icon: Database },
  { id: "asr", name: "语音识别", icon: Mic2 },
  { id: "shortcuts", name: "快捷入口", icon: Orbit },
  { id: "preferences", name: "偏好设置", icon: SlidersHorizontal },
] as const;
type Page = (typeof pages)[number]["id"];
function Field({
  label,
  hint,
  children,
}: {
  label: string;
  hint?: string;
  children: ReactNode;
}) {
  return (
    <label className="field">
      <span>{label}</span>
      {children}
      {hint && <small>{hint}</small>}
    </label>
  );
}
function Toggle({
  label,
  hint,
  value,
  onChange,
}: {
  label: string;
  hint?: string;
  value: boolean;
  onChange: (v: boolean) => void;
}) {
  return (
    <label className="toggle-row">
      <span>
        <strong>{label}</strong>
        {hint && <small>{hint}</small>}
      </span>
      <input
        type="checkbox"
        role="switch"
        checked={value}
        onChange={(e) => onChange(e.target.checked)}
      />
    </label>
  );
}
function Section({ title, children }: { title: string; children: ReactNode }) {
  return (
    <section>
      <h2>{title}</h2>
      {children}
    </section>
  );
}
function App() {
  const [settings, setSettings] = useState<Settings>();
  const [saved, setSaved] = useState("");
  const [status, setStatus] = useState<Status>();
  const [page, setPage] = useState<Page>("status");
  const [busy, setBusy] = useState(false);
  const [notice, setNotice] = useState("");
  const [error, setError] = useState("");
  const [autostart, setAutostart] = useState(false);
  const [models, setModels] = useState<InferenceModel[]>([]);
  const visible = useWindowVisible();
  const update = useSoftwareUpdate(visible);
  const [installing, setInstalling] = useState(false);
  const updating =
    installing ||
    ["downloading", "installing", "restarting"].includes(
      update.status?.phase ?? "",
    );
  const dirty = !!settings && JSON.stringify(settings) !== saved;
  const inference = status?.inference ?? {
    status: "offline",
    connected: false,
    device: "",
    queued: 0,
    outbox: 0,
    error: "",
    modelId: "qwen3-asr-0.6b-q8_0",
    modelReady: false,
    modelDownloading: false,
    modelDownloaded: 0,
    modelTotal: 0,
    modelError: "",
  };
  function bytes(value: number) {
    if (value >= 1024 ** 3) return `${(value / 1024 ** 3).toFixed(1)} GB`;
    if (value >= 1024 ** 2) return `${(value / 1024 ** 2).toFixed(0)} MB`;
    return `${value} B`;
  }
  async function action(fn: () => Promise<void>) {
    setBusy(true);
    setError("");
    setNotice("");
    try {
      await fn();
    } catch (e) {
      setError(String(e instanceof Error ? e.message : e));
    } finally {
      setBusy(false);
    }
  }
  async function load() {
    const s = await call<Settings>("settings_get");
    setSettings(s);
    setSaved(JSON.stringify(s));
    setStatus(await call<Status>("agent_status"));
    setAutostart(await call<boolean>("autostart_get"));
    setModels(await call<InferenceModel[]>("inference_models"));
  }
  useEffect(() => {
    void action(load);
  }, []);
  useVisiblePolling(
    visible,
    () => call<Status>("agent_status"),
    setStatus,
    2000,
  );
  useEffect(() => {
    document.querySelector(".scroll")?.scrollTo({ top: 0 });
  }, [page]);
  useEffect(() => {
    if (page === "asr") void action(async () => setModels(await call("inference_models")));
  }, [page]);
  function config<K extends keyof Settings["config"]>(
    key: K,
    value: Settings["config"][K],
  ) {
    setSettings((s) =>
      s ? { ...s, config: { ...s.config, [key]: value } } : s,
    );
  }
  function source(
    group: "core" | "mqtt" | "codex" | "usage" | "inference",
    key: string,
    value: unknown,
  ) {
    setSettings((s) =>
      s
        ? {
            ...s,
            config: {
              ...s.config,
              [group]: { ...s.config[group], [key]: value },
            },
          }
        : s,
    );
  }
  async function choose(
    group: "mqtt" | "codex",
    key: string,
    directory = false,
  ) {
    const path = await open({ directory, multiple: false });
    if (typeof path === "string") source(group, key, path);
  }
  function path(
    group: "mqtt" | "codex",
    key: string,
    label: string,
    value: string,
    directory = false,
  ) {
    return (
      <Field label={label}>
        <span className="path-input">
          <input
            value={value}
            onChange={(e) => source(group, key, e.target.value)}
            placeholder={directory ? "自动查找" : undefined}
          />
          <button
            type="button"
            aria-label={`选择${label}`}
            onClick={() => void action(() => choose(group, key, directory))}
          >
            <FolderOpen size={17} />
          </button>
        </span>
      </Field>
    );
  }
  function number(
    group: "codex" | "usage",
    key: string,
    label: string,
    value: number,
  ) {
    return (
      <Field label={label}>
        <input
          type="number"
          min={1}
          value={value}
          onChange={(e) => source(group, key, Number(e.target.value))}
        />
      </Field>
    );
  }
  async function importSettings() {
    const p = await open({
      multiple: false,
      filters: [{ name: "YAML 配置", extensions: ["yaml", "yml"] }],
    });
    if (typeof p === "string") {
      const imported = await call<Settings>("settings_import", { path: p });
      if (settings) {
        imported.config.shortcuts = settings.config.shortcuts;
        imported.config.shortcutsRevision = settings.config.shortcutsRevision;
        imported.config.shortcutsShowTitles =
          settings.config.shortcutsShowTitles;
        imported.config.shortcutsAtTop = settings.config.shortcutsAtTop;
        imported.config.nfcdMetricsLeftPadding =
          settings.config.nfcdMetricsLeftPadding;
      }
      setSettings(imported);
      setNotice("配置已导入，检查后保存即可生效");
      setPage("connection");
    }
  }
  async function save(): Promise<Settings> {
    const result = await call<Settings>("settings_save", { settings });
    setSettings(result);
    setSaved(JSON.stringify(result));
    setNotice("设置已保存");
    setStatus(await call<Status>("agent_status"));
    return result;
  }
  function sourceState(name: string, enabled: boolean, s: Source) {
    return (
      <div className="source-status">
        <div>
          <strong>{name}</strong>
          <span
            className={`tag ${enabled && s.lastSuccess && !s.error ? "good" : ""}`}
          >
            {!enabled
              ? "未启用"
              : s.error
                ? "采集异常"
                : s.lastSuccess
                  ? "已采集"
                  : "等待采集"}
          </span>
        </div>
        <p>
          {enabled
            ? s.error ||
              (s.lastSuccess
                ? `最近采集 ${new Date(s.lastSuccess * 1000).toLocaleTimeString("zh-CN")}`
                : "启动后自动采集本机数据")
            : "可在数据来源中开启"}
        </p>
        {enabled && s.sessions !== null && (
          <p className="counts">
            {s.sessions} 个会话 · {s.runningSessions} 个运行中
          </p>
        )}
      </div>
    );
  }
  return (
    <div className="app">
      <aside>
        <div className="brand">
          <Orbit size={27} />
          <div>
            Orbit <span>Agent</span>
          </div>
        </div>
        <nav aria-label="设置导航">
          {pages.map((p) => (
            <button
              key={p.id}
              className={page === p.id ? "selected" : ""}
              aria-current={page === p.id ? "page" : undefined}
              onClick={() => setPage(p.id)}
            >
              <p.icon size={18} />
              {p.name}
            </button>
          ))}
        </nav>
        <div className="aside-bottom">
          <span className={`dot ${status?.connected ? "online" : ""}`} />
          {status?.connected ? "已连接" : status?.running ? "连接中" : "未运行"}
          <small>
            {update.status ? `v${update.status.currentVersion}` : ""}
          </small>
        </div>
        {update.status?.available && (
          <button
            className="update-notice"
            onClick={() => setPage("preferences")}
          >
            有新版本可用
          </button>
        )}
      </aside>
      <main>
        <header>
          <div>
            <h1>{pages.find((p) => p.id === page)?.name}</h1>
            <p>
              {page === "status"
                ? "让工作台与你的电脑保持同步"
                : page === "connection"
                  ? "连接到你的 Orbit 服务"
                  : page === "sources"
                    ? "选择要同步到工作台的数据"
                    : page === "asr"
                      ? "管理本机模型、推理设备和 ASR Worker"
                    : page === "shortcuts"
                      ? "管理可由其他设备打开的本机应用和网址"
                      : "管理启动方式与远程操作"}
            </p>
          </div>
          {preview && <span className="tag">界面预览</span>}
        </header>
        <div className="feedback" aria-live="polite">
          {error && (
            <div className="alert error" role="alert">
              {error}
            </div>
          )}
          {notice && (
            <div className="alert">
              <Check size={16} />
              {notice}
            </div>
          )}
        </div>
        <div className="scroll">
          {page === "preferences" && (
            <SoftwareUpdate
              status={update.status}
              onStatus={update.setStatus}
              unsaved={dirty}
              working={busy}
              onInstallingChange={setInstalling}
            />
          )}
          {!settings ? (
            <div className="empty">
              <p>{busy ? "正在读取设置…" : "未能读取设置"}</p>
              {!busy && (
                <div className="recovery-actions">
                  <button onClick={() => void action(load)}>重试</button>
                  <button onClick={() => void action(importSettings)}>
                    导入配置恢复
                  </button>
                </div>
              )}
            </div>
          ) : (
            <fieldset disabled={busy || updating}>
              {page === "status" && (
                <>
                  <section className="connection-state">
                    <div
                      className={`status-symbol ${status?.connected ? "good" : ""}`}
                    >
                      <Activity size={25} />
                    </div>
                    <div>
                      <h2>{status?.message || "正在读取状态"}</h2>
                      <p>
                        {status?.lastAck
                          ? `服务最近确认 ${new Date(status.lastAck * 1000).toLocaleTimeString("zh-CN")}`
                          : "完成设置并启动后，即可在工作台查看本机"}
                      </p>
                    </div>
                    <button
                      className={status?.running ? "" : "primary"}
                      onClick={() =>
                        void action(async () => {
                          await call("agent_set_running", {
                            running: !status?.running,
                          });
                          setStatus(await call("agent_status"));
                        })
                      }
                    >
                      {status?.running ? (
                        <Pause size={15} />
                      ) : (
                        <Play size={15} />
                      )}
                      {status?.running ? "暂停" : "启动"}
                    </button>
                  </section>
                  <Section title="数据采集">
                    <div className="status-grid">
                      {sourceState(
                        "Codex",
                        settings.config.codex.enabled,
                        status?.codex || {
                          lastSuccess: null,
                          error: "",
                          sessions: null,
                          runningSessions: null,
                        },
                      )}
                      {sourceState(
                        "Sub2API",
                        settings.config.usage.enabled,
                        status?.usage || {
                          lastSuccess: null,
                          error: "",
                          sessions: null,
                          runningSessions: null,
                        },
                      )}
                    </div>
                    {status?.weeklyError && (
                      <p className="inline-error">周限：{status.weeklyError}</p>
                    )}
                    {settings.config.codex.enabled &&
                      settings.config.codex.rateLimits &&
                      status?.weeklyDiagnostics?.lastError && (
                        <details>
                          <summary>周限诊断</summary>
                          <p>
                            最近失败：
                            {new Date(
                              status.weeklyDiagnostics.lastFailure! * 1000,
                            ).toLocaleString("zh-CN")}{" "}
                            · 耗时 {status.weeklyDiagnostics.failureDurationMs}{" "}
                            ms
                          </p>
                          <p>
                            阶段：{status.weeklyDiagnostics.lastError.stage} ·
                            错误：{status.weeklyDiagnostics.lastError.code}
                            {status.weeklyDiagnostics.lastError.rpcCode !==
                              null &&
                              ` · RPC ${status.weeklyDiagnostics.lastError.rpcCode}`}
                            {status.weeklyDiagnostics.lastError.httpStatus !==
                              null &&
                              ` · HTTP ${status.weeklyDiagnostics.lastError.httpStatus}`}
                          </p>
                          <p>
                            最近成功：
                            {status.weeklyDiagnostics.lastSuccess === null
                              ? "尚未成功"
                              : new Date(
                                  status.weeklyDiagnostics.lastSuccess * 1000,
                                ).toLocaleString("zh-CN")}
                          </p>
                          <p>
                            最近请求耗时：{status.weeklyDiagnostics.durationMs}{" "}
                            ms · 连续失败：
                            {status.weeklyDiagnostics.consecutiveFailures}
                          </p>
                          {status.running &&
                            status.weeklyDiagnostics.nextAttempt !== null && (
                              <p>
                                下次采集：
                                {new Date(
                                  status.weeklyDiagnostics.nextAttempt * 1000,
                                ).toLocaleTimeString("zh-CN")}
                              </p>
                            )}
                        </details>
                      )}
                    {status?.clockError && (
                      <p className="muted">
                        时间校准暂不可用，当前使用系统时间。
                      </p>
                    )}
                  </Section>
                  <div className="help-row">
                    <span>首次使用？填写服务连接，或导入已有配置。</span>
                    <button onClick={() => setPage("connection")}>
                      设置连接
                    </button>
                  </div>
                </>
              )}
              {page === "connection" && (
                <>
                  <Section title="设备">
                    <Field label="本机名称">
                      <input
                        value={settings.config.hostLabel}
                        onChange={(e) => config("hostLabel", e.target.value)}
                      />
                    </Field>
                    <details>
                      <summary>设备标识</summary>
                      <Field
                        label="Agent ID"
                        hint="同一台电脑迁移时保留此标识；不要同时运行使用相同标识的两个 Agent。"
                      >
                        <input
                          value={settings.config.agentId}
                          onChange={(e) => config("agentId", e.target.value)}
                        />
                      </Field>
                    </details>
                  </Section>
                  <Section title="Core 自动下发">
                    <Field
                      label="Core 地址"
                      hint="填写 Core 的 HTTPS 地址和设备令牌，保存时会自动获取 MQTT 配置。"
                    >
                      <input
                        placeholder="https://orbit.example.com"
                        value={settings.config.core.url}
                        onChange={(e) =>
                          source("core", "url", e.target.value)
                        }
                      />
                    </Field>
                    <Field
                      label="Core 设备令牌"
                      hint="在 Core 管理台登记一个 Windows 设备后复制令牌；令牌保存在系统凭据存储中。"
                    >
                      <input
                        type="password"
                        autoComplete="new-password"
                        placeholder={
                          settings.hasCoreToken ? "已保存，未修改时保留" : "请输入设备令牌"
                        }
                        value={settings.coreToken ?? ""}
                        onChange={(e) =>
                          setSettings({ ...settings, coreToken: e.target.value })
                        }
                      />
                    </Field>
                    <button
                      type="button"
                      onClick={() =>
                        void action(async () => {
                          let result: Settings;
                          const needsProvision =
                            settings.config.mqtt.url.trim() === "";
                          if (dirty) {
                            result = await save();
                            if (!needsProvision) {
                              result = await call<Settings>("core_provision_mqtt");
                            }
                          } else {
                            result = await call<Settings>("core_provision_mqtt");
                          }
                          setSettings(result);
                          setSaved(JSON.stringify(result));
                          setNotice("已从 Core 获取 MQTT 配置并连接");
                          setStatus(await call<Status>("agent_status"));
                        })
                      }
                    >
                      <RefreshCw size={15} />
                      从 Core 更新 MQTT
                    </button>
                  </Section>
                  <Section title="服务连接">
                    <Field label="MQTT 地址">
                      <input
                        placeholder="mqtts://broker.example.com:8883"
                        value={settings.config.mqtt.url}
                        onChange={(e) => source("mqtt", "url", e.target.value)}
                      />
                    </Field>
                    <div className="two-col">
                      <Field label="用户名">
                        <input
                          autoComplete="off"
                          value={settings.config.mqtt.username}
                          onChange={(e) =>
                            source("mqtt", "username", e.target.value)
                          }
                        />
                      </Field>
                      <Field label="密码">
                        <input
                          type="password"
                          autoComplete="new-password"
                          placeholder={
                            settings.hasMqttPassword
                              ? "已保存，未修改时保留"
                              : "可选"
                          }
                          value={settings.mqttPassword ?? ""}
                          onChange={(e) =>
                            setSettings({
                              ...settings,
                              mqttPassword: e.target.value,
                            })
                          }
                        />
                      </Field>
                    </div>
                    <details>
                      <summary>TLS 证书</summary>
                      {path(
                        "mqtt",
                        "caFile",
                        "CA 证书",
                        settings.config.mqtt.caFile,
                      )}
                      <div className="two-col">
                        {path(
                          "mqtt",
                          "certFile",
                          "客户端证书",
                          settings.config.mqtt.certFile,
                        )}
                        {path(
                          "mqtt",
                          "keyFile",
                          "客户端私钥",
                          settings.config.mqtt.keyFile,
                        )}
                      </div>
                    </details>
                    <button
                      onClick={() =>
                        void action(async () =>
                          setNotice(
                            await call("connection_test", { settings }),
                          ),
                        )
                      }
                    >
                      <RefreshCw size={15} />
                      测试连接
                    </button>
                  </Section>
                  <div className="help-row">
                    <span>已有 Go Agent 配置可直接导入。</span>
                    <button onClick={() => void action(importSettings)}>
                      <ArrowDownToLine size={15} />
                      导入配置
                    </button>
                  </div>
                </>
              )}
              {page === "asr" && (
                <>
                  <section className="connection-state">
                    <div
                      className={`status-symbol ${inference.connected && inference.modelReady ? "good" : ""}`}
                    >
                      <Mic2 size={25} />
                    </div>
                    <div>
                      <h2>
                        {!settings.config.inference.enabled
                          ? "本地 ASR 未启用"
                          : inference.error ||
                            (inference.connected
                              ? "ASR Worker 已连接 MQTT"
                              : "ASR Worker 未连接")}
                      </h2>
                      <p>
                        {inference.modelReady
                          ? `${inference.device.toUpperCase()} · ${inference.queued} 个待处理任务 · ${inference.outbox} 个待确认结果`
                          : "下载并选择模型后，保存设置即可启动本机推理。"}
                      </p>
                    </div>
                    <span className={`tag ${inference.modelReady ? "good" : ""}`}>
                      {inference.status || "offline"}
                    </span>
                  </section>
                  <Section title="Worker 设置">
                    <Toggle
                      label="启用本地 ASR Worker"
                      hint="Agent 会在后台加载 speech.cpp，并从 Core 音频队列消费任务。"
                      value={settings.config.inference.enabled}
                      onChange={(v) => source("inference", "enabled", v)}
                    />
                    <div className="two-col">
                      <Field label="计算设备">
                        <select
                          value={settings.config.inference.device}
                          onChange={(e) =>
                            source("inference", "device", e.target.value)
                          }
                        >
                          <option value="auto">自动选择</option>
                          <option value="cpu">CPU</option>
                          <option value="gpu">GPU（Vulkan / Metal）</option>
                        </select>
                      </Field>
                      <Field label="队列容量">
                        <input
                          type="number"
                          min={1}
                          max={1000}
                          value={settings.config.inference.queueCapacity}
                          onChange={(e) =>
                            source("inference", "queueCapacity", Number(e.target.value))
                          }
                        />
                      </Field>
                    </div>
                    <div className="two-col">
                      <Field label="Worker 标识">
                        <input
                          value={settings.config.inference.agentId}
                          onChange={(e) =>
                            source("inference", "agentId", e.target.value)
                          }
                        />
                      </Field>
                      <Field label="Worker 用户名">
                        <input
                          value={settings.config.inference.username}
                          onChange={(e) =>
                            source("inference", "username", e.target.value)
                          }
                        />
                      </Field>
                    </div>
                    <Field label="Worker 密码" hint="密码保存在 Windows 凭据存储中。">
                      <input
                        type="password"
                        autoComplete="new-password"
                        placeholder={
                          settings.hasInferencePassword
                            ? "已保存，未修改时保留"
                            : ""
                        }
                        value={settings.inferencePassword ?? ""}
                        onChange={(e) =>
                          setSettings({
                            ...settings,
                            inferencePassword: e.target.value,
                          })
                        }
                      />
                    </Field>
                  </Section>
                  <Section title="模型库">
                    {models.length === 0 ? (
                      <p className="muted">正在读取模型库…</p>
                    ) : (
                      models.map((model) => {
                        const progress =
                          inference.modelTotal > 0
                            ? Math.min(
                                100,
                                (inference.modelDownloaded / inference.modelTotal) * 100,
                              )
                            : 0;
                        return (
                          <div className="model-card" key={model.id}>
                            <div className="model-card-head">
                              <div>
                                <strong>{model.name}</strong>
                                <small>{model.filename} · {bytes(model.size)}</small>
                              </div>
                              <span className={`tag ${model.active && model.installed ? "good" : ""}`}>
                                {model.active && model.installed
                                  ? "当前使用"
                                  : model.installed
                                    ? "已下载"
                                    : "未下载"}
                              </span>
                            </div>
                            <p className="muted">固定版本 {model.revision.slice(0, 8)} · Qwen3-ASR 原生 GGUF</p>
                            <p className="model-path">{model.path}</p>
                            {inference.modelDownloading && (
                              <div className="model-progress" aria-label="模型下载进度">
                                <div style={{ width: `${progress}%` }} />
                                <span>{progress.toFixed(0)}% · {bytes(inference.modelDownloaded)} / {bytes(inference.modelTotal)}</span>
                              </div>
                            )}
                            {inference.modelError && <p className="inline-error">{inference.modelError}</p>}
                            <div className="model-actions">
                              <button
                                className="primary"
                                disabled={inference.modelDownloading || model.active}
                                onClick={() =>
                                  void action(async () => {
                                    const result = await call<Settings>(
                                      model.installed
                                        ? "inference_select_model"
                                        : "inference_download_model",
                                      { modelId: model.id },
                                    );
                                    setSettings(result);
                                    setSaved(JSON.stringify(result));
                                    setModels(await call<InferenceModel[]>("inference_models"));
                                    setStatus(await call<Status>("agent_status"));
                                    setNotice(model.installed ? "模型已切换并应用" : "模型下载完成，已应用");
                                  })
                                }
                              >
                                {model.active ? "当前使用" : model.installed ? "使用此模型" : "下载并使用"}
                              </button>
                              <button
                                onClick={() =>
                                  void action(async () => {
                                    const p = await open({
                                      multiple: false,
                                      filters: [{ name: "GGUF 模型", extensions: ["gguf"] }],
                                    });
                                    if (typeof p === "string") source("inference", "modelPath", p);
                                  })
                                }
                              >
                                选择本地 GGUF
                              </button>
                            </div>
                          </div>
                        );
                      })
                    )}
                  </Section>
                  <Section title="运行库">
                    <Field
                      label="speech.cpp 运行库"
                      hint="Windows 使用 speech.dll；运行库由 Agent 负责加载，模型与运行库可以独立替换。"
                    >
                      <span className="path-input">
                        <input
                          value={settings.config.inference.runtimePath}
                          onChange={(e) =>
                            source("inference", "runtimePath", e.target.value)
                          }
                        />
                        <button
                          type="button"
                          onClick={() =>
                            void action(async () => {
                              const p = await open({
                                multiple: false,
                                filters: [{ name: "speech runtime", extensions: ["dll", "dylib"] }],
                              });
                              if (typeof p === "string") source("inference", "runtimePath", p);
                            })
                          }
                        >
                          <FolderOpen size={17} />
                        </button>
                      </span>
                    </Field>
                    <p className="muted">
                      {models[0]?.runtimeInstalled
                        ? `检测到托管运行库：${models[0].runtimePath}`
                        : "尚未检测到托管运行库；请填写已安装的 speech.dll 路径。"}
                    </p>
                  </Section>
                </>
              )}
              {page === "sources" && (
                <>
                  <Section title="Codex">
                    <Toggle
                      label="同步 Codex 会话"
                      hint="读取本机的会话状态，显示到工作台。"
                      value={settings.config.codex.enabled}
                      onChange={(v) => {
                        source("codex", "enabled", v);
                        if (!v) config("allowOpen", false);
                      }}
                    />
                    {settings.config.codex.enabled && (
                      <>
                        <Toggle
                          label="显示周限余量"
                          hint="通过已登录的 Codex 获取周限。"
                          value={settings.config.codex.rateLimits}
                          onChange={(v) => source("codex", "rateLimits", v)}
                        />
                        <Toggle
                          label="同步会话标题"
                          value={settings.config.codex.includeTitle}
                          onChange={(v) => source("codex", "includeTitle", v)}
                        />
                        <Toggle
                          label="同步项目名称"
                          value={settings.config.codex.includeProject}
                          onChange={(v) => source("codex", "includeProject", v)}
                        />
                        <p className="muted">
                          开启后，标题和项目名称将发送到 Orbit
                          服务及已连接的工作台。
                        </p>
                        <details>
                          <summary>采集设置</summary>
                          {path(
                            "codex",
                            "home",
                            "Codex 数据目录",
                            settings.config.codex.home,
                            true,
                          )}
                          {path(
                            "codex",
                            "binary",
                            "Codex 程序",
                            settings.config.codex.binary,
                          )}
                          <div className="two-col">
                            {number(
                              "codex",
                              "interval",
                              "采集间隔（秒）",
                              settings.config.codex.interval,
                            )}
                            {number(
                              "codex",
                              "ttl",
                              "数据有效期（秒）",
                              settings.config.codex.ttl,
                            )}
                          </div>
                          {number(
                            "codex",
                            "limit",
                            "显示会话数（最多 20）",
                            settings.config.codex.limit,
                          )}
                          <Toggle
                            label="包含已归档会话"
                            value={settings.config.codex.includeArchived}
                            onChange={(v) =>
                              source("codex", "includeArchived", v)
                            }
                          />
                          <Field label="忽略路径（每行一项）">
                            <textarea
                              value={settings.config.codex.ignoreCwd.join("\n")}
                              onChange={(e) =>
                                source(
                                  "codex",
                                  "ignoreCwd",
                                  e.target.value.split("\n"),
                                )
                              }
                            />
                          </Field>
                          <Field label="忽略来源（每行一项）">
                            <textarea
                              value={settings.config.codex.ignoreSource.join(
                                "\n",
                              )}
                              onChange={(e) =>
                                source(
                                  "codex",
                                  "ignoreSource",
                                  e.target.value.split("\n"),
                                )
                              }
                            />
                          </Field>
                        </details>
                      </>
                    )}
                  </Section>
                  <Section title="Sub2API">
                    <Toggle
                      label="同步今日用量"
                      hint="显示费用、Token 数和 TPM。"
                      value={settings.config.usage.enabled}
                      onChange={(v) => source("usage", "enabled", v)}
                    />
                    {settings.config.usage.enabled && (
                      <>
                        <Field label="服务地址">
                          <input
                            placeholder="https://api.example.com"
                            value={settings.config.usage.baseUrl}
                            onChange={(e) =>
                              source("usage", "baseUrl", e.target.value)
                            }
                          />
                        </Field>
                        <div className="two-col">
                          <Field label="邮箱">
                            <input
                              type="email"
                              value={settings.config.usage.email}
                              onChange={(e) =>
                                source("usage", "email", e.target.value)
                              }
                            />
                          </Field>
                          <Field label="密码">
                            <input
                              type="password"
                              autoComplete="new-password"
                              placeholder={
                                settings.hasUsagePassword
                                  ? "已保存，未修改时保留"
                                  : ""
                              }
                              value={settings.usagePassword ?? ""}
                              onChange={(e) =>
                                setSettings({
                                  ...settings,
                                  usagePassword: e.target.value,
                                })
                              }
                            />
                          </Field>
                        </div>
                        <details>
                          <summary>用量设置</summary>
                          <Field label="统计时区">
                            <input
                              value={settings.config.usage.timezone}
                              onChange={(e) =>
                                source("usage", "timezone", e.target.value)
                              }
                            />
                          </Field>
                          <div className="two-col">
                            {number(
                              "usage",
                              "interval",
                              "采集间隔（秒）",
                              settings.config.usage.interval,
                            )}
                            {number(
                              "usage",
                              "ttl",
                              "数据有效期（秒）",
                              settings.config.usage.ttl,
                            )}
                          </div>
                          {number(
                            "usage",
                            "timeout",
                            "请求超时（秒）",
                            settings.config.usage.timeout,
                          )}
                          {(
                            [
                              "loginEndpoint",
                              "refreshEndpoint",
                              "usageEndpoint",
                            ] as const
                          ).map((key, i) => (
                            <Field
                              key={key}
                              label={
                                [
                                  "自定义登录接口",
                                  "自定义刷新接口",
                                  "自定义用量接口",
                                ][i]
                              }
                            >
                              <input
                                placeholder="使用默认接口"
                                value={settings.config.usage[key]}
                                onChange={(e) =>
                                  source("usage", key, e.target.value)
                                }
                              />
                            </Field>
                          ))}
                        </details>
                      </>
                    )}
                  </Section>
                </>
              )}
              {page === "shortcuts" && (
                <Shortcuts
                  visible={visible}
                  items={settings.config.shortcuts}
                  onChange={(items) => config("shortcuts", items)}
                />
              )}
              {page === "preferences" && (
                <>
                  <Section title="启动与运行">
                    <Toggle
                      label="登录电脑后自动启动"
                      hint="在后台启动，随时从托盘打开设置。立即生效。"
                      value={autostart}
                      onChange={(v) =>
                        void action(async () => {
                          await call("autostart_set", { enabled: v });
                          setAutostart(v);
                        })
                      }
                    />
                    <p className="muted">
                      关闭设置窗口后，Agent 会继续运行。退出请使用托盘菜单。
                    </p>
                  </Section>
                  <Section title="远程操作">
                    <Toggle
                      label="允许工作台打开 Codex 会话"
                      hint="点击工作台中的会话时，在这台电脑上打开对应会话。需要安装 Codex 桌面应用。"
                      value={settings.config.allowOpen}
                      onChange={(v) => {
                        config("allowOpen", v);
                        if (v) source("codex", "enabled", true);
                      }}
                    />
                  </Section>
                  <Section title="时间校准">
                    <Field
                      label="时间服务器"
                      hint="仅用于同步数据时间，不修改系统时钟。"
                    >
                      <input
                        value={settings.config.ntpServer}
                        onChange={(e) => config("ntpServer", e.target.value)}
                      />
                    </Field>
                  </Section>
                  <p className="muted">
                    连接设置保存在本机，密码保存在系统凭据存储中。
                  </p>
                </>
              )}
            </fieldset>
          )}
        </div>
        <footer>
          <span>{busy ? "正在处理…" : dirty ? "有未保存的修改" : ""}</span>
          <div>
            <button
              disabled={busy || updating || !dirty}
              onClick={() => void action(load)}
            >
              还原修改
            </button>
            <button
              className="primary"
              disabled={busy || updating || !dirty}
              onClick={() => void action(async () => { await save(); })}
            >
              保存并应用
            </button>
          </div>
        </footer>
      </main>
    </div>
  );
}
createRoot(document.getElementById("root")!).render(
  <React.StrictMode>
    <App />
  </React.StrictMode>,
);
