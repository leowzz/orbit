import {
  ArrowUpRight,
  Box,
  CircleDot,
  Monitor,
  Smartphone,
  TerminalSquare,
} from "lucide-react";
import type { ReactNode } from "react";
import { Link } from "react-router-dom";
export function Mark({ small = false }: { small?: boolean }) {
  return (
    <span className={"orbit-mark " + (small ? "small" : "")} aria-hidden>
      <span />
    </span>
  );
}
export function PageHead({
  eyebrow,
  title,
  description,
  action,
}: {
  eyebrow: string;
  title: string;
  description: string;
  action?: ReactNode;
}) {
  return (
    <div className="page-head">
      <div>
        <div className="eyebrow">{eyebrow}</div>
        <h1>{title}</h1>
        <p>{description}</p>
      </div>
      {action}
    </div>
  );
}
export function Badge({
  children,
  tone = "neutral",
}: {
  children: ReactNode;
  tone?: "neutral" | "good" | "warn";
}) {
  return (
    <span className={"badge " + tone}>
      <i />
      {children}
    </span>
  );
}
export function Empty({
  title,
  children,
  action,
}: {
  title: string;
  children: ReactNode;
  action?: ReactNode;
}) {
  return (
    <div className="empty">
      <Box size={26} strokeWidth={1.2} />
      <h3>{title}</h3>
      <p>{children}</p>
      {action}
    </div>
  );
}
export function DeviceIcon({ model }: { model: string }) {
  const Icon =
    model === "android"
      ? Smartphone
      : model === "web"
        ? Monitor
        : TerminalSquare;
  return (
    <span className="device-icon">
      <Icon size={21} strokeWidth={1.4} />
    </span>
  );
}
export function SectionTitle({
  title,
  meta,
  link,
}: {
  title: string;
  meta?: string;
  link?: string;
}) {
  return (
    <div className="section-title">
      <h2>
        {title}
        <span>{meta}</span>
      </h2>
      {link && (
        <Link className="text-link" to={link}>
          查看全部 <ArrowUpRight size={14} />
        </Link>
      )}
    </div>
  );
}
export function Loading() {
  return (
    <div className="loading">
      <CircleDot className="spin" size={20} />
      正在同步网络状态…
    </div>
  );
}
