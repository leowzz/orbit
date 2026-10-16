import { useEffect, useRef, useState, useSyncExternalStore } from "react";
import { AutoSave } from "./autoSave";

export default function useAutoSave<T>(
  initial: T | null,
  write: (value: T, saved: T | null) => Promise<T>,
  equal?: (a: T | null, b: T | null) => boolean,
) {
  const writeRef = useRef(write);
  writeRef.current = write;
  const [queue] = useState(
    () =>
      new AutoSave(
        initial,
        (value, saved) => writeRef.current(value, saved),
        equal,
      ),
  );
  const state = useSyncExternalStore(queue.subscribe, queue.getSnapshot);
  useEffect(() => {
    const leave = (event: BeforeUnloadEvent) => {
      const current = queue.getSnapshot();
      if (current.dirty || current.saving) {
        event.preventDefault();
        event.returnValue = "";
      }
    };
    window.addEventListener("beforeunload", leave);
    return () => {
      window.removeEventListener("beforeunload", leave);
      void queue.flush();
    };
  }, [queue]);
  return { ...state, queue };
}
