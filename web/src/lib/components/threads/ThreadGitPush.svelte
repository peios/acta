<script lang="ts">
  let {
    data,
    timestamp,
  }: { data: Record<string, unknown>; timestamp: string } = $props();
  const cwd = $derived(String(data.cwd ?? ""));
  const directory = $derived(cwd.split(/[\\/]/).filter(Boolean).at(-1) || cwd);
</script>

<div class="git-push" title={`${cwd}\n${new Date(timestamp).toLocaleString()}`}>
  <svg viewBox="0 0 20 20" aria-hidden="true"
    ><path d="M10 13V3m-4 4 4-4 4 4M4 13v4h12v-4" /></svg
  >
  <span>Pushed <strong>{String(data.branch ?? "")}</strong></span>
  <span class="directory">{directory}</span>
</div>

<style>
  .git-push {
    display: flex;
    align-items: center;
    gap: 8px;
    padding: 7px 6px;
    color: var(--muted);
    font-size: 12px;
    min-width: 0;
  }
  svg {
    width: 15px;
    height: 15px;
    flex-shrink: 0;
    fill: none;
    stroke: currentColor;
    stroke-width: 1.5;
    stroke-linecap: round;
    stroke-linejoin: round;
  }
  strong {
    color: var(--text);
    font-weight: 500;
    overflow-wrap: anywhere;
  }
  .directory {
    opacity: 0.75;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }
</style>
