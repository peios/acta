<script lang="ts">
  import { api, errorMessage } from "$lib/api";
  import type { Memory } from "$lib/memories";
  import MemoryDetail from "./MemoryDetail.svelte";
  import DetailViewer from "$lib/components/DetailViewer.svelte";
  let {
    selected,
    available,
    onclose,
  }: { selected: string; available: number; onclose: () => void } = $props();
  let memory = $state<Memory | null>(null),
    error = $state("");
  let retry = $state(0);
  $effect(() => {
    const id = selected;
    retry;
    const abort = new AbortController();
    memory = null;
    error = "";
    if (id)
      void api<Memory>(`memories/${id}`, undefined, { signal: abort.signal })
        .then((value) => {
          if (!abort.signal.aborted) memory = value;
        })
        .catch((e) => {
          if (!abort.signal.aborted) error = errorMessage(e);
        });
    return () => abort.abort();
  });
</script>

<DetailViewer
  {selected}
  {available}
  {onclose}
  title="Memory"
  label="memory"
  storageKey="acta.memory"
  embedded
>
  <div class="memory-body">
    {#if error}<p class="notice error" role="alert">{error}</p>
      <button class="secondary" onclick={() => retry++}>Retry</button>
    {:else if memory}{#key memory.id}<MemoryDetail
          {memory}
          onchange={(value) => (memory = value)}
          ondelete={onclose}
        />{/key}
    {:else}<p class="hint" role="status">Loading memory…</p>{/if}
  </div>
</DetailViewer>

<style>
  .memory-body {
    padding: 24px;
  }
  @media (max-width: 600px) {
    .memory-body {
      padding: 16px;
    }
  }
</style>
