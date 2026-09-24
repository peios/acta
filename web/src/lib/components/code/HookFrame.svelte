<script lang="ts">
  import type { CATFrame } from "$lib/code-threads.svelte";
  import RelativeTime from "$lib/components/RelativeTime.svelte";
  let { frame, active }: { frame: CATFrame; active: boolean } = $props();
  const hook = $derived(frame.hook!);
  const running = $derived(
    frame.type !== "hook.context" &&
      hook.status === "running" &&
      !frame.superseded_by &&
      active,
  );
  const failed = $derived(hook.status === "failed");
  const label = $derived(
    frame.superseded_by
      ? "Superseded"
      : frame.type === "hook.context"
        ? "Context added"
        : hook.status === "running"
          ? active
            ? frame.type === "hook.started"
              ? "Starting"
              : "Running"
            : "Result unavailable"
          : hook.status.charAt(0).toUpperCase() + hook.status.slice(1),
  );
  const event = $derived(hook.event.replaceAll("_", " "));
  // Claude also supplies combined output. Prefer the separate streams for the
  // main view; the original combined field remains available in raw details.
  const outputs = $derived(
    hook.outputs.filter(
      (o) =>
        o.kind !== "output" ||
        !hook.outputs.some((s) => s.kind === "stdout" || s.kind === "stderr"),
    ),
  );
  const raw = $derived.by(() => {
    try {
      return JSON.stringify(JSON.parse(frame.raw || ""), null, 2);
    } catch {
      return frame.raw || "";
    }
  });
</script>

<article
  class="hook"
  class:superseded={!!frame.superseded_by}
  class:failed
  class:blocked={hook.status === "blocked"}
  aria-label={`Hook ${hook.name || event}: ${label}`}
>
  <details class="disclosure">
    <summary class="heading">
      <svg
        class="indicator"
        class:spinning={running}
        width="18"
        height="18"
        viewBox="0 0 24 24"
        fill="none"
        stroke="currentColor"
        stroke-width="1.6"
        stroke-linecap="round"
        stroke-linejoin="round"
        aria-hidden="true"
      >
        {#if running}<path d="M20 12a8 8 0 1 1-8-8" />
        {:else}<path d="M7 3v11a5 5 0 0 0 10 0V9l-3 3M4 3h6" />{/if}
      </svg>
      <strong title={hook.name || event}>{hook.name || event}</strong>
      <span class="status">{label}</span>
      <svg
        class="chevron"
        width="14"
        height="14"
        viewBox="0 0 24 24"
        fill="none"
        stroke="currentColor"
        stroke-width="1.7"
        stroke-linecap="round"
        stroke-linejoin="round"
        aria-hidden="true"><path d="m9 6 6 6-6 6" /></svg
      >
      <span class="time"><RelativeTime value={frame.at} /></span>
    </summary>
    <div class="contents">
      {#if hook.context}
        <section class="context" aria-label="Hook context">
          <h3>
            {hook.context.source === "context_item"
              ? "Added to context"
              : "Reported context"}
          </h3>
          {#each hook.context.texts as text}<pre>{text}</pre>{/each}
          {#if hook.context.source === "hook_response"}<p class="context-note">
              From the hook response; Claude does not report a separate
              insertion acknowledgement.
            </p>{/if}
        </section>
      {:else}
        <p class="context-note">
          {hook.status === "running"
            ? "Context is not available yet."
            : "No context content was reported for this hook."}
        </p>
      {/if}
      <details class="technical">
        <summary>Technical details</summary>
        <div class="meta">
          {event}{hook.handler_type
            ? ` · ${hook.handler_type}`
            : ""}{hook.exit_code !== undefined
            ? ` · exit ${hook.exit_code}`
            : ""}{hook.duration_ms !== undefined
            ? ` · ${hook.duration_ms} ms`
            : ""}
        </div>
        {#if hook.status_message}<p>{hook.status_message}</p>{/if}
        {#each outputs as output}
          <section class="output">
            <h3>{output.kind}</h3>
            <pre>{output.text}</pre>
          </section>
        {/each}
        <p class="provenance">
          {frame.type} · #{frame.seq} · source #{frame.source_seq}<br
          />{hook.id}
        </p>
        {#if frame.supersedes}<p class="provenance">
            Supersedes {frame.supersedes}
          </p>{/if}
        {#if frame.superseded_by}<p class="provenance">
            Superseded by {frame.superseded_by}
          </p>{/if}
        <details class="raw">
          <summary>Raw provider frame</summary>
          <pre>{raw}</pre>
        </details>
      </details>
    </div>
  </details>
</article>

<style>
  .hook {
    margin-bottom: 4px;
  }
  .heading {
    display: flex;
    align-items: center;
    gap: 10px;
    padding: 10px 4px;
    border-radius: 5px;
    font-size: 13px;
    line-height: 20px;
    list-style: none;
  }
  .heading::-webkit-details-marker {
    display: none;
  }
  .heading:hover {
    background: var(--hover-surface);
  }
  .heading:focus-visible {
    outline: 2px solid var(--accent);
    outline-offset: 2px;
  }
  .chevron {
    flex-shrink: 0;
    color: var(--muted);
    transition: transform 120ms ease;
  }
  .disclosure[open] > .heading .chevron {
    transform: rotate(90deg);
  }
  .contents {
    margin: 2px 0 12px 12px;
    padding: 6px 14px 8px 19px;
    border-left: 1px solid var(--panel-border);
  }
  strong {
    font-weight: 450;
    color: var(--subtle-text);
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
    min-width: 0;
  }
  .indicator {
    flex: 0 0 18px;
    color: var(--muted);
  }
  .spinning {
    animation: spin 1s linear infinite;
  }
  .status,
  .time {
    color: var(--muted);
    font-size: 12px;
    white-space: nowrap;
  }
  .meta {
    color: var(--muted);
    font-size: 11px;
    margin-bottom: 9px;
    text-transform: capitalize;
  }
  .failed .status,
  .failed .indicator {
    color: var(--danger);
  }
  .blocked .status,
  .blocked .indicator {
    color: #b87922;
  }
  .superseded {
    opacity: 0.65;
  }
  details {
    font-size: 10px;
    color: var(--muted);
  }
  summary {
    cursor: pointer;
  }
  p {
    font-size: 12px;
    color: var(--subtle-text);
    white-space: pre-wrap;
    overflow-wrap: anywhere;
  }
  .output,
  .raw {
    border-top: 1px solid var(--panel-border);
    padding-top: 10px;
    margin-top: 10px;
  }
  h3 {
    font-size: 10px;
    font-weight: 550;
    margin: 0 0 5px;
    text-transform: uppercase;
  }
  pre {
    margin: 6px 0;
    white-space: pre-wrap;
    overflow-wrap: anywhere;
    font:
      11px/1.6 ui-monospace,
      monospace;
    color: var(--subtle-text);
  }
  .provenance {
    font:
      10px/1.6 ui-monospace,
      monospace;
    color: var(--muted);
  }
  .context-note {
    color: var(--muted);
    font-size: 11px;
    line-height: 1.6;
  }
  .context {
    margin: 4px 0 12px;
  }
  .technical {
    margin-top: 12px;
  }
  .technical > .meta {
    margin-top: 10px;
  }
  @keyframes spin {
    to {
      transform: rotate(360deg);
    }
  }
  @media (prefers-reduced-motion: reduce) {
    .spinning {
      animation: none;
    }
    .chevron {
      transition: none;
    }
  }
  @media (max-width: 480px) {
    .heading {
      gap: 7px;
    }
    .time {
      display: none;
    }
  }
</style>
