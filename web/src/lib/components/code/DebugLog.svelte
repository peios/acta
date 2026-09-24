<script lang="ts">
  import type { CATFrame } from "$lib/code-threads.svelte";
  let { frame }: { frame: CATFrame } = $props();
  const timestamp = $derived(frame.timestamp || frame.at);
  const raw = $derived.by(() => {
    try {
      return JSON.stringify(JSON.parse(frame.raw || ""), null, 2);
    } catch {
      return frame.raw || "";
    }
  });
</script>

<article
  class="log"
  class:warning={frame.level === "warn"}
  class:error={frame.level === "error" || frame.level === "fatal"}
  aria-label={`Frame ${frame.seq}: debug.log ${frame.level}`}
>
  <div class="heading">
    <span class="level">{frame.level}</span>
    <span class="target" title={frame.target}
      >{frame.target || frame.provider}</span
    >
    <time datetime={timestamp} title={timestamp}
      >{new Date(timestamp).toLocaleTimeString()}</time
    >
  </div>
  <p>{frame.message}</p>
  <details>
    <summary
      >Details <span
        >debug.log · #{frame.seq} · {frame.stream} · source #{frame.source_seq}</span
      ></summary
    >
    <pre>{raw}</pre>
  </details>
</article>

<style>
  .log {
    --log-color: var(--muted);
    border: 1px solid var(--panel-border);
    border-left: 3px solid var(--log-color);
    border-radius: 8px;
    margin-bottom: 10px;
    padding: 12px 14px;
    background: color-mix(in srgb, var(--log-color) 4%, transparent);
  }
  .warning {
    --log-color: #b87922;
  }
  .error {
    --log-color: var(--danger);
  }
  .heading {
    display: flex;
    gap: 10px;
    align-items: center;
    font-size: 10px;
    color: var(--muted);
  }
  .level {
    color: var(--log-color);
    background: color-mix(in srgb, var(--log-color) 12%, transparent);
    border-radius: 4px;
    padding: 2px 5px;
    font-weight: 650;
    text-transform: uppercase;
  }
  .target {
    min-width: 0;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
    font-family: ui-monospace, monospace;
  }
  time {
    margin-left: auto;
    white-space: nowrap;
  }
  p {
    margin: 9px 0;
    white-space: pre-wrap;
    overflow-wrap: anywhere;
    font-size: 12px;
    line-height: 1.6;
    color: var(--subtle-text);
  }
  details {
    color: var(--muted);
    font-size: 10px;
  }
  summary {
    cursor: pointer;
  }
  summary span {
    margin-left: 8px;
    overflow-wrap: anywhere;
  }
  pre {
    margin: 10px 0 0;
    padding-top: 10px;
    border-top: 1px solid var(--panel-border);
    white-space: pre-wrap;
    overflow-wrap: anywhere;
    font:
      11px/1.6 ui-monospace,
      monospace;
  }
</style>
