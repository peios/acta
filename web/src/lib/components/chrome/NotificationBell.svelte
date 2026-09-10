<script lang="ts">
  import { useNotifications } from "$lib/notifications.svelte";
  import { relativeClock, relativeTime } from "$lib/relative-time.js";
  const view = useNotifications();
  let { collapsed = false }: { collapsed?: boolean } = $props();
  const id = $props.id();
  let button = $state<HTMLButtonElement>();
  let popup = $state<HTMLDivElement>();
  let left = $state(12),
    bottom = $state(12);
  function position(e: Event) {
    if ((e as ToggleEvent).newState !== "open" || !button) return;
    const rect = button.getBoundingClientRect();
    left = Math.max(12, Math.min(rect.left, window.innerWidth - 372));
    bottom = Math.max(12, window.innerHeight - rect.top + 8);
  }
</script>

{#if view}
  <button
    bind:this={button}
    class="bell"
    class:collapsed
    popovertarget={id}
    aria-label={`Notifications${view.inbox.total ? ` (${view.inbox.total} unread)` : ""}`}
    title="Notifications"
  >
    <svg
      width="19"
      height="19"
      viewBox="0 0 24 24"
      fill="none"
      stroke="currentColor"
      stroke-width="1.6"
      aria-hidden="true"
      ><path d="M18 8a6 6 0 0 0-12 0c0 7-3 7-3 9h18c0-2-3-2-3-9Z" /><path
        d="M10 21h4"
      /></svg
    >
    {#if !collapsed}<span>Notifications</span>{/if}
    {#if view.inbox.total}<span class="count"
        >{view.inbox.total > 99 ? "99+" : view.inbox.total}</span
      >{/if}
  </button>
  <div
    bind:this={popup}
    {id}
    popover="auto"
    onbeforetoggle={position}
    class="inbox"
    style:left={`${left}px`}
    style:bottom={`${bottom}px`}
    style:max-height={`calc(100dvh - ${bottom + 12}px)`}
    role="region"
    aria-label="Notifications"
  >
    <header>
      <strong>Notifications</strong>{#if view.inbox.items.length}<button
          class="quiet"
          disabled={view.reading}
          onclick={() => void view.read([...view.inbox.items])}
          >{view.inbox.total > view.inbox.items.length
            ? "Mark shown read"
            : "Mark all read"}</button
        >{/if}
    </header>
    {#if view.push.error}<p class="error" role="alert">
        {view.push.error}
      </p>{/if}
    {#if view.error}<p class="error" role="alert">{view.error}</p>{/if}
    <div class="items">
      {#each view.inbox.items as n (n.id)}
        <button
          class="notice"
          onclick={() => {
            popup?.hidePopover();
            void view.open(n);
          }}
        >
          <span
            class="indicator"
            class:failed={n.kind === "failed"}
            class:attention={n.kind === "attention"}
            >{n.task_id
              ? "↗"
              : n.kind === "finished"
                ? "✓"
                : n.kind === "failed"
                  ? "!"
                  : "?"}</span
          >
          <span class="content"
            ><span class="title">{n.title}</span><span class="name"
              >{n.task_id
                ? `${n.task_reference} · ${n.task_title}`
                : n.thread_name}{n.lane_id ? " · Subagent" : ""}</span
            ><time
              datetime={n.created_at}
              title={new Date(n.created_at).toLocaleString()}
              >{relativeTime(n.created_at, $relativeClock)}</time
            ></span
          >
          <span class="arrow" aria-hidden="true">›</span>
        </button>
      {:else}<div class="empty">
          <span aria-hidden="true">✓</span><strong>All caught up</strong>
          <p>Updates from your tasks and agents will appear here.</p>
        </div>{/each}
    </div>
    {#if view.inbox.total > view.inbox.items.length}<p class="limit">
        Showing the most recent {view.inbox.items.length} unread updates.
      </p>{/if}
    {#if !view.push.standalone}<div class="installation">
        {#if view.push.installPrompt}<button
            class="quiet"
            onclick={() => void view.push.installApp()}>Install Acta</button
          >
        {:else}<span
            >{view.push.ios
              ? "For push alerts: Share → Add to Home Screen, then open Acta there."
              : "Install Acta from your browser’s app menu."}</span
          >{/if}
      </div>{/if}
    <footer>
      <button
        class="quiet"
        disabled={view.permission === "unsupported" || view.push.busy}
        onclick={() => void view.browserAlerts()}
        >{view.enabled
          ? "Disable push notifications"
          : "Enable push notifications"}</button
      ><span
        >{view.permission === "denied"
          ? "Blocked in browser settings"
          : view.permission === "unsupported"
            ? "Unavailable in this browser"
            : view.enabled
              ? "On this browser"
              : "Optional"}</span
      >
    </footer>
  </div>
{/if}

<style>
  .bell {
    display: flex;
    align-items: center;
    gap: 11px;
    width: 100%;
    min-height: 42px;
    padding: 10px;
    border: 0;
    background: transparent;
    color: var(--muted);
    border-radius: 8px;
    font-size: 13px;
    text-align: left;
  }
  .bell:hover {
    background: var(--hover-surface);
    color: var(--text);
  }
  .bell > span:first-of-type {
    flex: 1;
  }
  .bell.collapsed {
    position: relative;
    justify-content: center;
    width: 44px;
  }
  .count {
    font-size: 10px;
    line-height: 17px;
    min-width: 17px;
    padding: 0 4px;
    border-radius: 20px;
    background: var(--accent);
    color: var(--accent-contrast, #18202b);
    text-align: center;
    font-variant-numeric: tabular-nums;
  }
  .collapsed .count {
    position: absolute;
    top: 0;
    right: -2px;
  }
  .inbox {
    position: fixed;
    margin: 0;
    top: auto;
    right: auto;
    width: min(360px, calc(100vw - 24px));
    max-height: calc(100dvh - 32px);
    padding: 0;
    border: 1px solid var(--panel-border);
    border-radius: 14px;
    background: var(--surface);
    color: var(--text);
    box-shadow: 0 12px 44px #0005;
    overflow: auto;
  }
  header,
  footer {
    display: flex;
    align-items: center;
    justify-content: space-between;
    gap: 12px;
    padding: 14px 16px;
  }
  header {
    border-bottom: 1px solid var(--panel-border);
    font-size: 14px;
  }
  footer {
    border-top: 1px solid var(--panel-border);
    font-size: 11px;
    color: var(--muted);
  }
  .quiet {
    border: 0;
    background: transparent;
    color: var(--muted);
    font-size: 12px;
    min-height: 28px;
    padding: 3px 0;
  }
  .quiet:hover {
    color: var(--text);
  }
  .items {
    max-height: min(400px, 45dvh);
    overflow: auto;
    padding: 6px;
  }
  .notice {
    display: flex;
    align-items: flex-start;
    gap: 12px;
    padding: 12px 10px;
    width: 100%;
    text-align: left;
    border: 0;
    background: transparent;
    color: inherit;
    border-radius: 8px;
  }
  .notice:hover {
    background: var(--hover-surface);
  }
  .indicator {
    display: grid;
    place-items: center;
    width: 25px;
    height: 25px;
    border: 1px solid var(--panel-border);
    border-radius: 50%;
    color: var(--muted);
    flex-shrink: 0;
    font-size: 13px;
  }
  .indicator.attention {
    color: var(--accent);
    background: color-mix(in srgb, var(--accent) 10%, transparent);
  }
  .indicator.failed {
    color: #ef9b92;
    background: #ef9b9212;
  }
  .content {
    display: grid;
    gap: 4px;
    min-width: 0;
    flex: 1;
  }
  .title {
    font-size: 13px;
    font-weight: 550;
  }
  .name {
    font-size: 12px;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
    color: var(--muted);
  }
  time {
    color: var(--muted);
    font-size: 11px;
  }
  .arrow {
    align-self: center;
    color: var(--muted);
  }
  .empty {
    padding: 28px 16px;
    display: grid;
    gap: 10px;
    text-align: center;
    font-size: 13px;
  }
  .empty > span {
    color: var(--accent);
    font-size: 22px;
  }
  .empty p {
    margin: 0;
    color: var(--muted);
    font-size: 12px;
  }
  .installation {
    padding: 12px 16px;
    border-top: 1px solid var(--panel-border);
    color: var(--muted);
    font-size: 12px;
    line-height: 1.5;
  }
  .error,
  .limit {
    margin: 10px 16px;
    font-size: 12px;
    color: var(--muted);
  }
  .error {
    color: #ef9b92;
  }
</style>
