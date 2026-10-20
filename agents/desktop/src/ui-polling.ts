import { useEffect, useRef, useState } from "react";
import { isTauri } from "@tauri-apps/api/core";
import { listen } from "@tauri-apps/api/event";
import { getCurrentWindow } from "@tauri-apps/api/window";
import { startPolling } from "./polling";

export function useWindowVisible() {
  const [visible, setVisible] = useState(!isTauri() && !document.hidden);
  useEffect(() => {
    let alive = true;
    let revision = 0;
    let nativeVisible = !isTauri();
    let unlisten: (() => void) | undefined;
    const publish = () => {
      if (alive) setVisible(nativeVisible && !document.hidden);
    };
    const read = async () => {
      const version = ++revision;
      if (isTauri()) {
        const window = getCurrentWindow();
        const [shown, minimized] = await Promise.all([
          window.isVisible(),
          window.isMinimized(),
        ]);
        if (!alive || version !== revision) return;
        nativeVisible = shown && !minimized;
      }
      publish();
    };
    const changed = () => {
      publish();
      if (!document.hidden) void read().catch(() => {});
    };
    document.addEventListener("visibilitychange", changed);
    void (async () => {
      if (isTauri()) {
        const stop = await listen<boolean>(
          "orbit:ui-visible",
          ({ payload }) => {
            ++revision;
            nativeVisible = payload;
            publish();
          },
        );
        if (!alive) return stop();
        unlisten = stop;
      }
      await read();
    })().catch(() => {});
    return () => {
      alive = false;
      unlisten?.();
      document.removeEventListener("visibilitychange", changed);
    };
  }, []);
  return visible;
}

export function useVisiblePolling<T>(
  visible: boolean,
  read: () => Promise<T>,
  receive: (value: T) => void,
  interval: number,
) {
  const latest = useRef({ read, receive });
  latest.current = { read, receive };
  useEffect(() => {
    if (!visible) return;
    return startPolling(
      () => latest.current.read(),
      (value) => latest.current.receive(value),
      interval,
    );
  }, [visible, interval]);
}
