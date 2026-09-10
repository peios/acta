<script lang="ts">
  import { renderTaskMarkdown } from "$lib/task-markdown.js";
  import "$lib/task-rich-text.css";
  let { value }: { value: string } = $props();
  let host = $state<HTMLDivElement>();
  $effect(() => {
    if (host) host.replaceChildren(renderTaskMarkdown(value));
  });
</script>

{#if value.trim()}<div class="task-rich-text" bind:this={host}></div>
{:else}<p class="empty-description">No description yet.</p>{/if}

<style>
  .empty-description {
    margin: 12px 0;
    color: var(--muted);
    font-size: 14px;
  }
</style>
