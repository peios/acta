<script lang="ts">
  import { releaseStates } from "$lib/releases";
  let { state }: { state: string } = $props();
  const info = $derived(releaseStates.find((s) => s.value === state));
</script>

<span class={`state ${state}`} title={info?.hint}>{info?.label ?? state}</span>

<style>
  .state {
    font-size: 11px;
    font-weight: 500;
    padding: 3px 8px;
    border-radius: 10px;
    white-space: nowrap;
    border: 1px solid var(--panel-border);
    color: var(--muted);
  }
  .open {
    background: var(--scope-active);
    color: var(--accent);
    border-color: transparent;
  }
  .frozen {
    color: var(--text);
    border-style: dashed;
  }
</style>
