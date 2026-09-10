<script lang="ts">
  import {
    taskReferences,
    type TaskReference,
  } from "$lib/thread-task-references.js";
  import { useThreadTasks } from "$lib/thread-task-context";
  import type { ToolCallItem } from "$lib/thread-tool-calls.js";
  let { call }: { call: ToolCallItem } = $props();
  const tasks = useThreadTasks();
  const references = $derived(taskReferences(call));
  let verified = $state<TaskReference[]>([]);
  $effect(() => {
    let cancelled = false;
    verified = [];
    if (tasks)
      void Promise.all(references.map((r) => tasks.resolve(r.id))).then(
        (rows) => {
          if (!cancelled)
            verified = rows.filter((r): r is TaskReference => r !== null);
        },
      );
    return () => {
      cancelled = true;
    };
  });
</script>

{#if references.length && tasks}
  <div class="task-references" aria-label="Referenced tasks">
    {#each references as reference (reference.id)}
      {@const task = verified.find((t) => t.id === reference.id)}
      {#if task}
        <button
          type="button"
          onclick={() => tasks.open(task.id)}
          title={`Open ${task.reference}: ${task.title}`}
        >
          <svg viewBox="0 0 20 20" aria-hidden="true"
            ><rect x="3" y="3" width="14" height="14" rx="3" /><path
              d="m6 10 3 3 5-6"
            /></svg
          >
          <span class="reference">{task.reference}</span><span class="title"
            >{task.title}</span
          >
          <svg class="arrow" viewBox="0 0 20 20" aria-hidden="true"
            ><path d="m8 5 5 5-5 5" /></svg
          >
        </button>
      {:else}
        <span class="unresolved"
          >{reference.reference || reference.id}{reference.title
            ? ` · ${reference.title}`
            : ""}</span
        >
      {/if}
    {/each}
  </div>
{/if}

<style>
  .task-references {
    display: flex;
    flex-wrap: wrap;
    gap: 6px;
    margin: 4px 0 6px 26px;
    min-width: 0;
  }
  button {
    display: inline-flex;
    align-items: center;
    gap: 7px;
    min-width: 0;
    max-width: 100%;
    padding: 6px 9px;
    border: 1px solid var(--panel-border);
    border-radius: 8px;
    background: var(--surface);
    color: var(--text);
    font-size: 12px;
    text-align: left;
  }
  button:hover {
    background: var(--hover-surface);
    border-color: var(--accent);
  }
  svg {
    width: 14px;
    height: 14px;
    flex: 0 0 auto;
    fill: none;
    stroke: currentColor;
    stroke-width: 1.4;
  }
  .reference {
    color: var(--muted);
    white-space: nowrap;
  }
  .title {
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }
  .arrow {
    color: var(--muted);
  }
  .unresolved {
    color: var(--muted);
    font-size: 12px;
    overflow-wrap: anywhere;
  }
</style>
