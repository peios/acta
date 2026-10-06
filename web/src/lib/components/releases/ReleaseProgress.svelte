<script lang="ts">
  let { total, finished }: { total: number; finished: number } = $props();
  const percent = $derived(total ? Math.round((finished / total) * 100) : 0);
</script>

<span class="progress">
  <span
    class="bar"
    role="progressbar"
    aria-label="Finished tasks"
    aria-valuemin="0"
    aria-valuemax={total}
    aria-valuenow={finished}
    aria-valuetext={`${finished} of ${total} tasks finished`}
    ><span class="fill" style:width={`${percent}%`}></span></span
  ><span class="count"
    >{total ? `${finished} of ${total} finished` : "No tasks yet"}</span
  >
</span>

<style>
  .progress {
    display: flex;
    align-items: center;
    gap: 10px;
    font-size: 12px;
    color: var(--muted);
  }
  .bar {
    flex: 0 1 160px;
    height: 6px;
    border-radius: 3px;
    background: var(--panel-border);
    overflow: hidden;
  }
  .fill {
    display: block;
    height: 100%;
    background: var(--accent);
  }
  .count {
    white-space: nowrap;
  }
</style>
