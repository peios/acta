<script lang="ts">
  let { data }: { data: Record<string, unknown> } = $props();
  const failed = $derived(data.status !== "completed");
</script>

<details class:failed class="background-notice">
  <summary>
    <svg viewBox="0 0 20 20" aria-hidden="true">
      {#if failed}<path d="m6 6 8 8M14 6l-8 8" />{:else}<path
          d="m4 10 4 4 8-8"
        />{/if}
    </svg>
    <span>{String(data.label ?? "Background task")}</span>
    <span class="outcome"
      >{data.status === "completed"
        ? "Finished in background"
        : data.status === "interrupted"
          ? "Background task stopped"
          : "Failed in background"}</span
    >
    <svg class="chevron" viewBox="0 0 20 20" aria-hidden="true"
      ><path d="m8 6 4 4-4 4" /></svg
    >
  </summary>
  <p>{String(data.summary ?? "")}</p>
</details>

<style>
  .background-notice {
    color: var(--muted);
    font-size: 12px;
  }
  summary {
    display: flex;
    align-items: center;
    gap: 8px;
    padding: 7px 6px;
    border-radius: 7px;
    cursor: pointer;
    list-style: none;
    width: fit-content;
    max-width: 100%;
  }
  summary::-webkit-details-marker {
    display: none;
  }
  summary:hover,
  details[open] summary {
    background: var(--hover-surface);
  }
  summary:focus-visible {
    outline: 2px solid var(--muted);
    outline-offset: 2px;
  }
  svg {
    width: 15px;
    height: 15px;
    flex-shrink: 0;
    fill: none;
    stroke: currentColor;
    stroke-width: 1.5;
    stroke-linecap: round;
    stroke-linejoin: round;
  }
  summary > span:first-of-type {
    color: var(--text);
    overflow-wrap: anywhere;
  }
  .outcome {
    font-size: 11px;
  }
  .failed .outcome {
    color: var(--danger);
  }
  .chevron {
    width: 12px;
    transition: transform 160ms;
  }
  details[open] .chevron {
    transform: rotate(90deg);
  }
  p {
    margin: 6px 0 8px 29px;
    padding: 12px 14px;
    border-radius: 10px;
    background: var(--surface);
    white-space: pre-wrap;
    overflow-wrap: anywhere;
    line-height: 1.6;
  }
  @media (prefers-reduced-motion: reduce) {
    .chevron {
      transition: none;
    }
  }
</style>
