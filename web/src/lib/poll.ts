import { writable, type Readable } from "svelte/store";

export interface Polled<T> {
  data: T | null;
  error: Error | null;
  loading: boolean;
  updatedAt: Date | null;
  paused: boolean;
}

export interface Poller<T> extends Readable<Polled<T>> {
  refresh: () => Promise<void>;
  start: () => void;
  stop: () => void;
  pause: () => void;
  resume: () => void;
  destroy: () => void;
}

export function isAbort(error: unknown): boolean {
  return error instanceof DOMException && error.name === "AbortError";
}

/**
 * polling runs a fetcher immediately and then on an interval while the tab is
 * visible. It aborts the in-flight request on refresh/destroy so a slow
 * response cannot overwrite a newer one.
 */
export function polling<T>(
  fetcher: (signal: AbortSignal) => Promise<T>,
  intervalMs = 5000,
): Poller<T> {
  const store = writable<Polled<T>>({
    data: null,
    error: null,
    loading: true,
    updatedAt: null,
    paused: false,
  });

  let timer: ReturnType<typeof setInterval> | undefined;
  let controller: AbortController | undefined;
  let running = false;

  function onVisibility(): void {
    const hidden = document.visibilityState === "hidden";
    store.update((state) => ({ ...state, paused: hidden }));
    if (!hidden) void refresh();
  }

  async function refresh(): Promise<void> {
    controller?.abort();
    controller = new AbortController();
    store.update((state) => ({ ...state, loading: state.data === null, error: null }));
    try {
      const data = await fetcher(controller.signal);
      store.update((state) => ({
        ...state,
        data,
        error: null,
        loading: false,
        updatedAt: new Date(),
      }));
    } catch (error) {
      if (isAbort(error)) return;
      store.update((state) => ({
        ...state,
        error: error instanceof Error ? error : new Error(String(error)),
        loading: false,
      }));
    }
  }

  function start(): void {
    if (running) return;
    running = true;
    store.update((state) => ({ ...state, paused: document.visibilityState === "hidden" }));
    document.addEventListener("visibilitychange", onVisibility);
    void refresh();
    timer = setInterval(() => {
      if (document.visibilityState === "visible") void refresh();
    }, intervalMs);
  }

  function stop(): void {
    if (timer) {
      clearInterval(timer);
      timer = undefined;
    }
    document.removeEventListener("visibilitychange", onVisibility);
    running = false;
    controller?.abort();
    controller = undefined;
  }

  function pause(): void {
    stop();
    store.update((state) => ({ ...state, paused: true }));
  }

  function resume(): void {
    start();
  }

  function destroy(): void {
    stop();
  }

  return { subscribe: store.subscribe, refresh, start, stop, pause, resume, destroy };
}
