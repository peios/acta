<script lang="ts">
  import type { ThinkingItem } from "$lib/thread-thinking.js";
  import MarkdownView from "$lib/components/tasks/MarkdownView.svelte";
  let {
    thinking,
    live,
    turnEnded,
  }: { thinking: ThinkingItem; live: boolean; turnEnded: boolean } = $props();
  let now = $state(Date.now());
  const running = $derived(
    !thinking.completed && !thinking.interrupted && live,
  );
  const hasText = $derived(thinking.text.trim().length > 0);
  let expanded = $state(false);
  $effect(() => {
    expanded = hasText && !turnEnded;
  });
  $effect(() => {
    if (!running) return;
    now = Date.now();
    const timer = setInterval(() => {
      now = Date.now();
    }, 1000);
    return () => clearInterval(timer);
  });
  // An unavailable provider has no reliable completion time. Do not invent one
  // or keep an elapsed timer running from an old capture.
  const seconds = $derived(
    thinking.started && (thinking.ended || running)
      ? Math.max(
          0,
          Math.floor(
            ((thinking.ended ? Date.parse(thinking.ended) : now) -
              Date.parse(thinking.started)) /
              1000,
          ),
        )
      : null,
  );
  const label = $derived(
    running
      ? seconds
        ? `Thinking for ${seconds}s`
        : "Thinking…"
      : thinking.completed
        ? seconds !== null
          ? `Thought for ${seconds === 0 ? "<1" : seconds}s`
          : "Thought"
        : live
          ? "Thinking interrupted"
          : "Thinking unavailable",
  );
</script>

<div class="thinking" class:running>
  {#snippet heading()}
    <span class="indicator" aria-hidden="true"
      >{#if !running}<svg viewBox="0 0 20 20"
          ><path
            d="M7 15h6m-5 3h4M6 10a5 5 0 1 1 8 0c-1 1-1 2-1 3H7c0-1 0-2-1-3Z"
          /></svg
        >{/if}</span
    >
    <span>{label}</span>
    {#if thinking.tokens !== null}<span class="tokens"
        >· ~{thinking.tokens.toLocaleString()} tokens</span
      >{/if}
    {#if hasText}<svg
        class="chevron"
        class:expanded
        viewBox="0 0 20 20"
        aria-hidden="true"><path d="m8 6 4 4-4 4" /></svg
      >{/if}
  {/snippet}
  {#if hasText}
    <button
      type="button"
      class="heading"
      aria-expanded={expanded}
      onclick={() => (expanded = !expanded)}>{@render heading()}</button
    >
  {:else}<div class="heading">{@render heading()}</div>{/if}
  {#if hasText && expanded}<div class="content">
      <MarkdownView value={thinking.text} />
    </div>{/if}
</div>

<style>
  .thinking {
    min-width: 0;
    color: var(--muted);
    font-size: 12px;
  }
  .heading {
    display: flex;
    align-items: center;
    gap: 8px;
    min-height: 32px;
    padding: 4px 6px;
    border: 0;
    border-radius: 7px;
    background: transparent;
    color: inherit;
    font: inherit;
    width: fit-content;
    max-width: 100%;
    text-align: left;
  }
  button.heading {
    cursor: pointer;
    transition: background 160ms ease;
  }
  button.heading:hover {
    background: var(--hover-surface);
  }
  button.heading:focus-visible {
    outline: 2px solid var(--muted);
    outline-offset: 2px;
  }
  svg {
    width: 15px;
    height: 15px;
    fill: none;
    stroke: currentColor;
    stroke-width: 1.5;
    stroke-linecap: round;
    stroke-linejoin: round;
  }
  .indicator {
    display: flex;
    width: 15px;
    height: 15px;
    align-items: center;
    justify-content: center;
    flex-shrink: 0;
  }
  .running .indicator {
    width: 11px;
    height: 11px;
    margin: 2px;
    border: 1.5px solid var(--panel-border);
    border-top-color: var(--muted);
    border-radius: 50%;
    animation: spin 1s linear infinite;
  }
  .tokens {
    font-size: 11px;
  }
  .chevron {
    width: 12px;
    height: 12px;
    flex-shrink: 0;
    transition: transform 160ms ease;
  }
  .chevron.expanded {
    transform: rotate(90deg);
  }
  .content {
    margin: 6px 0 8px 29px;
    padding: 12px 16px;
    border-left: 2px solid var(--panel-border);
    font-size: 14px;
    color: var(--muted);
    min-width: 0;
  }
  @keyframes spin {
    to {
      transform: rotate(360deg);
    }
  }
  @media (prefers-reduced-motion: reduce) {
    .running .indicator {
      animation: none;
    }
    .chevron,
    button.heading {
      transition: none;
    }
  }
</style>
