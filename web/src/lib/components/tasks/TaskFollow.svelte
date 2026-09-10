<script lang="ts">
  import { onMount } from "svelte";
  import { api, errorMessage } from "$lib/api";
  let { task }: { task: string } = $props();
  let following = $state(false),
    ready = $state(false),
    busy = $state(false),
    error = $state("");
  let generation = 0;
  const controller = new AbortController();
  async function refresh() {
    if (busy) return;
    const version = ++generation;
    try {
      const value = await api<{ following: boolean }>(
        `tasks/${task}/following`,
        undefined,
        { signal: controller.signal },
      );
      if (controller.signal.aborted || version !== generation) return;
      following = value.following;
      ready = true;
      error = "";
    } catch (e) {
      if (!controller.signal.aborted && version === generation)
        error = errorMessage(e);
    }
  }
  async function toggle() {
    if (busy) return;
    if (!ready) {
      await refresh();
      return;
    }
    busy = true;
    ++generation;
    error = "";
    try {
      const value = await api<{ following: boolean }>(
        `tasks/${task}/following`,
        { following: !following },
        { signal: controller.signal },
      );
      if (!controller.signal.aborted) following = value.following;
    } catch (e) {
      if (!controller.signal.aborted) error = errorMessage(e);
    } finally {
      busy = false;
    }
  }
  onMount(() => {
    void refresh();
    const focus = () => void refresh();
    window.addEventListener("focus", focus);
    window.addEventListener("acta:tasks-changed", focus);
    return () => {
      controller.abort();
      window.removeEventListener("focus", focus);
      window.removeEventListener("acta:tasks-changed", focus);
    };
  });
</script>

<div class="follow-control">
  <button
    class:following
    disabled={busy}
    aria-pressed={following}
    aria-label={following ? "Unfollow task" : "Follow task"}
    title={following ? "Following · Click to unfollow" : "Follow task updates"}
    onclick={() => void toggle()}
  >
    <svg
      width="19"
      height="19"
      viewBox="0 0 24 24"
      fill={following ? "currentColor" : "none"}
      stroke="currentColor"
      stroke-width="1.6"
      aria-hidden="true"
      ><path d="M18 8a6 6 0 0 0-12 0c0 7-3 7-3 9h18c0-2-3-2-3-9Z" /><path
        d="M10 21h4"
      /></svg
    >
  </button>
  {#if error}<div class="follow-error" role="alert">
      {error}<button onclick={() => void refresh()}>Retry</button>
    </div>{/if}
</div>

<style>
  .follow-control {
    position: relative;
    display: flex;
  }
  button {
    display: inline-flex;
    align-items: center;
    justify-content: center;
    min-width: 36px;
    min-height: 36px;
    padding: 6px 8px;
    border: 0;
    border-radius: 8px;
    background: transparent;
    color: var(--muted);
  }
  button:hover {
    background: var(--hover-surface);
    color: var(--text);
  }
  button.following {
    color: var(--accent);
  }
  .follow-error {
    position: absolute;
    right: 0;
    top: 100%;
    width: 240px;
    padding: 12px;
    background: var(--surface);
    border: 1px solid var(--border);
    border-radius: 8px;
    box-shadow: 0 8px 24px #0003;
    font-size: 13px;
    z-index: 10;
  }
</style>
