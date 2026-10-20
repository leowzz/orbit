// Schedule after completion so slow IPC cannot build up overlapping requests.
export function startPolling<T>(
  read: () => Promise<T>,
  receive: (value: T) => void,
  interval: number,
  failed: (error: unknown) => void = () => {},
) {
  let active = true;
  let timer: ReturnType<typeof setTimeout>;
  async function poll() {
    try {
      const value = await read();
      if (active) receive(value);
    } catch (error) {
      if (active) failed(error);
    } finally {
      if (active) timer = setTimeout(poll, interval);
    }
  }
  void poll();
  return () => {
    active = false;
    clearTimeout(timer);
  };
}
