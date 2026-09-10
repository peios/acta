<script lang="ts">
  import { diffLines } from "$lib/thread-diffs.js";
  let { text, format = "unified" }: { text: string; format?: string } =
    $props();
  const lines = $derived(diffLines(text, format));
</script>

{#if lines.length}
  <!-- svelte-ignore a11y_no_noninteractive_tabindex (The scrollable diff needs keyboard scrolling.) -->
  <div class="diff" tabindex="0" role="region" aria-label="File changes">
    <pre><code
        >{#each lines as line}<span class={line.kind}
            >{line.text.endsWith("\n")
              ? line.text.slice(0, -1)
              : line.text}</span
          >{/each}</code
      ></pre>
  </div>
{:else}
  <p class="empty">No text changes.</p>
{/if}

<style>
  .diff {
    overflow: auto;
    max-height: 420px;
    border: 1px solid var(--panel-border);
    border-radius: 8px;
    background: var(--surface);
  }
  .diff:focus-visible {
    outline: 2px solid var(--muted);
    outline-offset: 2px;
  }
  pre {
    margin: 0;
    padding: 8px 0;
    font: 12px/1.7 var(--font-mono, monospace);
    width: max-content;
    min-width: 100%;
  }
  code {
    font: inherit;
  }
  span {
    display: block;
    padding: 0 12px;
    min-height: 1.7em;
    white-space: pre;
  }
  .addition {
    background: var(--success-surface);
    color: var(--success-text);
  }
  .deletion {
    background: color-mix(in srgb, var(--danger) 12%, transparent);
    color: var(--danger);
  }
  .header {
    color: var(--muted);
  }
  .hunk {
    color: var(--accent);
    background: var(--hover-surface);
  }
  .context {
    color: var(--text);
  }
  .empty {
    color: var(--muted);
    font-size: 12px;
    margin: 0;
  }
</style>
