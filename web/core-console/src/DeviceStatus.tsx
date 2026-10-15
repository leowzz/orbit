import { useId } from "react";
import type { AppDevice } from "./api";
import { appConnectionLabel } from "./api";

const activityDate = new Intl.DateTimeFormat("zh-CN", {
  year: "numeric",
  month: "2-digit",
  day: "2-digit",
  hour: "2-digit",
  minute: "2-digit",
  second: "2-digit",
  hour12: false,
});

export default function DeviceStatus({
  app,
  archived = false,
  discovered = false,
  reportedAt,
}: {
  app?: AppDevice;
  archived?: boolean;
  discovered?: boolean;
  reportedAt?: string;
}) {
  const tooltipId = useId();
  const connection = app ? appConnectionLabel(app.activity) : "";
  const status = archived
    ? "archived"
    : app?.revoked
      ? "revoked"
      : app
        ? connection === "已连接"
          ? "connected"
          : connection === "最近活跃"
            ? "active"
            : "offline"
        : discovered
          ? "discovered"
          : "pending";
  const label = {
    connected: "已连接",
    active: "最近活跃",
    offline: "未连接",
    discovered: "已发现",
    pending: "预配置",
    archived: "已归档",
    revoked: "已撤销访问",
  }[status];
  const timestamp = app ? app.activity.last_seen : reportedAt;
  const hasTime = !!timestamp && Number.isFinite(Date.parse(timestamp));

  return (
    <span
      className="device-status-wrap"
      tabIndex={0}
      aria-describedby={tooltipId}
    >
      <span className={`badge device-status ${status}`}>
        <i aria-hidden="true" />
        {label}
      </span>
      <span className="device-status-tooltip" role="tooltip" id={tooltipId}>
        <span>
          {app
            ? hasTime
              ? "最近活跃"
              : "暂无活跃记录"
            : hasTime
              ? "最近设备报告"
              : "暂无设备报告"}
        </span>
        {hasTime && (
          <time dateTime={timestamp}>
            {activityDate.format(new Date(timestamp!))}
          </time>
        )}
        <small>
          {app
            ? hasTime
              ? "以最近访问 Core 的时间为准。"
              : "本次服务运行尚无访问记录。"
            : hasTime
              ? "报告时间不代表当前在线。"
              : "接入后可查看设备报告时间。"}
        </small>
      </span>
    </span>
  );
}
