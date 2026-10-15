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
