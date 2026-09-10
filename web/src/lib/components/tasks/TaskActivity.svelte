<script lang="ts">
  import { onMount, tick, untrack } from "svelte";
  import ActivityChange from "./ActivityChange.svelte";
  import CommentThread from "./CommentThread.svelte";
  import CommentComposer from "./CommentComposer.svelte";
  import { emptyCommentDraft } from "$lib/comment-draft.js";

  import { api, errorMessage } from "$lib/api";
  import { loadActivityWindow } from "$lib/activity-window.js";
  import { LatestRequest } from "$lib/requests.js";
  import { useTaskRefresh } from "$lib/task-refresh-context";
  import {
    type ActivityEntry,
    type ActivityPage,
    type ActivityOrder,
  } from "$lib/activity";
  let {
    task,
    focusComment = "",
    revision,
    active,
    order,
    onunread,
    canComment,
  }: {
    task: string;
    focusComment?: string;
    canComment: boolean;
    revision: number;
    active: boolean;
    order: ActivityOrder;
    onunread: (value: boolean) => void;
  } = $props();
  let host: HTMLDivElement;
  let focusedEntry = $state<ActivityEntry | null>(null),
    focusError = $state("");
  let focusedThread = $state<CommentThread>();
  const focusRequests = new LatestRequest();
  let dismissFocus = $state("");
  // Search can target a comment outside the loaded activity window. Fetch its
  // root directly instead of loading years of intervening history.
  $effect(() => {
    const target = focusComment;
    focusedEntry = null;
    focusError = "";
    const read = focusRequests.begin();
    if (!target || dismissFocus === target) return;
    void (async () => {
      try {
        let entry = await api<ActivityEntry>(
          `tasks/${task}/comments/${encodeURIComponent(target)}`,
          undefined,
          { signal: read.signal },
        );
        if (entry.comment?.thread_id)
          entry = await api<ActivityEntry>(
            `tasks/${task}/comments/${entry.comment.thread_id}`,
            undefined,
            { signal: read.signal },
          );
        if (!read.current()) return;
        focusedEntry = entry;
        await tick();
        if (!read.current()) return;
        await focusedThread?.jumpTo(target);
      } catch (e) {
        if (read.current()) focusError = errorMessage(e);
      }
    })();
    return () => focusRequests.begin();
  });
  let readRefresh = $state(0);
  const draft = $state(emptyCommentDraft());
  let threads: Record<string, CommentThread> = {};
  let attentionID = "";
  let latestEvent = "";
  const position = (e: ActivityEntry) =>
    BigInt(e.thread_last_event || "0") > BigInt(e.last_event)
      ? e.thread_last_event!
      : e.last_event;
  async function posted(entry: ActivityEntry) {
    await load();
    attentionID = entry.id;
    await jumpLatest();
  }

  let entries = $state<ActivityEntry[]>([]),
    more = $state(false),
    cursor = $state(""),
    loading = $state(false),
    error = $state(""),
    newActivity = $state(false);
  let highlighted = $state<Record<string, boolean>>({});
  let loaded = false;
  let mounted = false,
    disposed = false,
    reading = false;
  const requests = new LatestRequest();
  const recovery = useTaskRefresh();
  let readTimer: ReturnType<typeof setTimeout>;
  const ordered = $derived(order === "asc" ? [...entries].reverse() : entries);
  const firstNew = $derived(ordered.find((e) => highlighted[e.id])?.id);
  function scrollParent(): HTMLElement {
    let element = host.parentElement;
    while (element) {
      if (/auto|scroll/.test(getComputedStyle(element).overflowY))
        return element;
      element = element.parentElement;
    }
    return document.scrollingElement as HTMLElement;
  }
  function bounds() {
    const root = scrollParent();
    const r =
      root === document.scrollingElement
        ? { top: 0, bottom: innerHeight }
        : root.getBoundingClientRect();
    const header = host
      .closest(".task-view")
      ?.querySelector("header")
      ?.getBoundingClientRect();
    return {
      top: Math.max(r.top, header?.bottom ?? 0),
      bottom: Math.min(r.bottom, innerHeight),
    };
  }
  function latestElement() {
    return host?.querySelector<HTMLElement>(
      `[data-activity-id="${attentionID || entries[0]?.id}"]`,
    );
  }
  function atLatest() {
    if (!active || !host || !entries.length) return false;
    const r = latestElement()?.getBoundingClientRect();
    const b = bounds();
    return (
      !!r &&
      (order === "asc"
        ? r.bottom <= b.bottom + 24 && r.bottom > b.top
        : r.top >= b.top - 8 && r.top < b.bottom)
    );
  }
  function anchor() {
    if (!active || !host) return null;
    const b = bounds();
    const el = Array.from(
      host.querySelectorAll<HTMLElement>("[data-activity-id]"),
    ).find((el) => el.getBoundingClientRect().bottom > b.top);
    return el
      ? { id: el.dataset.activityId!, top: el.getBoundingClientRect().top }
      : null;
  }
  function restore(saved: ReturnType<typeof anchor>) {
    if (!saved) return;
    const el = host.querySelector<HTMLElement>(
      `[data-activity-id="${saved.id}"]`,
    );
    if (el)
      scrollParent().scrollTop += el.getBoundingClientRect().top - saved.top;
  }
  export async function jumpLatest(smooth = true) {
    while (attentionID && !entries.some((e) => e.id === attentionID) && more) {
      const before = cursor;
      await load(true);
      if (cursor === before) break;
    }
    await tick();
    const target = entries.find(
      (e) => e.id === (attentionID || entries[0]?.id),
    );
    if (target?.thread_unread && threads[target.id]) {
      await threads[target.id].jumpUnread();
      newActivity = false;
      scheduleRead();
      return;
    }
    const latest = latestElement();
    if (!latest) return;
    const r = latest.getBoundingClientRect(),
      b = bounds(),
      root = scrollParent();
    root.scrollBy({
      top: order === "asc" ? r.bottom - b.bottom : r.top - b.top,

      behavior:
        smooth && !matchMedia("(prefers-reduced-motion: reduce)").matches
          ? "smooth"
          : "instant",
    });
    newActivity = false;
    scheduleRead();
  }
  async function load(older = false) {
    if (!mounted || disposed) return;
    const request = requests.begin();
    loading = true;
    const previous = new Map(entries.map((e) => [e.id, position(e)]));
    const oldest = entries.at(-1)?.first_event;
    try {
      const result = await loadActivityWindow(
        (cursor) =>
          api<ActivityPage>(
            `tasks/${task}/activity?cursor=${encodeURIComponent(cursor)}`,
            undefined,
            { signal: request.signal },
          ),
        older ? { cursor } : { oldest },
      );
      const next = result.entries;
      if (!request.current()) return;
      const saved = anchor(),
        follow = atLatest();
      const changed =
        !older &&
        loaded &&
        (next.some((e) => previous.get(e.id) !== position(e)) ||
          result.latest_event !== latestEvent);
      if (!older) {
        attentionID = result.latest_entry || next[0]?.id || "";
        latestEvent = result.latest_event || "";
      }
      loaded = true;
      entries = older
        ? [...entries, ...next.filter((e) => !previous.has(e.id))]
        : next;
      for (const e of next) if (e.unread) highlighted[e.id] = true;
      cursor = result.cursor;
      more = result.more;
      error = "";
      onunread(result.unread);
      await tick();
      if (changed && follow) await jumpLatest(false);
      else {
        restore(saved);
        if (changed) newActivity = true;
      }
      if (atLatest()) newActivity = false;
      scheduleRead();
    } catch (e) {
      if (request.current()) error = errorMessage(e);
    } finally {
      if (request.current()) loading = false;
    }
  }
  function visibleEntries() {
    if (
      !active ||
      !host ||
      document.visibilityState !== "visible" ||
      !document.hasFocus()
    )
      return [];
    const b = bounds();
    return Array.from(
      host.querySelectorAll<HTMLElement>('[data-read-id][data-unread="true"]'),
    ).filter((element) => {
      const r = element.getBoundingClientRect();
      const visible = document.elementFromPoint(
        Math.max(0, Math.min(innerWidth - 1, r.left + r.width / 2)),
        (Math.max(r.top, b.top) + Math.min(r.bottom, b.bottom)) / 2,
      );
      return (
        r.height > 0 &&
        !!visible &&
        element.contains(visible) &&
        Math.min(r.bottom, b.bottom) - Math.max(r.top, b.top) >=
          Math.min(r.height * 0.7, 120)
      );
    });
  }

  function scheduleRead() {
    clearTimeout(readTimer);
    if (!disposed) readTimer = setTimeout(() => void readVisible(), 600);
  }
  async function readVisible() {
    if (reading || disposed) return;
    const seen = visibleEntries()
      .slice(0, 50)
      .map((e) => ({ id: e.dataset.readId!, through: e.dataset.readThrough! }));
    if (!seen.length) return;
    reading = true;
    try {
      await api(`tasks/${task}/activity/read`, { entries: seen });
      if (disposed) return;
      for (const s of seen) {
        const e = entries.find((e) => e.id === s.id);
        if (e?.last_event === s.through) e.unread = false;
      }
      readRefresh++;
      await load();
    } catch (e) {
      if (!disposed) error = errorMessage(e);
    } finally {
      reading = false;
    }
  }
  $effect(() => {
    void revision;
    void recovery.recovery;
    untrack(() => {
      if (mounted) void load();
    });
  });
  $effect(() => {
    void active;
    void order;
    untrack(() => {
      if (mounted) scheduleRead();
    });
  });
  onMount(() => {
    mounted = true;
    void load();
    const refresh = () => {
      if (document.visibilityState === "visible") void load();
    };
    const scroll = () => {
      if (atLatest()) newActivity = false;
      scheduleRead();
    };
    const interval = setInterval(refresh, 15000);
    window.addEventListener("focus", refresh);
    document.addEventListener("visibilitychange", refresh);
    window.addEventListener("scroll", scroll, true);
    window.addEventListener("resize", scroll);
    return () => {
      disposed = true;
      requests.dispose();
      focusRequests.dispose();
      clearTimeout(readTimer);
      clearInterval(interval);
      window.removeEventListener("focus", refresh);
      document.removeEventListener("visibilitychange", refresh);
      window.removeEventListener("scroll", scroll, true);
      window.removeEventListener("resize", scroll);
    };
  });
</script>

<div class="activity" bind:this={host} hidden={!active}>
  {#if focusError}<p class="notice error" role="alert">{focusError}</p>{/if}
  {#if focusedEntry}<section class="search-match" aria-label="Matching comment">
      <div class="match-heading">
        <span>Matching comment</span><button
          onclick={() => {
            dismissFocus = focusComment;
            focusedEntry = null;
            void jumpLatest();
          }}>Back to latest</button
        >
      </div>
      <CommentThread
        bind:this={focusedThread}
        {task}
        entry={focusedEntry}
        {canComment}
        highlighted={true}
        refresh={readRefresh}
        onchange={() => void load()}
        onlayout={scheduleRead}
      />
    </section>{/if}
  {#if order === "desc" && canComment}<CommentComposer
      {draft}
      {task}
      onposted={(e) => void posted(e)}
    />{/if}
  {#if error}<p class="notice error" role="alert">
      {error} <button onclick={() => void load()}>Retry</button>
    </p>{/if}
  {#if order === "asc" && more}<button
      class="older"
      disabled={loading}
      onclick={() => void load(true)}
      >{loading ? "Loading…" : "Load older activity"}</button
    >{/if}
  {#if !entries.length}<p class="empty">
      {loading
        ? "Loading activity…"
        : "No activity yet. New changes will appear here."}
    </p>{/if}
  <ol aria-label="Task activity">
    {#each ordered as entry (entry.id)}
      <li
        data-activity-id={entry.id}
        data-read-id={entry.comment ? undefined : entry.id}
        data-read-through={entry.last_event}
        data-unread={entry.unread}
        class:unread={!entry.comment && highlighted[entry.id]}
      >
        {#if firstNew === entry.id}<div class="new-divider">
            <span>New activity</span>
          </div>{/if}
        {#if entry.comment}<CommentThread
            bind:this={threads[entry.id]}
            {task}
            {entry}
            {canComment}
            highlighted={highlighted[entry.id]}
            refresh={readRefresh}
            onchange={() => void load()}
            onlayout={scheduleRead}
          />{:else}
          <ActivityChange {entry} />
        {/if}
      </li>
    {/each}
  </ol>
  {#if order === "desc" && more}<button
      class="older"
      disabled={loading}
      onclick={() => void load(true)}
      >{loading ? "Loading…" : "Load older activity"}</button
    >{/if}
  {#if order === "asc" && canComment}<CommentComposer
      {draft}
      {task}
      onposted={(e) => void posted(e)}
    />{/if}
  {#if newActivity}<button
      class="new-activity"
      onclick={() => void jumpLatest()}
      >New activity {order === "asc" ? "↓" : "↑"}</button
    >{/if}
</div>

<style>
  .search-match {
    padding: 14px;
    margin: 0 0 20px;
    border: 1px solid var(--panel-border);
    border-radius: 10px;
  }
  .match-heading {
    display: flex;
    align-items: center;
    justify-content: space-between;
    font-size: 12px;
    color: var(--muted);
    margin-bottom: 12px;
  }
  .match-heading button {
    border: 0;
    background: transparent;
    color: var(--accent);
    font-size: 12px;
    padding: 4px;
  }
  .activity {
    position: relative;
    overflow-anchor: none;
    padding: 2px 0 8px;
  }
  .activity[hidden] {
    display: none;
  }
  ol {
    list-style: none;
    margin: 0;
    padding: 0;
  }
  li {
    position: relative;
    border-radius: 10px;
    transition: background 180ms ease;
  }
  li.unread {
    background: color-mix(in srgb, var(--accent) 5%, transparent);
  }
  .new-divider {
    display: flex;
    align-items: center;
    gap: 10px;
    padding: 9px 10px 0;
    color: var(--accent);
    font-size: 10px;
  }
  .new-divider::after {
    content: "";
    height: 1px;
    flex: 1;
    background: color-mix(in srgb, var(--accent) 25%, transparent);
  }
  .empty {
    color: var(--muted);
    font-size: 12px;
    line-height: 1.7;
    padding: 20px 10px;
  }
  .older {
    display: block;
    margin: 10px auto;
    padding: 7px 12px;
    background: transparent;
    border: 0;
    color: var(--muted);
    font-size: 12px;
    border-radius: 7px;
  }
  .older:hover {
    background: var(--hover-surface);
    color: var(--text);
  }
  .new-activity {
    position: sticky;
    bottom: 12px;
    display: block;
    margin: 8px auto;
    padding: 8px 14px;
    border: 1px solid var(--panel-border);
    border-radius: 20px;
    background: var(--surface);
    color: var(--accent);
    font-size: 12px;
    box-shadow: 0 3px 14px #0002;
  }
  .notice button {
    background: none;
    border: 0;
    color: inherit;
    text-decoration: underline;
  }
  @media (prefers-reduced-motion: reduce) {
    li {
      transition: none;
    }
  }
</style>
