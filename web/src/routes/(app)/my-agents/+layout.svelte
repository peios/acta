<script lang="ts">
  import type { Snippet } from "svelte";
  import NewThread from "$lib/components/threads/NewThread.svelte";
  import { useThreads } from "$lib/threads.svelte";
  let { children }: { children: Snippet } = $props();
  const threads = useThreads();
</script>

<NewThread />
{#if threads.pending}<p class="hint" role="status">
    Waiting for the harness to advertise your new thread…
  </p>{/if}
{#if threads.notice}<p class="notice" role="status">{threads.notice}</p>{/if}
{#if threads.error}<p class="notice error" role="alert">{threads.error}</p>{/if}
{@render children()}
