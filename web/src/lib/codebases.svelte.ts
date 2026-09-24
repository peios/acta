import { getContext, setContext, untrack } from "svelte";
import { api, errorMessage, type Account } from "./api";
import type { provideCodeHosts } from "./code-hosts.svelte";

export type CodebaseRoot = { id: string; path: string };
export type Codebase = {
  id: string;
  name: string;
  roots: CodebaseRoot[];
  created_at: string;
};
export type DirectoryEntry = {
  name: string;
  kind: "directory" | "file" | "symlink" | "other";
};
export type Directory = { entries: DirectoryEntry[]; truncated: boolean };
const key = Symbol("Code codebases");

export function provideCodebases(
  account: () => Account,
  hosts: ReturnType<typeof provideCodeHosts>,
  active: () => boolean,
) {
  const state = $state({
    host: "",
    items: [] as Codebase[],
    selected: "",
    loading: false,
    error: "",
    refreshVersion: 0,
    selectedFile: "",
  });
  let abort = new AbortController();
  let revision = 0;
  const ready = $derived(
    !hosts.error &&
      hosts.hosts.some((host) => host.id === hosts.selected && host.connected),
  );
  $effect(() => {
    const owner = account().id,
      host = hosts.selected;
    untrack(() => {
      abort.abort();
      abort = new AbortController();
      revision++;
      state.host = owner ? host : "";
      state.items = [];
      state.selected = "";
      state.selectedFile = "";
      state.error = "";
      state.loading = false;
      state.refreshVersion++;
    });
    return () => abort.abort();
  });
  async function request<T>(
    host: string,
    method: string,
    params: unknown,
    signal?: AbortSignal,
  ) {
    if (!host || host !== state.host)
      throw new Error("Select a connected host first.");
    return api<T>(
      `code/hosts/${encodeURIComponent(host)}/request`,
      { method, params },
      {
        signal: AbortSignal.any([
          abort.signal,
          AbortSignal.timeout(12000),
          ...(signal ? [signal] : []),
        ]),
      },
    );
  }
  async function refresh() {
    const host = state.host,
      version = ++revision;
    if (!host) return;
    state.loading = true;
    try {
      const result = await request<{ codebases: Codebase[] }>(
        host,
        "codebases.list",
        {},
      );
      if (host !== state.host || version !== revision) return;
      state.items = result.codebases;
      if (!state.items.some((item) => item.id === state.selected))
        state.selected = state.items[0]?.id ?? "";
      state.error = "";
    } catch (error) {
      if (host === state.host && version === revision && !abort.signal.aborted)
        state.error = errorMessage(error);
    } finally {
      if (host === state.host && version === revision) state.loading = false;
    }
  }
  $effect(() => {
    const host = hosts.selected;
    if (active() && ready && host)
      untrack(() => {
        state.refreshVersion++;
        void refresh();
      });
  });
  async function add(
    host: string,
    params: { id: string; name: string; paths: string[] },
    signal?: AbortSignal,
  ) {
    const identity = abort;
    revision++;
    state.loading = false;
    const result = await request<Codebase>(
      host,
      "codebases.add",
      params,
      signal,
    );
    if (state.host === host && abort === identity && !identity.signal.aborted) {
      revision++;
      state.items = [
        ...state.items.filter((item) => item.id !== result.id),
        result,
      ];
      state.selected = result.id;
      state.error = "";
      state.loading = false;
    }
    return result;
  }
  const value = {
    state,
    get ready() {
      return ready;
    },
    request,
    refresh,
    add,
  };
  setContext(key, value);
  return value;
}
export function useCodebases(): ReturnType<typeof provideCodebases> {
  return getContext(key);
}
