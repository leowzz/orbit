export default function AutoSaveStatus({
  saving,
  dirty,
  changed,
  pending = false,
  empty = false,
}: {
  saving: boolean;
  dirty: boolean;
  changed: boolean;
  pending?: boolean;
  empty?: boolean;
}) {
  return (
    <p className="auto-save-status" role="status" aria-live="polite">
      {saving
        ? "正在保存…"
        : dirty
          ? "等待自动保存…"
          : pending
            ? "已保存，连接恢复后同步"
            : empty
              ? "选择内容和来源后自动同步"
              : changed
                ? "已自动保存"
                : "更改后自动保存"}
    </p>
  );
}
