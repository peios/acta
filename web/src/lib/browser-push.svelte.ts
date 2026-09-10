import { api, errorMessage } from "./api";
type InstallEvent = Event & {
  prompt: () => Promise<void>;
  userChoice: Promise<{ outcome: string }>;
};
async function workerMessage(reg: ServiceWorkerRegistration, message: unknown) {
  const worker = reg.active || reg.waiting || reg.installing;
  if (!worker)
    throw new Error("Acta's background service is not ready. Try again.");
  await new Promise<void>((resolve, reject) => {
    const channel = new MessageChannel();
    const timer = setTimeout(() => {
      channel.port1.close();
      reject(new Error("The background service did not respond. Try again."));
    }, 5000);
    channel.port1.onmessage = () => {
      clearTimeout(timer);
      channel.port1.close();
      resolve();
    };
    worker.postMessage(message, [channel.port2]);
  });
}
export async function clearBrowserPush(owner: string) {
  try {
    localStorage.removeItem(`acta2:notifications:${owner}`);
  } catch {
    /* unavailable */
  }
  if (!("serviceWorker" in navigator)) return;
  const reg = await navigator.serviceWorker.getRegistration("/");
  if (!reg) return;
  await workerMessage(reg, { type: "clear-push" });
  await (await reg.pushManager.getSubscription())?.unsubscribe();
}
export class BrowserPush {
  enabled = $state(false);
  busy = $state(false);
  error = $state("");
  permission = $state<NotificationPermission | "unsupported">("unsupported");
  installPrompt = $state<InstallEvent | null>(null);
  standalone = $state(false);
  ios = $state(false);
  private registration: ServiceWorkerRegistration | null = null;
  private publicKey = "";
  private stopped = false;
  private initializing = false;
  private key: string;
  private install = (e: Event) => {
    e.preventDefault();
    this.installPrompt = e as InstallEvent;
  };
  constructor(owner: string) {
    this.key = `acta2:notifications:${owner}`;
  }
  async start() {
    if (this.initializing || this.stopped) return;
    this.initializing = true;
    this.busy = true;
    this.enabled = false;
    this.standalone = matchMedia("(display-mode: standalone)").matches;
    this.ios =
      /iPhone|iPad|iPod/.test(navigator.userAgent) ||
      (/Macintosh/.test(navigator.userAgent) && navigator.maxTouchPoints > 1);
    window.addEventListener("beforeinstallprompt", this.install);
    try {
      if (!window.isSecureContext || !("serviceWorker" in navigator)) return;
      await navigator.serviceWorker.register("/service-worker.js", {
        scope: "/",
        updateViaCache: "none",
      });
      this.registration = await navigator.serviceWorker.ready;
      if (typeof Notification === "undefined" || !("PushManager" in window))
        return;
      this.permission = Notification.permission;
      const config = await api<{ public_key: string }>("push");
      this.publicKey = config.public_key;
      let desired = false;
      try {
        desired = localStorage.getItem(this.key) === "on";
      } catch {}
      if (desired && this.permission === "granted" && this.publicKey)
        await this.subscribe();
    } catch (e) {
      this.error = errorMessage(e);
    } finally {
      this.initializing = false;
      this.busy = false;
    }
  }
  close() {
    this.stopped = true;
    window.removeEventListener("beforeinstallprompt", this.install);
  }
  async installApp() {
    await this.installPrompt?.prompt();
    await this.installPrompt?.userChoice;
    this.installPrompt = null;
  }
  async toggle() {
    if (this.busy || !this.registration) return;
    this.busy = true;
    this.error = "";
    try {
      if (this.enabled) {
        const sub = await this.registration.pushManager.getSubscription();
        if (sub) await api("push/unsubscribe", { endpoint: sub.endpoint });
        await workerMessage(this.registration, { type: "clear-push" });
        await sub?.unsubscribe();
        this.enabled = false;
        try {
          localStorage.removeItem(this.key);
        } catch {}
      } else {
        // Request synchronously from this user gesture, before network awaits.
        this.permission = await Notification.requestPermission();
        if (this.permission !== "granted") return;
        if (!this.publicKey)
          throw new Error("Push is not configured on this server.");
        await this.subscribe();
        try {
          localStorage.setItem(this.key, "on");
        } catch {}
      }
    } catch (e) {
      this.error = errorMessage(e);
    } finally {
      this.busy = false;
    }
  }
  private async subscribe() {
    const reg = this.registration!;
    const binary = atob(this.publicKey.replace(/-/g, "+").replace(/_/g, "/"));
    const key = Uint8Array.from(binary, (c) => c.charCodeAt(0));
    let sub = await reg.pushManager.getSubscription();
    if (sub) {
      const prior = sub.options.applicationServerKey;
      if (
        !prior ||
        new Uint8Array(prior).some((v, i) => v !== key[i]) ||
        prior.byteLength !== key.byteLength
      ) {
        await sub.unsubscribe();
        sub = null;
      }
    }
    sub ||= await reg.pushManager.subscribe({
      userVisibleOnly: true,
      applicationServerKey: key,
    });
    const json = sub.toJSON();
    const saved = await api<{ id: string }>("push/subscribe", {
      endpoint: json.endpoint,
      keys: json.keys,
    });
    if (this.stopped) return;
    await workerMessage(reg, { type: "push-config", id: saved.id });
    this.enabled = true;
  }
}
