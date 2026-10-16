export type AutoSaveState<T> = {
  value: T | null;
  dirty: boolean;
  saving: boolean;
  error: unknown;
  changed: boolean;
};

// Keep one request in flight; edits made during it become the next request.
export class AutoSave<T> {
  private saved: T | null;
  private value: T | null;
  private version = 0;
  private uncertain = false;
  private timer: ReturnType<typeof setTimeout> | undefined;
  private request: Promise<void> | undefined;
  private listeners = new Set<() => void>();
  private state: AutoSaveState<T>;
  private write: (value: T, saved: T | null) => Promise<T>;
  private equal: (a: T | null, b: T | null) => boolean;

  constructor(
    initial: T | null,
    write: (value: T, saved: T | null) => Promise<T>,
    equal = (a: T | null, b: T | null) =>
      JSON.stringify(a) === JSON.stringify(b),
  ) {
    this.saved = this.value = initial;
    this.write = write;
    this.equal = equal;
    this.state = {
      value: initial,
      dirty: false,
      saving: false,
      error: null,
      changed: false,
    };
  }

  getSnapshot = () => this.state;
  subscribe = (listener: () => void) => {
    this.listeners.add(listener);
    return () => {
      this.listeners.delete(listener);
    };
  };
  private emit(patch: Partial<AutoSaveState<T>> = {}) {
    this.state = {
      ...this.state,
      ...patch,
      value: this.value,
      dirty: this.uncertain || !this.equal(this.value, this.saved),
    };
    this.listeners.forEach((listener) => listener());
  }
  private cancelTimer() {
    clearTimeout(this.timer);
    this.timer = undefined;
  }
  set = (value: T, delay = 350, force = false) => {
    this.cancelTimer();
    this.value = value;
    this.uncertain ||= force;
    this.version++;
    this.emit({ error: null, changed: true });
    if (this.state.dirty && !this.request)
      this.timer = setTimeout(() => {
        void this.flush();
      }, delay);
  };
  reset = (value: T) => {
    if (this.request) return;
    this.cancelTimer();
    this.saved = this.value = value;
    this.uncertain = false;
    this.version++;
    this.emit({ error: null, changed: false });
  };
  retry = () => {
    this.emit({ error: null });
    return this.flush();
  };
  flush = (): Promise<void> => {
    this.cancelTimer();
    if (this.request) return this.request.then(() => this.flush());
    if (!this.state.dirty || this.state.error || this.value === null)
      return Promise.resolve();
    const sent = this.value,
      saved = this.saved,
      version = this.version;
    this.emit({ saving: true });
    this.request = Promise.resolve()
      .then(() => this.write(sent, saved))
      .then(
        (result) => {
          this.saved = result;
          this.uncertain = false;
          if (version === this.version) this.value = result;
          this.emit({ error: null });
        },
        (error: unknown) => {
          this.uncertain = true;
          this.emit({ error });
        },
      )
      .finally(() => {
        this.request = undefined;
        this.emit({ saving: false });
        if (this.state.dirty && !this.state.error) void this.flush();
      });
    return this.request;
  };
}
