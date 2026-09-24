import { getContext, setContext, untrack } from "svelte";
import { errorMessage, type Account } from "./api";
import type { provideCodebases } from "./codebases.svelte";
import { mergeCodeFrames } from "./code-frame-projection.js";

export type CodeProvider = "claude" | "codex";
export const codeProviderName = (provider: string) =>
  provider === "claude"
    ? "Claude Code"
    : provider === "codex"
      ? "Codex"
      : provider;

export type CodeThread = {
  id: string;
  codebase_id: string;
  root_id: string;
  provider: string;
  name: string;
  directory: string;
  state: "starting" | "ready" | "exited" | "failed" | "stopped";
  busy: boolean;
  revision: number;
  source_truncated: boolean;
  error?: string;
  created_at: string;
};
export type CATFrame = {
  seq: number;
  change_seq: number;
  supersedes?: string;
  superseded_by?: string;
  source_seq: number;
  at: string;
  type: string;
  provider: string;
  stream: string;
  raw?: string;
  level?: "trace" | "debug" | "info" | "warn" | "error" | "fatal";
  message?: string;
  target?: string;
  timestamp?: string;
  id?: string;
  input_id?: string;
  text?: string;
  delivery?: string;
  turn_id?: string;
  error?: string;
  hook?: {
    id: string;
    event: string;
    name?: string;
    status:
      | "running"
      | "completed"
      | "failed"
      | "blocked"
      | "stopped"
      | "cancelled"
      | "unknown";
    context?: { texts: string[]; source: "hook_response" | "context_item" };
    outputs: { kind: string; text: string }[];
    exit_code?: number;
    status_message?: string;
    duration_ms?: number;
    handler_type?: string;
    execution_mode?: string;
    scope?: string;
    source?: string;
    source_path?: string;
    started_at?: number;
    completed_at?: number;
  };
};
type FramePage = {
  thread: CodeThread;
  frames: CATFrame[];
  next: number;
  first: number;
  more: boolean;
  reset: boolean;
};
const key = Symbol("Acta Code Threads");

export function provideCodeThreads(
  account: () => Account,
  codebases: ReturnType<typeof provideCodebases>,
  active: () => boolean,
) {
  const state = $state({
    items: [] as CodeThread[],
    selected: "",
    frames: [] as CATFrame[],
    error: "",
    frameError: "",
    loading: false,
    dropped: false,
    reprocessing: "",
    reprocessError: "",
  });
  let generation = 0;
  let listVersion = 0;
  let reloadVersion = $state(0);
  $effect(() => {
    const owner = account().id,
      host = codebases.state.host,
      base = codebases.state.selected;
    untrack(() => {
      generation++;
      state.items = [];
      state.loading = false;
      state.selected = "";
      state.frames = [];
      state.error = "";
      state.frameError = "";
      state.dropped = false;
      state.reprocessing = "";
      state.reprocessError = "";
    });
    if (!owner || !host || !base) return;
  });
  function upsert(thread: CodeThread) {
    state.items = [
      thread,
      ...state.items.filter((t) => t.id !== thread.id),
    ].sort((a, b) => b.created_at.localeCompare(a.created_at));
  }
  async function start(id: string, root: string, provider: CodeProvider) {
    listVersion++;
    const version = generation,
      host = codebases.state.host,
      base = codebases.state.selected;
    const thread = await codebases.request<CodeThread>(host, "threads.start", {
      id,
      codebase_id: base,
      root_id: root,
      provider,
    });
    if (version === generation) {
      listVersion++;
      upsert(thread);
      state.selected = thread.id;
      state.error = "";
    }
    return thread;
  }
  async function send(threadID: string, id: string, text: string) {
    listVersion++;
    const version = generation;
    const thread = await codebases.request<CodeThread>(
      codebases.state.host,
      "threads.send",
      {
        thread_id: threadID,
        codebase_id: codebases.state.selected,
        id,
        text,
        delivery: "start_turn",
      },
    );
    if (version === generation) {
      listVersion++;
      upsert(thread);
    }
  }
  async function reprocess(threadID: string, revision: number) {
    if (state.reprocessing) return;
    const version = generation;
    state.reprocessing = threadID;
    state.reprocessError = "";
    listVersion++;
    try {
      const thread = await codebases.request<CodeThread>(
        codebases.state.host,
        "threads.reprocess",
        {
          thread_id: threadID,
          codebase_id: codebases.state.selected,
          revision,
        },
      );
      if (version !== generation) return;
      listVersion++;
      upsert(thread);
      if (state.selected === threadID) reloadVersion++;
    } catch (error) {
      if (version === generation && state.selected === threadID)
        state.reprocessError = errorMessage(error);
    } finally {
      if (version === generation) state.reprocessing = "";
    }
  }
  $effect(() => {
    const host = codebases.state.host,
      base = codebases.state.selected;
    if (!active() || !codebases.ready || !base || !host) return;
    const abort = new AbortController();
    let timer: ReturnType<typeof setTimeout>;
    async function poll() {
      if (abort.signal.aborted) return;
      const version = listVersion;
      try {
        if (document.visibilityState === "visible") {
          const result = await codebases.request<{ threads: CodeThread[] }>(
            host,
            "threads.list",
            { codebase_id: base },
            abort.signal,
          );
          if (abort.signal.aborted || version !== listVersion) return;
          state.items = result.threads;
          if (!result.threads.some((t) => t.id === state.selected))
            state.selected = result.threads[0]?.id ?? "";
          state.error = "";
        }
      } catch (error) {
        if (!abort.signal.aborted) state.error = errorMessage(error);
      } finally {
        if (!abort.signal.aborted) {
          state.loading = false;
          timer = setTimeout(() => void poll(), 3000);
        }
      }
    }
    untrack(() => {
      state.loading = true;
      void poll();
    });
    return () => {
      abort.abort();
      clearTimeout(timer);
    };
  });
  $effect(() => {
    const selected = state.selected,
      host = codebases.state.host,
      base = codebases.state.selected;
    void reloadVersion;
    untrack(() => {
      state.frames = [];
      state.frameError = "";
      state.dropped = false;
      state.reprocessError = "";
    });
    if (!active() || !codebases.ready || !selected || !base || !host) return;
    const abort = new AbortController();
    let cursor = 0,
      revision = 0,
      timer: ReturnType<typeof setTimeout>;
    async function poll() {
      if (abort.signal.aborted) return;
      let delay = 500;
      try {
        if (document.visibilityState === "visible") {
          const result = await codebases.request<FramePage>(
            host,
            "threads.read",
            { codebase_id: base, thread_id: selected, after: cursor, revision },
            abort.signal,
          );
          if (abort.signal.aborted) return;
          if (result.reset || revision !== result.thread.revision) {
            cursor = 0;
            state.frames = [];
            state.dropped = false;
          }
          revision = result.thread.revision;
          const merged = mergeCodeFrames(
            state.frames,
            result.frames,
            result.first,
          );
          state.frames = merged.frames;
          state.dropped ||= merged.dropped;
          cursor = result.next;
          upsert(result.thread);
          state.frameError = "";
          if (result.more) delay = 0;
        }
      } catch (error) {
        if (!abort.signal.aborted) {
          state.frameError = errorMessage(error);
          delay = 2000;
        }
      } finally {
        if (!abort.signal.aborted) timer = setTimeout(() => void poll(), delay);
      }
    }
    untrack(() => void poll());
    return () => {
      abort.abort();
      clearTimeout(timer);
    };
  });
  const value = { state, start, send, reprocess };
  setContext(key, value);
  return value;
}
export function useCodeThreads(): ReturnType<typeof provideCodeThreads> {
  return getContext(key);
}
