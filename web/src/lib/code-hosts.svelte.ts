import { getContext, setContext } from "svelte";
import { api, errorMessage, type Account } from "./api";

export type CodeHost = {
  id: string;
  name: string;
  os: string;
  arch: string;
  online: boolean;
  connected: boolean;
  last_seen_at: string;
  provider_status?: {
    checked_at: string;
    providers: CodeProvider[];
  };
};

export type CodeProvider = {
  id: "claude" | "codex";
  installed: boolean;
  auth: "signed_in" | "signed_out" | "unknown";
};

const key = Symbol("private Code hosts");
export function provideCodeHosts(
  account: () => Account,
  active: () => boolean,
) {
  const state = $state({
    hosts: [] as CodeHost[],
    selected: "",
    loading: true,
    error: "",
  });
  $effect(() => {
    const owner = account().id;
    const agent = account().owner_id;
    state.hosts = [];
    state.selected = "";
    state.error = "";
    state.loading = true;
    // Nested effect retains selection across scope changes, but resets all
    // account data and aborts outstanding reads when identity changes.
    $effect(() => {
      if (!active() || !owner) return;
      if (agent) {
        state.loading = false;
        state.error = "Code hosts are available with your human account.";
        return;
      }
      const abort = new AbortController();
      let reading = false;
      async function refresh() {
        if (reading || document.visibilityState !== "visible") return;
        reading = true;
        try {
          const result = await api<{ hosts: CodeHost[] }>(
            "code/hosts",
            undefined,
            {
              signal: AbortSignal.any([
                abort.signal,
                AbortSignal.timeout(5000),
              ]),
            },
          );
          if (abort.signal.aborted) return;
          state.hosts = result.hosts;
          if (!result.hosts.some((host) => host.id === state.selected))
            state.selected =
              (result.hosts.find((host) => host.online) ?? result.hosts[0])
                ?.id ?? "";
          state.error = "";
        } catch (error) {
          if (!abort.signal.aborted) state.error = errorMessage(error);
        } finally {
          reading = false;
          if (!abort.signal.aborted) state.loading = false;
        }
      }
      void refresh();
      const interval = setInterval(() => void refresh(), 3000);
      const wake = () => void refresh();
      document.addEventListener("visibilitychange", wake);
      window.addEventListener("online", wake);
      return () => {
        abort.abort();
        clearInterval(interval);
        document.removeEventListener("visibilitychange", wake);
        window.removeEventListener("online", wake);
      };
    });
  });
  setContext(key, state);
  return state;
}

export function useCodeHosts(): ReturnType<typeof provideCodeHosts> {
  return getContext(key);
}
