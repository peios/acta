import { getContext, setContext, onMount } from "svelte";
import { goto } from "$app/navigation";
import { BrowserPush } from "./browser-push.svelte";
import { api, errorMessage, type Account } from "./api";
export type Notice = {
  task_id?: string;
  task_title?: string;
  task_reference?: string;
  workspace_slug?: string;
  activity_id?: string;
  id: string;
  revision: number;
  thread_id: string;
  lane_id: string;
  run_id: string;
  sequence: number;
  kind: string;
  title: string;
  thread_name: string;
  created_at: string;
};
type Inbox = { items: Notice[]; counts: Record<string, number>; total: number };
type Viewing = {
  thread: string;
  lane: string;
  sequence: number;
  ready: boolean;
};
const key = Symbol("notifications");
export function noticeURL(n: Notice) {
  if (n.task_id && n.workspace_slug)
    return `/workspaces/${encodeURIComponent(n.workspace_slug)}?task=${encodeURIComponent(n.task_id)}`;
  return `/my-agents/${encodeURIComponent(n.thread_id)}${n.lane_id ? `?lane=${encodeURIComponent(n.lane_id)}` : ""}`;
}
export class NotificationView {
  inbox = $state<Inbox>({ items: [], counts: {}, total: 0 });
  error = $state("");
  push: BrowserPush;
  get enabled() {
    return this.push.enabled;
  }
  get permission() {
    return this.push.permission;
  }
  reading = $state(false);
  viewing = $state<Viewing | null>(null);
  private version = 0;
  private abort = new AbortController();
  constructor(owner: string) {
    this.push = new BrowserPush(owner);
  }
  start() {
    void this.push.start();
  }
  close() {
    this.abort.abort();
    this.push.close();
  }
  private focused() {
    return document.visibilityState === "visible" && document.hasFocus();
  }
  async browserAlerts() {
    await this.push.toggle();
  }
  async refresh() {
    if (this.reading || this.abort.signal.aborted) return;
    const version = ++this.version;
    try {
      const inbox = await api<Inbox>("notifications", undefined, {
        signal: AbortSignal.any([
          this.abort.signal,
          AbortSignal.timeout(10000),
        ]),
      });
      if (this.abort.signal.aborted || version !== this.version) return;
      this.inbox = inbox;
      this.error = "";
      await this.readViewed();
    } catch (e) {
      if (!this.abort.signal.aborted && version === this.version)
        this.error = errorMessage(e);
    }
  }
  async read(items: Notice[]) {
    if (!items.length || this.reading || this.abort.signal.aborted) return;
    this.reading = true;
    ++this.version; // Discard reads started before this acknowledgement.
    try {
      await api(
        "notifications/read",
        { items: items.map(({ id, revision }) => ({ id, revision })) },
        {
          signal: AbortSignal.any([
            this.abort.signal,
            AbortSignal.timeout(10000),
          ]),
        },
      );
      // Acknowledge exactly the displayed revisions, never unseen concurrent events.
      const ids = new Set(items.map((n) => `${n.id}:${n.revision}`));
      const counts = { ...this.inbox.counts };
      let total = this.inbox.total;
      const remaining = this.inbox.items.filter((n) => {
        if (!ids.has(`${n.id}:${n.revision}`)) return true;
        const target = n.task_id || n.thread_id;
        counts[target] = Math.max(0, (counts[target] || 0) - 1);
        total = Math.max(0, total - 1);

        return false;
      });
      this.inbox = { items: remaining, counts, total };
      this.error = "";
    } catch (e) {
      if (!this.abort.signal.aborted) this.error = errorMessage(e);
    } finally {
      ++this.version;
      this.reading = false;
    }
  }
  async readViewed() {
    const v = this.viewing;
    if (!v?.ready || !this.focused()) return;
    await this.read(
      this.inbox.items.filter(
        (n) =>
          n.thread_id === v.thread &&
          n.lane_id === v.lane &&
          n.sequence <= v.sequence,
      ),
    );
  }
  async open(n: Notice) {
    await goto(noticeURL(n));
    await this.read([n]);
  }
}
export function provideNotifications(account: () => Account) {
  const view = new NotificationView(account().id);
  setContext(key, view);
  onMount(() => {
    view.start();
    let timer: ReturnType<typeof setTimeout>;
    let closed = false;
    const run = async () => {
      await view.refresh();
      if (!closed) timer = setTimeout(run, 2000);
    };
    const focused = () => void view.readViewed();
    const storage = () => view.start();
    window.addEventListener("focus", focused);
    document.addEventListener("visibilitychange", focused);
    window.addEventListener("storage", storage);
    void run();
    return () => {
      closed = true;
      clearTimeout(timer);
      view.close();
      window.removeEventListener("focus", focused);
      document.removeEventListener("visibilitychange", focused);
      window.removeEventListener("storage", storage);
    };
  });
  return view;
}
export function useNotifications() {
  return getContext<NotificationView | undefined>(key);
}
