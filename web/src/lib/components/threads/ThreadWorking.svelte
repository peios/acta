<script lang="ts">
  let {
    active,
    compacting = false,
    waiting = false,
    question = false,
    background = 0,
  }: {
    active: boolean;
    compacting?: boolean;
    waiting?: boolean;
    question?: boolean;
    background?: number;
  } = $props();
</script>

<div
  class="working"
  class:active={active || compacting || background > 0}
  role="status"
  aria-live="polite"
>
  {#if active || compacting}
    <span class="pulse" aria-hidden="true"></span>
    <span
      >{compacting
        ? "Compacting context…"
        : question
          ? "Waiting for your answer"
          : waiting
            ? "Waiting for approval"
            : "Working…"}</span
    >
  {/if}
  {#if background > 0}
    {#if active || compacting}<span aria-hidden="true">·</span>{/if}
    <span
      >{background}
      {background === 1 ? "task" : "tasks"} running in background</span
    >
  {/if}
</div>

<style>
  .working {
    flex-shrink: 0;
  }
  .working.active {
    display: flex;
    align-items: center;
    flex-wrap: wrap;
    gap: 9px;
    padding: 0 12px 12px;
    color: var(--muted);
    font-size: 12px;
    line-height: 20px;
  }
  .pulse {
    width: 6px;
    height: 6px;
    border-radius: 50%;
    background: currentColor;
    animation: breathe 1.8s ease-in-out infinite;
  }
  @keyframes breathe {
    0%,
    100% {
      opacity: 0.35;
    }
    50% {
      opacity: 1;
    }
  }
  @media (prefers-reduced-motion: reduce) {
    .pulse {
      animation: none;
    }
  }
</style>
