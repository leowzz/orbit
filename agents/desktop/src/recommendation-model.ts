import type { Shortcut } from "./types";

export type Recommendations = {
  supported: boolean;
  items: { app: Shortcut; lastUsed: number; iconKey: string }[];
};

export function knownIcons(previous: Recommendations | null) {
  return Object.fromEntries(
    previous?.items.map(({ app, iconKey }) => [app.target, iconKey]) ?? [],
  );
}

export function mergeRecommendations(
  previous: Recommendations | null,
  incoming: Recommendations,
): Recommendations {
  const cached = new Map(
    previous?.items.map((item) => [item.app.target, item]),
  );
  const items = incoming.items.map((item) => {
    const old = cached.get(item.app.target);
    const app = {
      ...item.app,
      icon: old?.iconKey === item.iconKey ? old.app.icon : item.app.icon,
    };
    if (
      old &&
      old.iconKey === item.iconKey &&
      old.lastUsed === item.lastUsed &&
      old.app.id === app.id &&
      old.app.title === app.title &&
      old.app.kind === app.kind &&
      old.app.target === app.target
    )
      return old;
    return { ...item, app };
  });
  const result = { ...incoming, items };
  return previous?.supported === result.supported &&
    previous.items.length === items.length &&
    previous.items.every((item, index) => item === items[index])
    ? previous
    : result;
}
