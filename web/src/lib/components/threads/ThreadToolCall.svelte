<script lang="ts">
  import ThreadMemoryLinks from "./ThreadMemoryLinks.svelte";
  import ThreadTaskLinks from "./ThreadTaskLinks.svelte";
  import ThreadDiff from "./ThreadDiff.svelte";
  import ThreadToolImages from "./ThreadToolImages.svelte";
  import ThreadToolAttachments from "./ThreadToolAttachments.svelte";
  import { fileChanges } from "$lib/thread-diffs.js";
  import type { ToolCallItem } from "$lib/thread-tool-calls.js";
  import { terminalTool } from "$lib/thread-tool-calls.js";
  let { call, live }: { call: ToolCallItem; live: boolean } = $props();
  const d = $derived(call.data);
  const changes = $derived(fileChanges(d.changes));
  const hasImages = $derived(Array.isArray(d.images) && d.images.length > 0);
  const hasAttachments = $derived(
    Array.isArray(d.attachments) && d.attachments.length > 0,
  );
  const status = $derived(
    !live && !terminalTool(call.status) ? "unavailable" : call.status,
  );
  const busy = $derived(
    ["preparing", "pending", "running", "background"].includes(status),
  );
  const failed = $derived(
    ["failed", "declined", "interrupted", "permission_denied"].includes(status),
  );
  const label = $derived(typeof d.label === "string" ? d.label : "Tool call");
  const labels: Record<string, string> = {
    preparing: "Preparing",
    pending: "Pending",
    running: "Running",
    background: "Running in background",
    completed: "Completed",
    failed: "Failed",
    declined: "Declined",
    permission_denied: "Permission denied",
    interrupted: "Interrupted",
    unavailable: "Unavailable",
  };
  const denial = $derived(
    d.permission_denial && typeof d.permission_denial === "object"
      ? (d.permission_denial as Record<string, unknown>)
      : null,
  );
  const duration = $derived(
    typeof d.duration_ms === "number"
      ? d.duration_ms < 1000
        ? `${d.duration_ms}ms`
        : `${(d.duration_ms / 1000).toFixed(1)}s`
      : null,
  );
  const streams = $derived(
    ["stdout", "stderr"].filter((k) => typeof d[k] === "string" && d[k] !== ""),
  );
</script>

<details class="tool-call" class:failed>
  <summary>
    <svg class="tool-icon" viewBox="0 0 20 20" aria-hidden="true"
      >{#if d.category === "read"}<path
          d="M5 2h7l4 4v12H5ZM12 2v5h4M8 10h5m-5 3h5"
        />{:else if d.category === "write" || d.category === "edit"}<path
          d="m4 13 9-9 3 3-9 9-4 1ZM11 6l3 3M4 19h12"
        />{:else if d.category === "command"}<path
          d="m3 5 5 5-5 5m7 0h7"
        />{:else}<path d="m7 5-4 5 4 5m6-10 4 5-4 5" />{/if}</svg
    >
    <span class="label">{label}</span>
    {#if busy}<span class="spinner" aria-hidden="true"></span>{/if}
    <span class="status">{labels[status] ?? status}</span>
    {#if duration}<span class="duration">· {duration}</span>{/if}
    <svg class="chevron" viewBox="0 0 20 20" aria-hidden="true"
      ><path d="m8 6 4 4-4 4" /></svg
    >
  </summary>
  <div class="body">
    <div class="metadata">
      <span>{d.name ?? "Tool"}</span>{#if typeof d.cwd === "string"}<span
          title={d.cwd}>{d.cwd}</span
        >{/if}{#if typeof d.exit_code === "number"}<span
          >Exit {d.exit_code}</span
        >{/if}
    </div>
    {#if denial}
      <section class="denial">
        <h4>Permission denied</h4>
        <p>{String(denial.message ?? "Permission was not granted.")}</p>
        {#if typeof denial.reason === "string" && denial.reason !== denial.message}
          <p class="reason">{denial.reason}</p>
        {/if}
      </section>
    {/if}
    {#if changes.length}
      <section>
        <h4>{status === "completed" ? "Changes" : "Requested changes"}</h4>
        {#each changes as change}
          <div class="file-change">
            <div class="file-path">
              <span>{change.path}</span><span class="file-kind"
                >{change.kind === "add"
                  ? "Added"
                  : change.kind === "delete"
                    ? "Deleted"
                    : "Modified"}</span
              >
            </div>
            {#if change.move_path}<p class="move-path">
                → {change.move_path}
              </p>{/if}
            <ThreadDiff text={change.diff} format={change.format} />
          </div>
        {/each}
      </section>
    {/if}
    <section>
      <h4>Arguments</h4>
      {#if call.argumentsText}<pre>{call.argumentsText}</pre>{:else}<p
          class="empty"
        >
          Waiting for arguments…
        </p>{/if}
    </section>
    {#if !changes.length || call.output || hasImages || hasAttachments}
      <section>
        <h4>Output</h4>
        {#if hasAttachments}<ThreadToolAttachments
            attachments={d.attachments}
          />{/if}
        {#if hasImages}<ThreadToolImages images={d.images} {label} />{/if}
        {#if call.output}<pre>{call.output}</pre>{:else if !hasImages && !hasAttachments}<p
            class="empty"
          >
            {terminalTool(status) ? "No output returned." : "No output yet."}
          </p>{/if}
      </section>
    {/if}
    {#if streams.length && !(streams.length === 1 && d[streams[0]] === call.output)}
      <details class="streams">
        <summary>Output streams</summary>{#each streams as key}<section>
            <h4>{key === "stdout" ? "Standard output" : "Standard error"}</h4>
            <pre>{String(d[key])}</pre>
          </section>{/each}
      </details>
    {/if}
  </div>
</details>

<ThreadTaskLinks {call} />
<ThreadMemoryLinks {call} />

<style>
  .tool-call {
    min-width: 0;
    color: var(--muted);
    font-size: 12px;
  }
  summary {
    display: flex;
    align-items: center;
    gap: 8px;
    min-height: 32px;
    padding: 4px 6px;
    border-radius: 7px;
    cursor: pointer;
    list-style: none;
    max-width: 100%;
    width: fit-content;
    transition: background 160ms ease;
  }
  summary::-webkit-details-marker {
    display: none;
  }
  summary:hover,
  details[open] > summary {
    background: var(--hover-surface);
  }
  summary:focus-visible {
    outline: 2px solid var(--muted);
    outline-offset: 2px;
  }
  svg {
    width: 15px;
    height: 15px;
    fill: none;
    stroke: currentColor;
    stroke-width: 1.5;
    stroke-linecap: round;
    stroke-linejoin: round;
    flex-shrink: 0;
  }
  .label {
    color: var(--text);
    font-weight: 500;
    min-width: 0;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }
  .status,
  .duration {
    font-size: 11px;
    white-space: nowrap;
  }
  .failed .tool-icon,
  .failed .status {
    color: var(--danger);
  }
  .chevron {
    width: 12px;
    height: 12px;
    transition: transform 160ms ease;
  }
  details[open] > summary > .chevron {
    transform: rotate(90deg);
  }
  .body {
    margin: 6px 0 8px 29px;
    padding: 14px 16px;
    border: 1px solid var(--panel-border);
    border-radius: 10px;
    background: var(--surface);
    min-width: 0;
  }
  .metadata {
    display: flex;
    flex-wrap: wrap;
    gap: 12px;
    font-size: 11px;
    overflow-wrap: anywhere;
  }
  section {
    margin-top: 14px;
    min-width: 0;
  }
  h4 {
    margin: 0 0 7px;
    font-size: 11px;
    font-weight: 500;
  }
  pre {
    margin: 0;
    white-space: pre-wrap;
    overflow-wrap: anywhere;
    max-height: 360px;
    overflow-y: auto;
    font: 12px/1.6 var(--font-mono, monospace);
    color: var(--text);
  }
  .file-change + .file-change {
    margin-top: 16px;
  }
  .file-path {
    display: flex;
    gap: 12px;
    align-items: baseline;
    justify-content: space-between;
    margin-bottom: 8px;
    color: var(--text);
    overflow-wrap: anywhere;
  }
  .file-kind {
    color: var(--muted);
    font-size: 11px;
    flex-shrink: 0;
  }
  .move-path {
    margin: 0 0 8px;
    overflow-wrap: anywhere;
  }
  .denial {
    border-left: 2px solid var(--danger);
    padding-left: 12px;
  }
  .denial h4 {
    color: var(--danger);
  }
  .denial p {
    margin: 0;
    white-space: pre-wrap;
    overflow-wrap: anywhere;
    line-height: 1.6;
    color: var(--text);
  }
  .denial .reason {
    margin-top: 4px;
    color: var(--muted);
  }
  .empty {
    margin: 0;
    font-size: 12px;
  }
  .streams {
    margin-top: 12px;
    border-top: 1px solid var(--panel-border);
    padding-top: 8px;
  }
  .spinner {
    width: 9px;
    height: 9px;
    border: 1.5px solid var(--panel-border);
    border-top-color: var(--muted);
    border-radius: 50%;
    animation: spin 1s linear infinite;
    flex-shrink: 0;
  }
  @keyframes spin {
    to {
      transform: rotate(360deg);
    }
  }
  @media (max-width: 480px) {
    .body {
      margin-left: 6px;
      padding: 12px;
    }
    .duration {
      display: none;
    }
  }
  @media (prefers-reduced-motion: reduce) {
    .spinner {
      animation: none;
    }
    summary,
    .chevron {
      transition: none;
    }
  }
</style>
