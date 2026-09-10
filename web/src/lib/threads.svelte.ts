import { getContext, setContext, onMount } from "svelte";
import { api, errorMessage } from "./api";
export type Thread = {
  name?: string;
  name_sync_pending?: boolean;
  name_sync_error?: string;
  id: string;
  provider: string;
  cwd: string;
  provider_id: string;
  run_id: string;
  state: string;
  error?: string;
  created_at: string;
  revision: number;
  committed: boolean;
  connection_id?: string;
};
export type ThreadFrame = {
  lane_id?: string;
  thread_id: string;
  run_id: string;
  sequence: number;
  output_index: number;
  received_at: string;
  provider: string;
  kind: string;
  raw_json: string;
  approval_resolved?: boolean;
  data?: Record<string, unknown>;
};
const key = Symbol("threads");
export function threadName(t: Thread) {
  return t.name || t.cwd.split(/[\\/]/).filter(Boolean).at(-1) || "New thread";
}
export class ThreadView {
  items = $state<Thread[]>([]);
  error = $state("");
  createOpen = $state(false);
  notice = $state("");
  pending = $state<{ id: string; connection: string; started: number } | null>(
    null,
  );
  private refreshVersion = 0;
  forget(id: string) {
    ++this.refreshVersion;
    this.items = this.items.filter((thread) => thread.id !== id);
  }
  async refresh(signal?: AbortSignal) {
    const version = ++this.refreshVersion;
    try {
      const data = await api<{ threads: Thread[] }>("threads", undefined, {
        signal,
      });
      if (signal?.aborted || version !== this.refreshVersion) return;
      this.items = data.threads;
      this.error = "";
    } catch (e) {
      if (!signal?.aborted && version === this.refreshVersion)
        this.error = errorMessage(e);
    }
  }
}
export function provideThreads() {
  const view = new ThreadView();
  setContext(key, view);
  onMount(() => {
    const abort = new AbortController();
    let timer: ReturnType<typeof setTimeout>;
    const run = async () => {
      if (window.location.pathname.startsWith("/my-agents") || view.pending)
        await view.refresh(abort.signal);
      if (!abort.signal.aborted) timer = setTimeout(run, 1000);
    };
    void run();
    return () => {
      abort.abort();
      clearTimeout(timer);
    };
  });
  return view;
}
export function useThreads() {
  return getContext<ThreadView>(key);
}
