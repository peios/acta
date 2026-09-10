<script lang="ts">
  import { ThreadScrollIntent } from "$lib/thread-scroll-intent.js";
  import ThreadWorked from "./ThreadWorked.svelte";
  import { groupOlderWork } from "$lib/thread-work.js";
  import type { ThreadFrame } from "$lib/threads.svelte";
  import { tick, untrack } from "svelte";
  import ThreadSubagent from "./ThreadSubagent.svelte";
  import ThreadInteraction from "./ThreadInteraction.svelte";
  import ThreadApprovalReview from "./ThreadApprovalReview.svelte";
  import type { ApprovalItem } from "$lib/thread-permissions.js";
  import {
    finishedThinkingTurns,
    thinkingTurnKey,
  } from "$lib/thread-thinking.js";
  import { visibleFeed } from "$lib/thread-frames.js";
  import type { FeedItem } from "$lib/thread-feed.js";
  import type { Thread } from "$lib/threads.svelte";
  import ThreadCompaction from "./ThreadCompaction.svelte";
  import ThreadGitPush from "./ThreadGitPush.svelte";
  import ThreadToolNotification from "./ThreadToolNotification.svelte";
  import ThreadToolCall from "$lib/components/threads/ThreadToolCall.svelte";
  import ThreadThinking from "$lib/components/threads/ThreadThinking.svelte";
  import ThreadHook from "$lib/components/threads/ThreadHook.svelte";
  import ThreadUserMessage from "$lib/components/threads/ThreadUserMessage.svelte";
  import ThreadAssistantMessage from "$lib/components/threads/ThreadAssistantMessage.svelte";
  import ThreadToolError from "$lib/components/threads/ThreadToolError.svelte";
  import ThreadToolsStatus from "$lib/components/threads/ThreadToolsStatus.svelte";
  import ThreadTurnEnded from "$lib/components/threads/ThreadTurnEnded.svelte";
  import RelativeTime from "$lib/components/RelativeTime.svelte";
  const frameLabels: Record<string, string> = {
    "debug/unknown": "Unknown Frame",
    "debug/dropped": "Dropped Frame",
    "debug/local": "Local Frame",
    "debug/resolved": "Resolved Frame",
    "debug/provider-diagnostic": "Provider Diagnostic",
  };

  let {
    latestTurnStart,
    openLane = () => {},
    hasMore = false,
    loadingOlder = false,
    initialLoading = false,
    loadOlder = async () => {},
    threadId = "",
    approvals,
    answerApproval,
    editQuestion = () => {},
    items,
    thread,
    showDebug,
  }: {
    latestTurnStart?: ThreadFrame;
    openLane?: (id: string) => void;
    hasMore?: boolean;
    loadingOlder?: boolean;
    initialLoading?: boolean;
    loadOlder?: () => Promise<void>;
    threadId?: string;
    approvals: ApprovalItem[];
    editQuestion?: (
      item: ApprovalItem,
      id: string,
      value: { selected: string[]; text: string },
    ) => void;
    answerApproval: (
      item: ApprovalItem,
      decision: "approve" | "deny" | "answer",
      values?: Record<string, string[]>,
    ) => void;
    items: FeedItem[];
    thread?: Thread;
    showDebug: boolean;
  } = $props();

  const finishedTurns = $derived(finishedThinkingTurns(items));
  let scroller = $state<HTMLDivElement>();
  let content = $state<HTMLDivElement>();
  const intent = new ThreadScrollIntent();
  let touchY: number | null = null;
  let remembered: { id: string; offset: number } | null = null;
  // Only top-level rows participate in anchoring. Closed Worked groups still
  // contain descendants in the DOM; measuring them makes long histories costly.
  function anchorRows() {
    return (
      content?.querySelectorAll<HTMLElement>(":scope > [data-feed-id]") ?? []
    );
  }
  function visibleAnchor() {
    if (!scroller || !content) return null;
    const top = scroller.getBoundingClientRect().top;
    const rows = content.children;
    let low = 0,
      high = rows.length;
    // Feed rows are vertically ordered. Locate the first visible row without
    // measuring every preceding message on every native scroll event.
    while (low < high) {
      const middle = (low + high) >>> 1;
      if (rows[middle].getBoundingClientRect().bottom <= top) low = middle + 1;
      else high = middle;
    }
    for (; low < rows.length; low++) {
      const row = rows[low] as HTMLElement;
      if (row.dataset.feedId)
        return {
          id: row.dataset.feedId,
          offset: row.getBoundingClientRect().top - top,
        };
    }
    return null;
  }
  function matchesAnchor(el: HTMLElement, id: string | undefined) {
    return (
      el.dataset.feedId === id ||
      (el.dataset.feedMembers
        ? JSON.parse(el.dataset.feedMembers).includes(id)
        : false)
    );
  }
  function remember() {
    remembered = visibleAnchor();
  }
  function writeScroll(top: number) {
    if (!scroller) return;
    const target = Math.max(
      0,
      Math.min(top, scroller.scrollHeight - scroller.clientHeight),
    );
    // Even assigning the current offset interrupts a native smooth scroll.
    if (Math.abs(target - scroller.scrollTop) < 0.5) return;
    scroller.scrollTop = target;
    intent.wrote(scroller.scrollTop);
  }
  function readerMoved() {
    if (!scroller || !positioned) return false;
    const moved = intent.observe(
      scroller.scrollTop,
      scroller.scrollHeight - scroller.clientHeight,
    );
    if (moved) remember();
    return moved;
  }
  function input(direction: number) {
    if (!scroller) return;
    intent.input(
      direction,
      scroller.scrollTop,
      scroller.scrollHeight - scroller.clientHeight,
    );
    // A wheel/touch gesture at scrollTop=0 cannot produce another scroll event.
    if (direction < 0) loadNearStart();
  }
  $effect(() => {
    if (!scroller) return;
    const node = scroller;
    const wheel = (event: WheelEvent) => {
      if (event.deltaY) input(Math.sign(event.deltaY));
    };
    // Observe intent without making native wheel scrolling wait for a handler
    // that could cancel it. This is particularly important for Firefox.
    node.addEventListener("wheel", wheel, { passive: true });
    return () => node.removeEventListener("wheel", wheel);
  });
  $effect(() => {
    if (!scroller || !content) return;
    const node = scroller;
    // Markdown, expanded details and viewport changes can resize content after
    // Svelte's DOM tick. Keep the reader's position through those changes too.
    const observer = new ResizeObserver(() => {
      if (!positioned || initialLoading || readerMoved()) return;
      if (intent.following && !loadingOlder) writeScroll(node.scrollHeight);
      else if (remembered) {
        const anchor = Array.from(anchorRows()).find((el) =>
          matchesAnchor(el, remembered?.id),
        );
        if (anchor)
          writeScroll(
            node.scrollTop +
              anchor.getBoundingClientRect().top -
              node.getBoundingClientRect().top -
              remembered.offset,
          );
      }
      remember();
    });
    observer.observe(content);
    observer.observe(node);
    return () => observer.disconnect();
  });
  let positioned = false;
  let activeThread = "";
  const positions = new Map<
    string,
    { pinned: boolean; anchor: { id: string; offset: number } | null }
  >();
  let scrollRevision = 0;
  $effect.pre(() => {
    scroller;
    items;
    latestTurnStart;
    showDebug;
    initialLoading;
    threadId;
    loadingOlder;
    untrack(() => {
      if (activeThread !== threadId || initialLoading) {
        if (activeThread !== threadId && activeThread)
          positions.set(activeThread, {
            pinned: intent.following,
            anchor: remembered,
          });
        activeThread = threadId;
        positioned = false;
        intent.reset(scroller?.scrollTop ?? 0);
        remembered = null;
      }
      const node = scroller;
      if (!node) return;
      readerMoved();
      const inputRevision = intent.revision;
      const version = ++scrollRevision;
      const follow = positioned && !loadingOlder && intent.following;
      const anchor = visibleAnchor();
      const id = anchor?.id;
      const offset = anchor?.offset ?? 0;
      void tick().then(() => {
        if (version !== scrollRevision || !node.isConnected || initialLoading)
          return;
        readerMoved();
        if (intent.revision !== inputRevision) return;
        if (!positioned) {
          const saved = positions.get(threadId);
          const anchor = saved?.anchor;
          const el = anchor
            ? Array.from(anchorRows()).find((el) =>
                matchesAnchor(el, anchor.id),
              )
            : undefined;
          if (saved && !saved.pinned && el && anchor) {
            writeScroll(
              node.scrollTop +
                el.getBoundingClientRect().top -
                node.getBoundingClientRect().top -
                anchor.offset,
            );
            intent.following = false;
          } else writeScroll(node.scrollHeight);
        } else if (follow) writeScroll(node.scrollHeight);
        else if (id) {
          const updated = Array.from(anchorRows()).find((el) =>
            matchesAnchor(el, id),
          );
          if (updated)
            writeScroll(
              node.scrollTop +
                updated.getBoundingClientRect().top -
                node.getBoundingClientRect().top -
                offset,
            );
        }
        positioned = true;
        remember();
      });
    });
  });
  function scroll() {
    readerMoved();
    loadNearStart();
  }
  function loadNearStart() {
    if (
      positioned &&
      !intent.following &&
      hasMore &&
      !loadingOlder &&
      scroller &&
      scroller.isConnected &&
      scroller.scrollTop < 150
    )
      void requestHistory();
  }
  let historyRequest = false;
  async function requestHistory() {
    if (historyRequest || initialLoading || loadingOlder || !hasMore) return;
    historyRequest = true;
    const requestedThread = threadId;
    const oldest = items[0]?.id;
    try {
      await loadOlder();
      await tick();
    } finally {
      historyRequest = false;
    }
    // A page can disappear entirely into an existing collapsed Worked group.
    // Recheck after layout/anchor restoration instead of waiting for a scroll
    // event that will never arrive. Only continue when history actually advanced;
    // a failed/unchanged request must not start a retry loop.
    if (requestedThread === threadId && items[0]?.id !== oldest)
      requestAnimationFrame(() => {
        if (requestedThread === threadId) loadNearStart();
      });
  }
</script>

<!-- svelte-ignore a11y_no_noninteractive_tabindex, a11y_no_noninteractive_element_interactions (Scrollable conversation region must support native keyboard scrolling.) -->
<div
  class="frame-feed"
  bind:this={scroller}
  onscroll={scroll}
  onpointerdown={(event) => {
    if (event.target === scroller) intent.drag();
  }}
  ontouchstart={(event) => {
    touchY = event.touches.length === 1 ? event.touches[0].clientY : null;
  }}
  ontouchmove={(event) => {
    if (touchY !== null && event.touches.length === 1) {
      const y = event.touches[0].clientY;
      if (y !== touchY) input(Math.sign(touchY - y));
      touchY = y;
    }
  }}
  ontouchend={() => {
    touchY = null;
  }}
  onkeydown={(event) => {
    if (
      event.defaultPrevented ||
      (event.target as HTMLElement).closest(
        'input, textarea, [contenteditable="true"]',
      )
    )
      return;
    if (event.key === "End") intent.toBottom();
    else if (
      ["ArrowUp", "PageUp", "Home"].includes(event.key) ||
      (event.key === " " && event.shiftKey)
    )
      input(-1);
    else if (["ArrowDown", "PageDown"].includes(event.key) || event.key === " ")
      input(1);
  }}
  role="region"
  tabindex="0"
  aria-label="Thread frames"
>
  <div class="feed-content" bind:this={content}>
    {#if initialLoading}<p class="history-loading" role="status">
        Loading conversation…
      </p>
    {:else if hasMore}<button
        class="load-history"
        disabled={loadingOlder}
        onclick={() => void requestHistory()}
        >{loadingOlder
          ? "Loading older messages…"
          : "Load older messages"}</button
      >{/if}
    {#each groupOlderWork(visibleFeed(items, showDebug), latestTurnStart) as item (item.id)}
      {#if item.kind === "worked"}
        <div
          class="feed-item"
          data-feed-id={item.id}
          data-feed-members={JSON.stringify(item.memberIds)}
        >
          <ThreadWorked group={item}>
            {#each item.items as activity (activity.id)}{@render renderItem(
                activity,
              )}{/each}
          </ThreadWorked>
        </div>
      {:else}{@render renderItem(item)}{/if}
    {:else}{#if !initialLoading}<p class="empty-feed">
          {showDebug
            ? "Provider frames will appear here."
            : "No visible frames yet."}
        </p>{/if}{/each}
  </div>
</div>

{#snippet renderItem(item: FeedItem)}
  <div
    class="feed-item"
    data-feed-id={item.id}
    data-feed-members={item.kind === "thinking" && item.memberIds
      ? JSON.stringify(item.memberIds)
      : undefined}
  >
    {#if item.kind === "frame" && ["approval/request", "question/request"].includes(item.frame.kind)}
      {@const approval = approvals.find(
        (a) =>
          a.id ===
          (item.frame.data?.approval_id ?? item.frame.data?.question_id),
      )}
      {#if approval}<ThreadInteraction
          item={approval}
          answer={answerApproval}
          {editQuestion}
        />{/if}
    {:else if item.kind === "frame" && item.frame.kind === "context/compaction"}
      <ThreadCompaction data={item.frame.data ?? {}} />
    {:else if item.kind === "frame" && item.frame.kind === "vcs/push"}
      <ThreadGitPush
        data={item.frame.data ?? {}}
        timestamp={item.frame.received_at}
      />
    {:else if item.kind === "frame" && item.frame.kind === "tool/notification"}
      <ThreadToolNotification data={item.frame.data ?? {}} />
    {:else if item.kind === "frame" && item.frame.kind === "subagent/notification"}
      <ThreadSubagent
        notice
        data={item.frame.data ?? {}}
        open={openLane}
        live={false}
      />
    {:else if item.kind === "subagent"}
      <ThreadSubagent
        data={item.data}
        open={openLane}
        live={!!thread?.connection_id &&
          item.frame.run_id === thread?.run_id &&
          thread?.state === "running"}
      />
    {:else if item.kind === "tool-call"}
      <div class="tool-target" id={"tool-" + item.id}>
        <ThreadToolCall
          call={item}
          live={!!thread?.connection_id &&
            thread?.state === "running" &&
            item.frame.run_id === thread.run_id}
        />
      </div>
    {:else if item.kind === "approval-review"}
      {@const target = items.find(
        (i) =>
          i.kind === "tool-call" &&
          i.toolId === item.data.tool_id &&
          i.frame.run_id === item.frame.run_id,
      )}
      <ThreadApprovalReview
        review={item}
        live={!!thread?.connection_id &&
          thread?.state === "running" &&
          item.frame.run_id === thread.run_id}
        showTool={target
          ? () => {
              const node = document.getElementById("tool-" + target.id);
              const details = node?.querySelector("details");
              if (details) details.open = true;
              let ancestor = node?.parentElement;
              while (ancestor) {
                if (ancestor instanceof HTMLDetailsElement)
                  ancestor.open = true;
                ancestor = ancestor.parentElement;
              }
              node?.scrollIntoView({ block: "nearest" });
              details?.querySelector("summary")?.focus();
            }
          : undefined}
      />
    {:else if item.kind === "thinking"}
      <ThreadThinking
        thinking={item}
        turnEnded={finishedTurns.has(thinkingTurnKey(item.frame, item.turn))}
        live={!!thread?.connection_id &&
          thread?.state === "running" &&
          item.frame.run_id === thread.run_id}
      />
    {:else if item.kind === "hook"}
      <ThreadHook hook={item} />
    {:else if item.kind === "tools"}
      <ThreadToolsStatus batch={item} />
    {:else if item.kind === "tool-error"}
      <ThreadToolError error={item} />
    {:else if item.kind === "user-message"}
      <ThreadUserMessage message={item} />
    {:else if item.kind === "assistant-message"}
      <ThreadAssistantMessage message={item} />
    {:else if item.kind === "turn-ended"}
      <ThreadTurnEnded ending={item} />
    {:else}
      {@const frame = item.frame}
      <article
        class="frame"
        class:diagnostic={frame.kind === "debug/provider-diagnostic"}
      >
        <div class="frame-heading">
          <strong>{frameLabels[frame.kind] ?? frame.kind}</strong><span
            >#{frame.sequence}:{frame.output_index} · <RelativeTime
              value={frame.received_at}
            /></span
          >
        </div>
        <pre>{frame.raw_json}</pre>
      </article>
    {/if}
  </div>
{/snippet}

<style>
  .feed-item {
    min-width: 0;
  }
  .history-loading,
  .load-history {
    color: var(--muted);
    font-size: 12px;
    text-align: center;
  }
  .load-history {
    background: transparent;
    border: 0;
    width: auto;
    min-height: 30px;
    justify-self: center;
    padding: 4px 12px;
  }
  .tool-target {
    min-width: 0;
  }
  .frame-feed {
    flex: 1;
    min-height: 0;
    overflow: auto;
    overscroll-behavior: contain;
    padding: 0 12px 2px 0;
    margin-bottom: 14px;
    scrollbar-width: thin;
    scrollbar-color: var(--border) transparent;
    overflow-anchor: none;
  }
  .feed-content {
    display: grid;
    align-content: start;
    gap: 18px;
    min-width: 0;
  }
  .frame {
    min-width: 0;
    border: 1px solid var(--panel-border);
    border-radius: 12px;
    background: var(--hover-surface);
    padding: 16px 18px;
  }
  .frame-heading {
    display: flex;
    justify-content: space-between;
    align-items: center;
    gap: 12px;
    font-size: 12px;
  }
  .frame-heading strong {
    font-weight: 550;
  }
  .frame-heading > span {
    color: var(--muted);
    font-size: 11px;
  }
  pre {
    font-family: monospace;
    font-size: 12px;
    line-height: 1.7;
    white-space: pre-wrap;
    overflow-wrap: anywhere;
    margin: 16px 0 0;
    max-height: 520px;
    overflow: auto;
  }
  .diagnostic {
    opacity: 0.75;
  }
  .empty-feed {
    color: var(--muted);
    font-size: 13px;
    text-align: center;
    padding: 60px 0;
  }
  @media (max-width: 440px) {
    .frame {
      padding: 12px;
    }
    .frame-heading {
      align-items: flex-start;
      flex-direction: column;
      gap: 6px;
    }
  }
</style>
