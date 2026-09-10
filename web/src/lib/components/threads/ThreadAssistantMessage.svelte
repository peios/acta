<script lang="ts">
  import type { AssistantMessage } from "$lib/thread-feed.js";
  import MarkdownView from "$lib/components/tasks/MarkdownView.svelte";
  let { message }: { message: AssistantMessage } = $props();
</script>

<article
  class="assistant-message"
  aria-label="Assistant message"
  data-phase={message.phase}
>
  <MarkdownView value={message.text} />
</article>

<style>
  .assistant-message {
    min-width: 0;
    padding: 4px 2px;
    color: var(--text);
    font-size: 14px;
  }
</style>
