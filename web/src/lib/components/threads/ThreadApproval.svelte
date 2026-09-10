<script lang="ts">
  import type { ApprovalItem } from "$lib/thread-permissions.js";
  import ThreadDiff from "./ThreadDiff.svelte";
  let {
    item,
    answer,
    compact = false,
  }: {
    item: ApprovalItem;
    answer: (item: ApprovalItem, decision: "approve" | "deny") => void;
    compact?: boolean;
  } = $props();
  const details = $derived(item.data.details);
  const command = $derived(details.command ?? details.input?.command);
  const path = $derived(
    details.input?.file_path ?? details.blocked_path ?? details.grantRoot,
  );
</script>

<section class="approval" class:compact aria-label="Permission request">
  <div class="heading">
    <svg
      viewBox="0 0 24 24"
      width="18"
      height="18"
      fill="none"
      stroke="currentColor"
      stroke-width="1.6"
      aria-hidden="true"
      ><path d="m12 3 8 3v6c0 5-8 9-8 9s-8-4-8-9V6z" /><path
        d="M12 8v5m0 3v.1"
      /></svg
    ><strong>{item.data.title}</strong><span class="status"
      >{item.status === "pending"
        ? "Needs approval"
        : item.status === "unconfirmed"
          ? "Decision unconfirmed"
          : item.status[0].toUpperCase() + item.status.slice(1)}</span
    >
  </div>
  {#if item.data.reason}<p>{item.data.reason}</p>{/if}
  {#if typeof command === "string"}<pre class="command">{command}</pre>{/if}
  {#if typeof path === "string"}<div class="cwd">{path}</div>{/if}
  {#if typeof details?.cwd === "string"}<div class="cwd">
      {details.cwd}
    </div>{/if}
  {#if Array.isArray(details?.changes)}
    {#each details.changes as change}<div class="change">
        <div class="cwd">{change.path}</div>
        <ThreadDiff
          text={change.diff ?? ""}
          format={change.format ?? "unified"}
        />
      </div>{/each}
  {/if}
  <details>
    <summary>Details</summary>
    <pre>{JSON.stringify(details, null, 2)}</pre>
  </details>
  {#if item.error}<p role="alert" class="error">{item.error}</p>{/if}
  {#if item.status === "pending"}<div class="actions">
      <button
        disabled={!item.available || item.busy}
        onclick={() => answer(item, "deny")}>Deny</button
      ><button
        class="primary"
        disabled={!item.available || item.busy}
        onclick={() => answer(item, "approve")}
        >{item.busy ? "Sending…" : "Approve"}</button
      >
    </div>{/if}
</section>

<style>
  .approval {
    border: 1px solid var(--panel-border);
    border-radius: 12px;
    background: var(--hover-surface);
    padding: 15px 16px;
    font-size: 13px;
    min-width: 0;
  }
  .compact {
    border: 0;
    background: transparent;
    padding: 0;
  }
  .heading {
    display: flex;
    gap: 9px;
    align-items: center;
  }
  .heading svg {
    color: var(--muted);
    flex-shrink: 0;
  }
  strong {
    font-weight: 550;
  }
  .status {
    margin-left: auto;
    color: var(--muted);
    font-size: 11px;
  }
  .approval p {
    color: var(--muted);
    line-height: 1.6;
    margin: 12px 0;
    white-space: pre-wrap;
    overflow-wrap: anywhere;
  }
  .change {
    margin-top: 12px;
  }
  .cwd {
    font-size: 11px;
    color: var(--muted);
    overflow-wrap: anywhere;
  }
  pre {
    font-size: 12px;
    line-height: 1.6;
    white-space: pre-wrap;
    overflow-wrap: anywhere;
    max-height: 260px;
    overflow: auto;
    margin: 10px 0;
  }
  .command {
    background: var(--surface);
    border-radius: 8px;
    padding: 10px 12px;
  }
  details {
    margin-top: 12px;
    color: var(--muted);
  }
  summary {
    cursor: pointer;
    font-size: 12px;
  }
  .actions {
    display: flex;
    justify-content: flex-end;
    gap: 8px;
    margin-top: 16px;
  }
  .actions button {
    width: auto;
    min-height: 34px;
    padding: 6px 16px;
    font-size: 12px;
    border-radius: 8px;
  }
  .approval .error {
    color: var(--danger);
  }
</style>
