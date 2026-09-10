<script lang="ts">
  import type { ToolError } from "$lib/thread-feed.js";
  import RelativeTime from "../RelativeTime.svelte";
  let { error }: { error: ToolError } = $props();
</script>

<div class="tool-error">
  <div class="heading">
    <svg viewBox="0 0 20 20" aria-hidden="true"
      ><circle cx="10" cy="10" r="7" /><path d="M10 6v5m0 3h.01" /></svg
    ><strong
      >{error.cancelled ? "Tool startup cancelled" : "Tool failed to start"} ({error.name})</strong
    ><span><RelativeTime value={error.frame.received_at} /></span>
  </div>
  {#if error.message}<p>{error.message}</p>{/if}
  {#if error.reason && error.reason !== error.message}<p>{error.reason}</p>{/if}
</div>

<style>
  .tool-error {
    padding: 6px;
    color: var(--text);
    font-size: 12px;
    min-width: 0;
  }
  .heading {
    display: flex;
    align-items: center;
    flex-wrap: wrap;
    gap: 9px;
  }
  svg {
    width: 14px;
    height: 14px;
    fill: none;
    stroke: var(--danger, #db8d83);
    stroke-width: 1.6;
    flex-shrink: 0;
  }
  strong {
    font-weight: 500;
    overflow-wrap: anywhere;
  }
  .heading span {
    color: var(--muted);
    font-size: 11px;
  }
  p {
    margin: 6px 0 0 23px;
    color: var(--muted);
    white-space: pre-wrap;
    overflow-wrap: anywhere;
    line-height: 1.6;
  }
</style>
