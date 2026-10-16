import { useEffect, useState } from "react";
import { Download, RefreshCw } from "lucide-react";
import { call } from "./bridge";

export interface UpdateStatus {
  currentVersion: string;
  latestVersion: string | null;
  available: boolean;
  supported: boolean;
  phase: string;
  downloaded: number;
  total: number;
  error: string;
}

export function useSoftwareUpdate() {
  const [status, setStatus] = useState<UpdateStatus>();
  useEffect(() => {
    let alive = true;
    const refresh = () =>
      call<UpdateStatus>("update_status")
        .then((value) => {
          if (alive) setStatus(value);
        })
        .catch(() => {});
    void refresh();
    const timer = setInterval(() => void refresh(), 1000);
    return () => {
      alive = false;
      clearInterval(timer);
    };
  }, []);
  return { status, setStatus };
}

export function SoftwareUpdate({
  status,
  onStatus,
  unsaved,
  working,
  onInstallingChange,
}: {
  status?: UpdateStatus;
  onStatus: (value: UpdateStatus) => void;
  unsaved: boolean;
  working: boolean;
  onInstallingChange: (value: boolean) => void;
}) {
  const [pending, setPending] = useState(false);
  const [error, setError] = useState("");
  const busy = pending || (!!status && status.phase !== "idle");
  const failure = error || status?.error;
  async function action(command: string) {
    const installing = command === "update_install";
    if (installing) onInstallingChange(true);
    setPending(true);
    setError("");
    try {
      onStatus(await call<UpdateStatus>(command));
    } catch (e) {
      setError(String(e instanceof Error ? e.message : e));
    } finally {
      setPending(false);
      if (installing) onInstallingChange(false);
    }
  }
  const labels: Record<string, string> = {
    checking: "正在检查更新…",
    downloading: "正在下载更新…",
    installing: "正在准备安装…",
    restarting: "正在重启…",
  };
  return (
    <section className="software-update" aria-label="软件更新">
      <h2>软件更新</h2>
      <div className="update-heading">
        <div>
          <strong>
            {status ? `当前版本 ${status.currentVersion}` : "正在读取版本…"}
          </strong>
          <p role="status">
            {status && labels[status.phase]
              ? labels[status.phase]
              : status?.available
                ? `发现新版本 ${status.latestVersion}`
                : failure
                  ? "未能检查更新，请重试"
                  : status?.latestVersion
                    ? "已是最新版本"
                    : "启动时自动检查，也可手动检查更新"}
          </p>
        </div>
        <div className="update-actions">
          <button disabled={busy} onClick={() => void action("update_check")}>
            <RefreshCw size={15} />
            检查更新
          </button>
          {status?.available && (
            <button
              className="primary"
              disabled={busy || unsaved || working}
              onClick={() => void action("update_install")}
            >
              <Download size={15} />
              更新并重启
            </button>
          )}
        </div>
      </div>
      {status?.phase === "downloading" && (
        <progress
          aria-label="更新下载进度"
          value={status.downloaded}
          max={status.total || 1}
        />
      )}
      {status?.available && (
        <small>
          {unsaved
            ? "请先保存或还原设置，再更新。"
            : "下载完成后自动重启，已保存的设置会保留。"}
        </small>
      )}
      {failure && (
        <p className="inline-error" role="alert">
          {failure}
        </p>
      )}
    </section>
  );
}
