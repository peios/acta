<script lang="ts">
  import type { HookItem } from "$lib/thread-feed.js";
  import MarkdownView from "$lib/components/tasks/MarkdownView.svelte";
  import RelativeTime from "../RelativeTime.svelte";
  let { hook }: { hook: HookItem } = $props();
  const data = $derived(hook.frame.data ?? {});
  const status = $derived(String(data.status ?? "running"));
  const failed = $derived(status === "failed" || status === "blocked");
  const stateLabel = $derived(
    (
      {
        completed: "Completed",
        failed: "Failed",
        blocked: "Blocked",
        cancelled: "Cancelled",
      } as Record<string, string>
    )[status] ?? "Running",
  );
  const entries = $derived(
    Array.isArray(data.entries)
      ? (data.entries as { kind: string; text: string }[])
      : [],
  );
  const streams = $derived(
    ["output", "stdout", "stderr"].flatMap((key) =>
      typeof data[key] === "string" &&
      data[key] !== "" &&
      !(key === "stdout" && data.stdout === data.output)
        ? [{ key, text: data[key] as string }]
        : [],
    ),
  );
  const entryLabels: Record<string, string> = {
    context: "Added to context",
    feedback: "Feedback",
    warning: "Warning",
    error: "Error",
    stop: "Stop",
  };
  // Recognize the explicit context field without treating arbitrary hook output
  // as context. This also renders already-stored hook response frames.
  function additionalContext(text: string): string | null {
    try {
      const value = JSON.parse(text)?.hookSpecificOutput?.additionalContext;
      return typeof value === "string" && value.trim() ? value : null;
    } catch {
      return null;
    }
  }
  function format(text: string) {
    try {
      return JSON.stringify(JSON.parse(text), null, 2);
    } catch {
      return text;
    }
  }
</script>

{#snippet icon()}
  <svg class="hook-icon" viewBox="0 0 20 20" aria-hidden="true"
    ><path d="M6 3v9a4 4 0 0 0 8 0V9m-3 3 3-3 3 3M3 3h6" /></svg
  >
{/snippet}

{#if !hook.completed}
  <div class="hook-running" role="status">
    {@render icon()}<span class="spinner"></span><span
      >Running hook <strong>{hook.name}</strong></span
    >
  </div>
{:else}
  <details class="hook-result" class:failed>
    <summary>
      {@render icon()}<span class="name">{hook.name}</span>
      <span class="outcome">{stateLabel}</span>
      <svg class="chevron" viewBox="0 0 20 20" aria-hidden="true"
        ><path d="m8 6 4 4-4 4" /></svg
      >
      <span class="time"><RelativeTime value={hook.frame.received_at} /></span>
    </summary>
    <div class="response">
      <div class="metadata">
        <span>{hook.event}</span>
        {#if typeof data.duration_ms === "number"}<span
            >{(data.duration_ms / 1000).toFixed(1)}s</span
          >{/if}
        {#if typeof data.exit_code === "number"}<span
            >Exit {data.exit_code}</span
          >{/if}
      </div>
      {#if typeof data.status_message === "string" && data.status_message}<p>
          {data.status_message}
        </p>{/if}
      {#each streams as stream}
        {@const context =
          stream.key !== "stderr" ? additionalContext(stream.text) : null}
        <section>
          {#if context}
            <h4>Added to context</h4>
            <div class="context-content"><MarkdownView value={context} /></div>
            <details class="raw-response">
              <summary
                ><svg class="chevron" viewBox="0 0 20 20" aria-hidden="true"
                  ><path d="m8 6 4 4-4 4" /></svg
                >Raw hook response</summary
              >
              <pre>{format(stream.text)}</pre>
            </details>
          {:else}
            <h4>
              {stream.key === "output"
                ? data.stdout === data.output
                  ? "Response · stdout"
                  : "Response"
                : stream.key === "stdout"
                  ? "Standard output"
                  : "Standard error"}
            </h4>
            <pre>{format(stream.text)}</pre>
          {/if}
        </section>
      {/each}
      {#each entries as entry}
        <section>
          <h4>{entryLabels[entry.kind] ?? entry.kind}</h4>
          {#if entry.kind === "context"}
            <div class="context-content">
              <MarkdownView value={entry.text} />
            </div>
          {:else}<pre>{format(entry.text)}</pre>{/if}
        </section>
      {/each}
      {#if !streams.length && !entries.length}<p class="empty">
          No output returned.
        </p>{/if}
    </div>
  </details>
{/if}

<style>
  .hook-result,
  .hook-running {
    min-width: 0;
    font-size: 12px;
    color: var(--muted);
  }
  summary,
  .hook-running {
    display: flex;
    align-items: center;
    gap: 9px;
    min-height: 32px;
    padding: 4px 6px;
    border-radius: 7px;
  }
  summary {
    cursor: pointer;
    list-style: none;
    width: fit-content;
    max-width: 100%;
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
  .name,
  strong {
    font-weight: 500;
    color: var(--text);
    overflow-wrap: anywhere;
    min-width: 0;
  }
  .outcome {
    font-size: 10px;
    white-space: nowrap;
    opacity: 0.85;
  }
  .failed .hook-icon,
  .failed .outcome {
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
  .time {
    font-size: 11px;
    white-space: nowrap;
  }
  .response {
    margin: 6px 0 4px 29px;
    padding: 14px 16px;
    background: var(--surface);
    border: 1px solid var(--panel-border);
    border-radius: 10px;
    min-width: 0;
  }
  .context-content {
    color: var(--text);
    font-size: 14px;
    min-width: 0;
  }
  .raw-response {
    margin-top: 16px;
    padding-top: 8px;
    border-top: 1px solid var(--panel-border);
  }
  .raw-response > summary {
    font-size: 11px;
  }
  .raw-response > pre {
    margin-top: 10px;
  }
  .metadata {
    display: flex;
    flex-wrap: wrap;
    gap: 12px;
    font-size: 11px;
  }
  section {
    margin-top: 14px;
    min-width: 0;
  }
  h4 {
    margin: 0 0 7px;
    font-size: 11px;
    font-weight: 500;
    color: var(--muted);
  }
  pre {
    margin: 0;
    color: var(--text);
    white-space: pre-wrap;
    overflow-wrap: anywhere;
    font-family: var(--font-mono, monospace);
    font-size: 12px;
    line-height: 1.6;
  }
  p {
    white-space: pre-wrap;
    overflow-wrap: anywhere;
  }
  .empty {
    margin-bottom: 0;
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
    .time {
      display: none;
    }
    .response {
      margin-left: 6px;
      padding: 12px;
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
