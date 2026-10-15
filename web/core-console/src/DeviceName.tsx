import { useState } from "react";
import type { ReactNode } from "react";
import { Pencil } from "lucide-react";
import { api, APIError } from "./api";
import { PageHead } from "./components";

export default function DeviceName({
  id,
  name,
  description,
  app,
  disabled,
  onSaved,
  action,
}: {
  id: string;
  name: string;
  description: string;
  app: boolean;
  disabled: boolean;
  onSaved: (label: string) => void;
  action?: ReactNode;
}) {
  const [editing, setEditing] = useState(false);
  const [draft, setDraft] = useState(name);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");
  return (
    <>
      <PageHead
        title={name}
        description={description}
        action={
          !editing && (
            <div className="device-heading-actions">
              <button
                className="button secondary"
                disabled={disabled}
                onClick={() => {
                  setDraft(name);
                  setError("");
                  setEditing(true);
                }}
              >
                <Pencil size={16} />
                改名
              </button>
              {action}
            </div>
          )
        }
      />
      {editing && (
        <form
          className="device-rename panel"
          onSubmit={async (event) => {
            event.preventDefault();
            const label = draft.trim();
            if (!label || new TextEncoder().encode(label).length > 120) {
              setError("请输入设备名称，最多 40 个汉字。");
              return;
            }
            setBusy(true);
            setError("");
            try {
              const result = await api<{ id: string; label: string }>(
                app
                  ? `/app/devices/${encodeURIComponent(id)}`
                  : `/nodes/${encodeURIComponent(id)}/label`,
                { method: "PUT", body: JSON.stringify({ label }) },
              );
              onSaved(result.label);
              setEditing(false);
            } catch (error) {
              setError(
                error instanceof APIError && error.status === 404
                  ? "设备已不可用，请刷新设备列表后重试。"
                  : "未能保存名称，请重试。",
              );
            } finally {
              setBusy(false);
            }
          }}
          onKeyDown={(event) => {
            if (event.key === "Escape" && !busy) {
              event.preventDefault();
              setEditing(false);
            }
          }}
        >
          <fieldset disabled={busy}>
            <label htmlFor="device-name">设备名称</label>
            <div className="device-rename-controls">
              <input
                id="device-name"
                autoFocus
                required
                maxLength={40}
                value={draft}
                onFocus={(event) => event.currentTarget.select()}
                onChange={(event) => {
                  setDraft(event.target.value);
                  setError("");
                }}
                autoComplete="off"
              />
              <button className="button primary" type="submit">
                {busy ? "保存中…" : "保存名称"}
              </button>
              <button
                className="button"
                type="button"
                onClick={() => setEditing(false)}
              >
                取消
              </button>
            </div>
          </fieldset>
          {error && (
            <p className="form-error" role="alert">
              {error}
            </p>
          )}
        </form>
      )}
    </>
  );
}
