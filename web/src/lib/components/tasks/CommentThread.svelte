<script lang="ts">
  import { onDestroy, tick, untrack } from "svelte";
  import { api, errorMessage } from "$lib/api";
  import type { ActivityEntry, ActivityPage } from "$lib/activity";
  import { LatestRequest } from "$lib/requests.js";
  import { loadActivityWindow } from "$lib/activity-window.js";
  import CommentItem from "./CommentItem.svelte";
  import CommentComposer from "./CommentComposer.svelte";
  let {
    task,
    entry,
    canComment,
    highlighted,
    refresh,
    onchange,
    onlayout,
  }: {
    task: string;
    entry: ActivityEntry;
    canComment: boolean;
    highlighted: boolean;
    refresh: number;
    onchange: () => void;
    onlayout: () => void;
  } = $props();
  let expanded = $state(false),
    replies = $state<ActivityEntry[]>([]),
    more = $state(false),
    cursor = $state(""),
    error = $state(""),
    loading = $state(false),
    target = $state<ActivityEntry | null>(null);
  let highlights = $state<Record<string, boolean>>({});
  let host: HTMLDivElement;
  const requests = new LatestRequest();
  function preservePosition() {
    const feed = host?.closest(".activity");
    if (!feed) return () => {};
    let scroller = host.parentElement;
    while (
      scroller &&
      !/auto|scroll/.test(getComputedStyle(scroller).overflowY)
    )
      scroller = scroller.parentElement;
    const root = scroller || (document.scrollingElement as HTMLElement);
    const top = Math.max(
      0,
      root.getBoundingClientRect().top,
      host
        .closest(".task-view")
        ?.querySelector("header")
        ?.getBoundingClientRect().bottom || 0,
    );
    const visible = Array.from(
      feed.querySelectorAll<HTMLElement>("[data-read-id]"),
    ).find((e) => e.getBoundingClientRect().bottom > top);
    if (!visible) return () => {};
    const before = visible.getBoundingClientRect().top;
    return () => {
      if (visible.isConnected)
        root.scrollTop += visible.getBoundingClientRect().top - before;
    };
  }
  async function load(older = false) {
    const request = requests.begin();
    loading = true;
    try {
      const p = await loadActivityWindow(
        (cursor) =>
          api<ActivityPage>(
            `tasks/${task}/comments/${entry.id}/replies?cursor=${encodeURIComponent(cursor)}`,
            undefined,
            { signal: request.signal },
          ),
        older ? { cursor } : { oldest: replies.at(-1)?.first_event },
      );
      if (!request.current()) return;
      const restore = preservePosition();
      replies = older
        ? [
            ...replies,
            ...p.entries.filter((e) => !replies.some((r) => r.id === e.id)),
          ]
        : p.entries;
      more = p.more;
      cursor = p.cursor;
      for (const r of p.entries) if (r.unread) highlights[r.id] = true;
      error = "";
      await tick();
      restore();
      onlayout();
    } catch (e) {
      if (request.current()) error = errorMessage(e);
    } finally {
      if (request.current()) loading = false;
    }
  }
  function reply(e: ActivityEntry) {
    target = e;
    expanded = true;
  }
  async function changed() {
    await load();
    onchange();
  }
  async function posted(e: ActivityEntry) {
    target = null;
    await changed();
    await jump(e.id);
  }
  async function jump(id: string) {
    expanded = true;
    // Let the expansion effect run before the awaited load owns the request.
    await tick();
    await load();
    while (id !== entry.id && !replies.some((r) => r.id === id) && more) {
      const before = cursor;
      await load(true);
      if (cursor === before) break;
    }
    await tick();
    const el = host.querySelector<HTMLElement>(`[data-read-id="${id}"]`);
    el?.scrollIntoView({
      block: "nearest",
      behavior: matchMedia("(prefers-reduced-motion: reduce)").matches
        ? "instant"
        : "smooth",
    });
    el?.focus({ preventScroll: true });
    onlayout();
  }
  export async function jumpTo(id: string) {
    highlights[id] = true;
    await jump(id);
  }
  export async function jumpUnread() {
    expanded = true;
    await tick();
    await load();
    while (!replies.some((e) => e.unread) && more) {
      const before = cursor;
      await load(true);
      if (cursor === before) break;
    }
    const next = [...replies].reverse().find((e) => e.unread) || replies[0];
    if (next) await jump(next.id);
  }
  $effect(() => {
    void entry.thread_last_event;
    void refresh;
    if (expanded) untrack(() => void load());
  });
  onDestroy(() => requests.dispose());
</script>

<div class="thread" bind:this={host}>
  <CommentItem
    {task}
    {entry}
    {canComment}
    {highlighted}
    onreply={reply}
    {onchange}
  />
  {#if entry.reply_count || target}<div class="replies">
      <button
        class="toggle"
        aria-expanded={expanded}
        onclick={() => (expanded = !expanded)}
        ><span class:expanded>›</span>{entry.reply_count || 0}
        {entry.reply_count === 1
          ? "reply"
          : "replies"}{#if entry.thread_unread}<span
            class="unread"
            aria-label="Unread replies"
          ></span>{/if}</button
      >
      {#if expanded}
        {#if error}<p class="notice error" role="alert">
            {error}<button onclick={() => void load()}>Retry</button>
          </p>{/if}
        {#if more}<button
            class="older"
            disabled={loading}
            onclick={() => void load(true)}>Load older replies</button
          >{/if}
        {#if loading && !replies.length}<p class="hint">
            Loading replies…
          </p>{/if}
        {#each [...replies].reverse() as r (r.id)}<CommentItem
            {task}
            entry={r}
            replyName={[entry, ...replies].find(
              (e) => e.id === r.comment?.reply_to,
            )?.actor.display_name ||
              [entry, ...replies].find((e) => e.id === r.comment?.reply_to)
                ?.actor.username ||
              "earlier comment"}
            {canComment}
            highlighted={highlights[r.id]}
            onreply={reply}
            onchange={() => void changed()}
            onreference={(id) => void jump(id)}
          />{/each}
        {#if target && canComment}{#key target.id}<CommentComposer
              {task}
              replyTo={target.id}
              replyName={target.actor.display_name || target.actor.username}
              onposted={(e) => void posted(e)}
              oncancel={() => (target = null)}
            />{/key}{/if}
      {/if}
    </div>{/if}
</div>

<style>
  .replies {
    margin: 0 0 12px 23px;
    border-left: 1px solid var(--panel-border);
    padding-left: 13px;
  }
  .toggle {
    display: flex;
    align-items: center;
    gap: 7px;
    padding: 5px 0;
    border: 0;
    background: transparent;
    font-size: 11px;
    color: var(--muted);
  }
  .toggle > span:first-child {
    font-size: 19px;
    display: inline-block;
    transition: transform 150ms;
  }
  .toggle .expanded {
    transform: rotate(90deg);
  }
  .unread {
    width: 5px;
    height: 5px;
    background: var(--accent);
    border-radius: 50%;
  }
  .older {
    margin: 8px 0;
    border: 0;
    background: transparent;
    color: var(--muted);
    font-size: 11px;
  }
  @media (max-width: 420px) {
    .replies {
      margin-left: 12px;
      padding-left: 6px;
    }
  }
  @media (prefers-reduced-motion: reduce) {
    .toggle > span:first-child {
      transition: none;
    }
  }
</style>
