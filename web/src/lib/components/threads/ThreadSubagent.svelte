<script lang="ts">
  import { onMount } from "svelte";
  import MarkdownView from "$lib/components/tasks/MarkdownView.svelte";
  let {
    data,
    open,
    live,
    notice = false,
  }: {
    data: Record<string, unknown>;
    open: (id: string) => void;
    live: boolean;
    notice?: boolean;
  } = $props();
  let now = $state(Date.now());
  onMount(() => {
    const timer = setInterval(() => (now = Date.now()), 1000);
    return () => clearInterval(timer);
  });
  const active = $derived(["running", "waiting"].includes(String(data.status)));
  const labels: Record<string, string> = {
    running: "Working",
    waiting: "Needs input",
    completed: "Completed",
    failed: "Failed",
    interrupted: "Stopped",
    unavailable: "Unavailable",
  };
  const label = $derived(
    active && !live
      ? "Unavailable"
      : (labels[String(data.status)] ?? "Subagent"),
  );
  const seconds = $derived(
    data.started_at && (data.completed_at || live)
      ? Math.max(
          0,
          Math.round(
            ((data.completed_at ? Date.parse(String(data.completed_at)) : now) -
              Date.parse(String(data.started_at))) /
              1000,
          ),
        )
      : null,
  );
  const elapsed = $derived(
    seconds === null || !Number.isFinite(seconds)
      ? ""
      : seconds < 60
        ? `${seconds}s`
        : `${Math.floor(seconds / 60)}m ${seconds % 60}s`,
  );
</script>

{#if notice}
  <details class="agent-notice">
    <summary
      ><span class="notice-icon" aria-hidden="true"
        >{data.status === "completed" ? "✓" : "!"}</span
      ><span>{data.name || "Subagent"}</span><span class="status">{label}</span
      ><span aria-hidden="true">›</span></summary
    >
    <div class="result">
      {#if data.result}<MarkdownView value={String(data.result)} />{/if}
      <button class="open" onclick={() => open(String(data.lane_id))}
        >Open conversation <span aria-hidden="true">↗</span></button
      >
    </div>
  </details>
{:else}
  <button
    class="agent-card"
    onclick={() => open(String(data.lane_id))}
    aria-label={`Open subagent ${data.name}`}
  >
    <span class="agent-icon"
      ><svg
        width="17"
        height="17"
        viewBox="0 0 24 24"
        fill="none"
        stroke="currentColor"
        stroke-width="1.6"
        aria-hidden="true"
        ><circle cx="12" cy="8" r="3" /><path
          d="M5 21v-2a7 7 0 0 1 14 0v2M5 5 2 8l3 3m14-6 3 3-3 3"
        /></svg
      ></span
    >
    <span class="body"
      ><strong>{data.name || "Subagent"}</strong><span
        class="status"
        class:active={active && live}
        >{label}{#if elapsed}
          · {elapsed}{/if}</span
      >{#if data.prompt}<span class="prompt">{data.prompt}</span>{/if}</span
    >
    <span class="arrow" aria-hidden="true">›</span>
  </button>
{/if}

<style>
  .agent-card {
    display: flex;
    width: 100%;
    max-width: 520px;
    text-align: left;
    align-items: center;
    gap: 12px;
    border: 1px solid var(--border);
    border-radius: 12px;
    background: var(--hover-surface);
    padding: 13px 15px;
    color: var(--text);
  }
  .agent-card:hover {
    border-color: var(--muted);
  }
  .agent-icon {
    color: var(--muted);
    display: grid;
    place-items: center;
  }
  .body {
    display: flex;
    flex: 1;
    min-width: 0;
    flex-direction: column;
    gap: 4px;
  }
  .body strong {
    font-size: 13px;
    font-weight: 550;
  }
  .status,
  .prompt {
    font-size: 12px;
    color: var(--muted);
  }
  .status.active,
  .notice-icon {
    color: #78b79a;
  }
  .prompt {
    white-space: nowrap;
    overflow: hidden;
    text-overflow: ellipsis;
  }
  .arrow {
    color: var(--muted);
    font-size: 22px;
  }
  .agent-notice {
    font-size: 12px;
    color: var(--muted);
  }
  summary {
    display: flex;
    align-items: center;
    gap: 8px;
    padding: 7px 6px;
    cursor: pointer;
    list-style: none;
    width: fit-content;
    border-radius: 7px;
  }
  summary::-webkit-details-marker {
    display: none;
  }
  summary:hover {
    background: var(--hover-surface);
  }
  summary > span:nth-child(2) {
    color: var(--text);
  }
  .result {
    margin: 6px 0 8px 26px;
    padding: 12px 14px;
    border-radius: 10px;
    background: var(--surface);
  }
  .open {
    width: auto;
    font-size: 12px;
    margin-top: 8px;
    color: var(--text);
    border: 0;
    background: transparent;
    padding: 5px 0;
  }
</style>
