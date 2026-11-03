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
import { Badge as AdminBadge } from "./components/ui/badge";
export function Mark({ small = false }: { small?: boolean }) {
  return (
    <img
      className={"orbit-mark " + (small ? "small" : "")}
      src="/orbit-icon.svg"
      alt=""
      aria-hidden
    />
  );
}
export function PageHead({
  title,
  description,
  action,
}: {
  title: string;
  description: string;
  action?: ReactNode;
}) {
  return (
    <div className="page-head">
      <div>
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
    <AdminBadge
      variant="outline"
      className={
        tone === "good"
          ? "border-transparent bg-emerald-50 text-emerald-700 dark:bg-emerald-950 dark:text-emerald-300"
          : tone === "warn"
            ? "border-transparent bg-amber-50 text-amber-800 dark:bg-amber-950 dark:text-amber-300"
            : "border-transparent bg-secondary text-muted-foreground"
      }
    >
      <i />
      {children}
    </AdminBadge>
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
