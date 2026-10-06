<script lang="ts">
  import { releaseStates, type ReleaseState } from "$lib/releases";
  let {
    value,
    onchange,
    disabled = false,
  }: {
    value: ReleaseState;
    onchange: (value: ReleaseState) => void;
    disabled?: boolean;
  } = $props();
</script>

<div class="state-picker" role="group" aria-label="Release state">
  {#each releaseStates as s}<button
      type="button"
      aria-pressed={value === s.value}
      title={s.hint}
      {disabled}
      onclick={() => {
        if (value !== s.value) onchange(s.value);
      }}>{s.label}</button
    >{/each}
</div>
<p class="state-hint">
  {releaseStates.find((s) => s.value === value)?.hint}
</p>

<style>
  .state-picker {
    display: inline-flex;
    flex-wrap: wrap;
    padding: 3px;
    gap: 3px;
    border-radius: 7px;
    background: var(--hover-surface);
  }
  .state-picker button {
    width: auto;
    border: 0;
    border-radius: 5px;
    background: transparent;
    color: var(--muted);
    padding: 7px 14px;
    font-size: 12px;
  }
  .state-picker button[aria-pressed="true"] {
    color: var(--text);
    background: var(--surface);
    box-shadow: 0 1px 3px #0002;
  }
  .state-hint {
    margin: 8px 0 0;
    font-size: 13px;
    color: var(--muted);
  }
</style>
