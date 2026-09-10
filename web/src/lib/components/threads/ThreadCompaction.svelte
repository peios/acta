<script lang="ts">
  import MarkdownView from "$lib/components/tasks/MarkdownView.svelte";
  let { data }: { data: Record<string, unknown> } = $props();
  const summary = $derived(
    typeof data.summary === "string" ? data.summary : "",
  );
  const count = (v: unknown) =>
    typeof v === "number" && v >= 0
      ? new Intl.NumberFormat(undefined, {
          notation: "compact",
          maximumFractionDigits: 1,
        }).format(v)
      : null;
  const before = $derived(count(data.before_tokens));
  const after = $derived(count(data.after_tokens));
  const duration = $derived(
    typeof data.duration_ms === "number"
      ? `${Math.round(data.duration_ms / 1000)}s`
      : null,
  );
</script>

{#snippet label()}
  <svg class="symbol" viewBox="0 0 20 20" aria-hidden="true"
    ><path d="M3 4h14M3 16h14M6 7l4 3 4-3M6 13l4-3 4 3" /></svg
  >
  <span>Context compacted</span>
  {#if before && after}<span
      class="metadata"
      title={`${data.before_tokens} → ${data.after_tokens} tokens`}
      >· {before} → {after}</span
    >{/if}
  {#if duration}<span class="metadata">· {duration}</span>{/if}
{/snippet}

{#if summary}
  <details class="compaction">
    <summary
      >{@render label()}<svg
        class="chevron"
        viewBox="0 0 20 20"
        aria-hidden="true"><path d="m8 6 4 4-4 4" /></svg
      ></summary
    >
    <div class="body"><MarkdownView value={summary} /></div>
  </details>
{:else}
  <div class="compaction notice">{@render label()}</div>
{/if}

<style>
  .compaction {
    color: var(--muted);
    font-size: 12px;
  }
  summary,
  .notice {
    display: flex;
    align-items: center;
    flex-wrap: wrap;
    gap: 8px;
    padding: 8px 6px;
    border-radius: 8px;
  }
  summary {
    width: fit-content;
    max-width: 100%;
    cursor: pointer;
    list-style: none;
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
  .metadata {
    font-size: 11px;
  }
  .chevron {
    width: 12px;
    transition: transform 160ms;
  }
  details[open] .chevron {
    transform: rotate(90deg);
  }
  .body {
    margin: 8px 0 8px 28px;
    padding: 14px 18px;
    border: 1px solid var(--border);
    border-radius: 12px;
    background: var(--surface);
    color: var(--text);
    overflow-wrap: anywhere;
    font-size: 13px;
  }
  @media (max-width: 600px) {
    .body {
      margin-left: 0;
      padding: 12px;
    }
  }
  @media (prefers-reduced-motion: reduce) {
    .chevron {
      transition: none;
    }
  }
</style>
