import type { Shortcut } from "./types";

export function mergeShortcuts(
  items: Shortcut[],
  added: Shortcut[],
): Shortcut[] {
  const next = [...items];
  for (const item of added) {
    if (!next.some((i) => i.kind === item.kind && i.target === item.target))
      next.push(item);
  }
  if (next.length > 24) throw new Error("最多添加 24 个快捷入口");
  return next;
}
export function moveShortcut(
  items: Shortcut[],
  id: string,
  over: string,
): Shortcut[] {
  const from = items.findIndex((i) => i.id === id);
  const to = items.findIndex((i) => i.id === over);
  if (from < 0 || to < 0 || from === to) return items;
  const next = [...items];
  next.splice(to, 0, next.splice(from, 1)[0]);
  return next;
}

export type WebsiteInfo = {
  icon: string;
  title: string;
  warning: string | null;
};

export function applyWebsiteInfo(
  current: Shortcut,
  requested: Shortcut,
  info: WebsiteInfo,
): Shortcut {
  if (current.id !== requested.id || current.target !== requested.target)
    return current;
  return {
    ...current,
    icon:
      current.icon === requested.icon && info.icon ? info.icon : current.icon,
    title:
      !requested.title.trim() && !current.title.trim() && info.title
        ? info.title
        : current.title,
  };
}

export function isShortcutUrl(value: string): boolean {
  if (
    !value ||
    value.trim() !== value ||
    /[\u0000-\u001f\u007f-\u009f]/.test(value) ||
    new TextEncoder().encode(value).length > 4096
  )
    return false;
  try {
    new URL(value);
    return true;
  } catch {
    return false;
  }
}

export function canFetchWebsiteInfo(value: string): boolean {
  try {
    const url = new URL(value.trim());
    return (
      ["http:", "https:"].includes(url.protocol) &&
      !!url.hostname &&
      !url.username &&
      !url.password
    );
  } catch {
    return false;
  }
}
