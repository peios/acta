<script lang="ts">
  import type { ToolBatch } from "$lib/thread-feed.js";
  let { batch }: { batch: ToolBatch } = $props();
  const ready = $derived(batch.servers.filter((server) => server.ready).length);
</script>

{#snippet statusIcon(done: boolean)}
  {#if done}<svg class="ready" viewBox="0 0 20 20" aria-hidden="true"
      ><path d="m4 10 4 4 8-8" /></svg
    >
  {:else}<svg class="spinner" viewBox="0 0 20 20" aria-hidden="true"
      ><circle cx="10" cy="10" r="7" /><path d="M10 3a7 7 0 0 1 7 7" /></svg
    >{/if}
{/snippet}

{#if batch.stacked}
  <details class="tools-status">
    <summary>
      {@render statusIcon(batch.sealed)}<span class="title"
        >{batch.sealed
          ? `Tools started (${batch.servers.length})`
          : "Starting tools"}</span
      >
      {#if !batch.sealed}<span class="count"
          >{ready} of {batch.servers.length}</span
        >{/if}
      <svg class="chevron" viewBox="0 0 20 20" aria-hidden="true"
        ><path d="m8 6 4 4-4 4" /></svg
      >
    </summary>
    <div class="server-list">
      <ul aria-label="MCP servers">
        {#each batch.servers as server (server.name)}
          <li>
            {@render statusIcon(server.ready)}<span
              class="server-name"
              title={server.name}>{server.name}</span
            ><span class="server-state"
              >{server.ready ? "Ready" : "Starting"}</span
            >
          </li>
        {/each}
      </ul>
    </div>
  </details>
{:else}
  <div class="tools-status">
    <div class="single">
      {@render statusIcon(batch.sealed)}<span class="title"
        >{batch.sealed ? "Tool started" : "Starting tool"} ({batch.servers[0]
          .name})</span
      >
    </div>
  </div>
{/if}

<style>
  .tools-status {
    min-width: 0;
    padding: 4px 2px;
  }
  summary,
  .single {
    display: flex;
    align-items: center;
    gap: 9px;
    width: fit-content;
    max-width: 100%;
    min-height: 32px;
    padding: 2px 4px;
    border-radius: 6px;
    color: var(--muted);

    list-style: none;
    font-size: 12px;
  }
  summary {
    cursor: pointer;
  }
  .ready {
    color: #6caa83;
  }
  summary::-webkit-details-marker {
    display: none;
  }
  summary:hover .title,
  details[open] .title {
    color: var(--text);
  }
  .title {
    font-weight: 500;
    transition: color 140ms;
  }
  .count {
    font-size: 11px;
    font-variant-numeric: tabular-nums;
    margin-left: 3px;
  }
  svg {
    width: 14px;
    height: 14px;
    flex-shrink: 0;
    fill: none;
    stroke: currentColor;
    stroke-width: 1.6;
    stroke-linecap: round;
    stroke-linejoin: round;
  }
  .spinner {
    animation: spin 1.2s linear infinite;
  }
  .spinner circle {
    opacity: 0.2;
  }
  .chevron {
    width: 12px;
    opacity: 0.65;
    transition: transform 160ms ease;
  }
  details[open] .chevron {
    transform: rotate(90deg);
  }
  .server-list {
    max-width: 380px;
    margin: 4px 0 2px 11px;
    padding-left: 20px;
    border-left: 1px solid var(--panel-border);
  }
  ul {
    list-style: none;
    padding: 0;
    margin: 0;
  }
  li {
    display: flex;
    align-items: center;
    gap: 10px;
    padding: 7px 0;
    font-size: 12px;
  }
  li .spinner {
    width: 12px;
    height: 12px;
    color: var(--muted);
  }
  .server-name {
    flex: 1;
    min-width: 0;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }
  .server-state {
    color: var(--muted);
    font-size: 11px;
  }
  @keyframes spin {
    to {
      transform: rotate(360deg);
    }
  }
  @media (prefers-reduced-motion: reduce) {
    .spinner {
      animation: none;
    }
    .chevron,
    .title {
      transition: none;
    }
  }
</style>
