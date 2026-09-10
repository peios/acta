<script lang="ts">
  import ThreadDiff from "./ThreadDiff.svelte";
  import type { TurnEnding } from "$lib/thread-turns.js";
  import RelativeTime from "$lib/components/RelativeTime.svelte";
  let { ending }: { ending: TurnEnding } = $props();
  const duration = $derived(
    ending.durationMs === null
      ? null
      : ending.durationMs < 1000
        ? `${ending.durationMs}ms`
        : `${Math.round(ending.durationMs / 100) / 10}s`,
  );
</script>

<details
  class="turn-ending"
  class:hasDiff={ending.diff !== null}
  class:failed={ending.label === "Turn failed"}
>
  <summary>
    <span class="rule"></span><span>{ending.label}</span>
    {#if duration}<span class="duration">· {duration}</span>{/if}
    {#if ending.diff}<span class="changes-label">· Changes</span>{/if}
    <svg viewBox="0 0 16 16" aria-hidden="true"><path d="m6 4 4 4-4 4" /></svg>
    <span class="rule"></span>
  </summary>
  <div class="details">
    <div class="completed">
      <RelativeTime value={ending.frame.received_at} />
      {#if ending.durationMs !== null}<span
          >{ending.durationMs.toLocaleString()} ms</span
        >{/if}
    </div>
    {#if ending.diff !== null}
      <h4>
        {ending.diffMode === "sequential"
          ? "Individual changes"
          : "Turn changes"}
      </h4>
      {#if ending.diffMode === "sequential"}<p class="note">
          Edits shown in order because their file snapshots could not be joined
          reliably.
        </p>{/if}
      {#if ending.diffIncomplete}<p class="note">
          Some operations did not report enough information for a diff. See
          their tool results for details.
        </p>{/if}
      <ThreadDiff text={ending.diff} />
    {/if}
    {#if ending.error}<p class="error-message">{ending.error}</p>{/if}
    {#if ending.tokens.length}
      <h4>Last model call</h4>
      <dl>
        {#each ending.tokens as token}<div>
            <dt>{token.label}</dt>
            <dd>{token.value.toLocaleString()}</dd>
          </div>{/each}
      </dl>
      <p class="note">
        The latest reported call in this turn, not a turn total.
      </p>
    {:else}<p class="note">Token usage was not reported for this turn.</p>{/if}
  </div>
</details>

<style>
  .turn-ending {
    min-width: 0;
    padding: 6px 0;
    color: var(--muted);
    font-size: 12px;
  }
  summary {
    display: flex;
    align-items: center;
    gap: 8px;
    cursor: pointer;
    list-style: none;
    min-height: 30px;
    border-radius: 6px;
  }
  summary::-webkit-details-marker {
    display: none;
  }
  summary:hover {
    color: var(--text);
  }
  .rule {
    height: 1px;
    background: var(--panel-border);
    flex: 1;
    min-width: 8px;
  }
  .duration {
    font-variant-numeric: tabular-nums;
  }
  svg {
    width: 12px;
    height: 12px;
    fill: none;
    stroke: currentColor;
    stroke-width: 1.5;
    flex-shrink: 0;
    transition: transform 140ms ease;
  }
  details[open] svg {
    transform: rotate(90deg);
  }
  .failed summary {
    color: var(--danger);
  }
  .details {
    max-width: 420px;
    margin: 10px auto 0;
    padding: 4px 12px;
  }
  .hasDiff .details {
    max-width: none;
  }
  .completed {
    display: flex;
    flex-wrap: wrap;
    justify-content: space-between;
    gap: 12px;
    font-size: 11px;
  }
  h4 {
    font-size: 12px;
    font-weight: 550;
    color: var(--text);
    margin: 16px 0 8px;
  }
  dl {
    margin: 0;
  }
  dl > div {
    display: flex;
    justify-content: space-between;
    gap: 16px;
    padding: 4px 0;
  }
  dd {
    color: var(--text);
    margin: 0;
    font-variant-numeric: tabular-nums;
  }
  .note {
    font-size: 11px;
    line-height: 1.6;
    margin: 10px 0 0;
  }
  .error-message {
    color: var(--danger);
    white-space: pre-wrap;
    overflow-wrap: anywhere;
    line-height: 1.6;
  }
  @media (prefers-reduced-motion: reduce) {
    svg {
      transition: none;
    }
  }
</style>
