import { api, APIError, errorMessage } from "./api";

export type ProviderStatus = {
  id: "codex" | "claude";
  installation: "checking" | "installed" | "not_installed" | "unknown";
  version?: string;
  authentication: "unknown" | "signed_in" | "signed_out";
  issue?: "timeout" | "probe_failed" | "version_unknown" | "auth_unavailable";
};
export type HarnessConnection = {
  results?: { id: string; thread_id: string; error?: string }[];
  providers: ProviderStatus[];
  id: string;
  hostname: string;
  connected_at: string;
};
export type HarnessView = {
  state: "connecting" | "live" | "reconnecting" | "error";
  connections: HarnessConnection[];
  message: string;
};

// Presence has no offline cache: once the stream is lost, its list is no longer
// authoritative. Every reattachment receives a complete owner-scoped snapshot.
export function watchHarnesses(update: (view: HarnessView) => void) {
  let disposed = false;
  let socket: WebSocket | undefined;
  let retry: ReturnType<typeof setTimeout>;
  let watchdog: ReturnType<typeof setTimeout>;
  let delay = 1000;
  let receivedAt = 0;
  let hasConnected = false;
  const abort = new AbortController();
  const emit = (state: HarnessView["state"], message = "") =>
    update({ state, connections: [], message });

  async function connect() {
    if (disposed) return;
    emit(hasConnected ? "reconnecting" : "connecting");
    try {
      // Normal HTTP supplies meaningful auth errors and the application's usual
      // logout/MFA/disabled redirects; browser WS handshakes hide HTTP details.
      await api("harnesses", undefined, {
        signal: AbortSignal.any([abort.signal, AbortSignal.timeout(10000)]),
      });
    } catch (e) {
      if (disposed) return;
      if (
        e instanceof APIError &&
        e.status >= 400 &&
        e.status < 500 &&
        e.status !== 429
      ) {
        emit("error", errorMessage(e));
        return;
      }
      schedule();
      return;
    }
    if (disposed) return;
    const url = new URL("/api/harnesses/live", window.location.href);
    url.protocol = url.protocol === "https:" ? "wss:" : "ws:";
    const current = new WebSocket(url, "acta-harness-v7");
    socket = current;
    const started = Date.now();
    let terminal = false;
    const arm = (ms: number) => {
      clearTimeout(watchdog);
      watchdog = setTimeout(disconnect, ms);
    };
    arm(10000);
    current.onmessage = (event) => {
      if (disposed || socket !== current) return;
      try {
        const value = JSON.parse(event.data);
        receivedAt = Date.now();
        arm(40000);
        if (value.type === "snapshot" && Array.isArray(value.connections)) {
          hasConnected = true;
          update({
            state: "live",
            connections: value.connections,
            message: "",
          });
        } else if (value.type === "error") {
          // A reconnect performs the normal HTTP authorization check again.
          disconnect();
        } else if (value.type !== "heartbeat") {
          terminal = true;
          emit(
            "error",
            "The harness connection protocol has changed. Reload this page.",
          );
          current.close();
        }
      } catch {
        terminal = true;
        emit(
          "error",
          "Acta returned an unexpected harness update. Reload this page.",
        );
        current.close();
      }
    };
    current.onclose = (event) => {
      if (disposed || socket !== current) return;
      socket = undefined;
      clearTimeout(watchdog);
      if (terminal) return;
      if (event.code === 1002) {
        emit(
          "error",
          "The harness connection protocol has changed. Reload this page.",
        );
        return;
      }
      if (Date.now() - started >= 30000) delay = 1000;
      schedule();
    };
  }
  function schedule() {
    if (disposed) return;
    emit("reconnecting");
    retry = setTimeout(
      () => void connect(),
      delay * (0.5 + Math.random() * 0.5),
    );
    delay = Math.min(30000, delay * 2);
  }
  function disconnect() {
    if (!socket || disposed) return;
    const previous = socket;
    socket = undefined;
    clearTimeout(watchdog);
    previous.close();
    schedule();
  }
  function wake() {
    if (socket && receivedAt && Date.now() - receivedAt > 40000) disconnect();
  }
  window.addEventListener("focus", wake);
  void connect();
  return () => {
    disposed = true;
    abort.abort();
    clearTimeout(retry);
    clearTimeout(watchdog);
    socket?.close();
    window.removeEventListener("focus", wake);
  };
}
