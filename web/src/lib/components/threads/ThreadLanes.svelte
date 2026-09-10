<script lang="ts">
  import { dragScroll } from "$lib/drag-scroll";
  import "$lib/horizontal-scroll.css";
  import type { ThreadFrame } from "$lib/threads.svelte";
  let {
    agents,
    selected,
    onselect,
    connected,
    runId,
  }: {
    agents: ThreadFrame[];
    selected: string;
    onselect: (id: string) => void;
    connected: boolean;
    runId?: string;
  } = $props();
  const visible = $derived(
    agents.filter(
      (f) =>
        f.data?.lane_id === selected ||
        (f.run_id === runId &&
          ["running", "waiting"].includes(String(f.data?.status))),
    ),
  );
</script>

{#if agents.length}
  <nav
    class="lanes horizontal-scroll"
    aria-label="Conversation lanes"
    use:dragScroll
  >
    <button
      class:selected={selected === ""}
      aria-current={selected === "" ? "page" : undefined}
      onclick={() => onselect("")}>Main</button
    >
    {#each visible as frame (String(frame.data?.lane_id))}
      {@const lane = frame.data!}
      {@const active = ["running", "waiting"].includes(String(lane.status))}
      {@const live = connected && frame.run_id === runId}
      <button
        class:selected={selected === lane.lane_id}
        aria-current={selected === lane.lane_id ? "page" : undefined}
        title={`${lane.name} · ${active && !live ? "Unavailable" : lane.status}`}
        onclick={() => onselect(String(lane.lane_id))}
      >
        <span
          class="dot"
          class:active={active && live}
          class:waiting={lane.status === "waiting"}
        ></span>
        <span>{lane.name || "Subagent"}</span>
      </button>
    {/each}
  </nav>
{/if}

<style>
  .lanes {
    display: flex;
    align-items: center;
    gap: 7px;
    flex-shrink: 0;
    overflow-x: auto;
    padding: 2px 0 12px;
    margin-bottom: 12px;
    min-width: 0;
  }
  button {
    display: inline-flex;
    align-items: center;
    gap: 7px;
    width: auto;
    flex-shrink: 0;
    min-height: 31px;
    border: 1px solid transparent;
    border-radius: 999px;
    background: var(--hover-surface);
    color: var(--muted);
    padding: 5px 12px;
    font-size: 12px;
    white-space: nowrap;
  }
  button:hover {
    color: var(--text);
  }
  button.selected {
    color: var(--text);
    border-color: var(--border);
    background: var(--panel);
  }
  .dot {
    width: 6px;
    height: 6px;
    border-radius: 50%;
    background: var(--muted);
    opacity: 0.7;
  }
  .dot.active {
    background: #78b79a;
    opacity: 1;
  }
  .dot.waiting {
    background: #d4b06c;
  }
</style>
