<script lang="ts">
  import type { Snippet } from "svelte";
  import type { WorkedGroup } from "$lib/thread-work.js";
  let { group, children }: { group: WorkedGroup; children: Snippet } = $props();
  const duration = $derived(
    group.durationMs === null
      ? null
      : group.durationMs < 1000
        ? "<1s"
        : `${Math.round(group.durationMs / 1000)}s`,
  );
</script>

<details class="worked">
  <summary>
    <svg class="activity-icon" viewBox="0 0 20 20" aria-hidden="true"
      ><path d="m4 10 4 4 8-8" /></svg
    >
    <span>{duration ? `Worked for ${duration}` : "Worked"}</span>
    <svg class="chevron" viewBox="0 0 20 20" aria-hidden="true"
      ><path d="m8 6 4 4-4 4" /></svg
    >
  </summary>
  <div class="activity">{@render children()}</div>
</details>

<style>
  .worked {
    min-width: 0;
  }
  summary {
    display: flex;
    align-items: center;
    gap: 8px;
    width: fit-content;
    padding: 6px 2px;
    color: var(--muted);
    font-size: 13px;
    cursor: pointer;
    list-style: none;
    border-radius: 6px;
  }
  summary::-webkit-details-marker {
    display: none;
  }
  summary:hover {
    color: var(--text);
  }
  summary:focus-visible {
    outline: 2px solid var(--accent);
    outline-offset: 3px;
  }
  svg {
    width: 16px;
    height: 16px;
    fill: none;
    stroke: currentColor;
    stroke-width: 1.5;
    stroke-linecap: round;
    stroke-linejoin: round;
  }
  .chevron {
    width: 14px;
    height: 14px;
    transition: transform 140ms ease;
  }
  details[open] > summary .chevron {
    transform: rotate(90deg);
  }
  .activity {
    display: grid;
    gap: 16px;
    margin: 12px 0 4px 9px;
    padding-left: 16px;
    border-left: 1px solid var(--border);
  }
  @media (prefers-reduced-motion: reduce) {
    .chevron {
      transition: none;
    }
  }
</style>
