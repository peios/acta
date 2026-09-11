<script lang="ts">
  import { onMount, tick, type Snippet } from "svelte";
  import TaskPanelResizer from "$lib/components/tasks/TaskPanelResizer.svelte";
  let {
    selected,
    title,
    label,
    available,
    embedded = false,
    storageKey,
    children,
    actions,
    onclose,
    onlayout = () => {},
  }: {
    selected: string;
    title: string;
    label: string;
    available: number;
    embedded?: boolean;
    storageKey: string;
    children: Snippet;
    actions?: Snippet;
    onclose: () => void;
    onlayout?: (mode: string) => void;
  } = $props();
  let detail: HTMLDialogElement;
  let closeButton: HTMLButtonElement;
  let opener: HTMLElement | null = null;
  let width = $state(0),
    forced = $state(false),
    mounted = $state(false);
  const detailID = $props.id();
  const detailVisible = $derived(!!selected);
  const mode = $derived(
    forced || width < 760 ? "full" : available >= 1150 ? "panel" : "modal",
  );
  $effect(() => onlayout(mode));
  function close() {
    onclose();
  }
  function fullscreen(value: boolean) {
    forced = value;
    try {
      localStorage.setItem(`${storageKey}-fullscreen`, String(value));
    } catch {}
  }
  onMount(() => {
    width = window.innerWidth;
    try {
      forced = localStorage.getItem(`${storageKey}-fullscreen`) === "true";
      const saved = Number(localStorage.getItem(`${storageKey}-panel-width`));
      if (Number.isFinite(saved) && saved >= panelMinimum)
        preferredPanelWidth = Math.min(1000, saved);
    } catch {}
    mounted = true;
  });
  const panelDefault = 600;
  const panelMinimum = 440;
  let preferredPanelWidth = $state(panelDefault);
  let panelPreview = $state<number | null>(null);
  const panelMaximum = $derived(
    Math.max(panelMinimum, Math.min(1000, available - 388)),
  );
  const panelWidth = $derived(
    Math.round(
      Math.max(
        panelMinimum,
        Math.min(panelMaximum, panelPreview ?? preferredPanelWidth),
      ),
    ),
  );
  function resizePanel(value: number) {
    preferredPanelWidth = value;
    try {
      localStorage.setItem(`${storageKey}-panel-width`, String(value));
    } catch {}
  }
  $effect(() => {
    if (!mounted || !detail) return;
    const visible = detailVisible;
    const presentation = mode;
    const mobile = width < 760;
    let cancelled = false;
    let entrance: Animation | undefined;
    void tick().then(() => {
      if (cancelled) return;
      const wasOpen = detail.open;
      if (!wasOpen && visible)
        opener = document.activeElement as HTMLElement | null;
      const active = document.activeElement as HTMLElement | null;
      if (detail.open) detail.close();
      if (visible) {
        if (
          presentation === "modal" ||
          ((embedded || mobile) && presentation === "full")
        )
          detail.showModal();
        else detail.show();
        if (active && detail.contains(active))
          active.focus({ preventScroll: true });
        else closeButton?.focus({ preventScroll: true });
        if (
          !wasOpen &&
          presentation === "panel" &&
          !window.matchMedia("(prefers-reduced-motion: reduce)").matches
        ) {
          entrance = detail.animate(
            [
              { transform: "translateX(32px)", opacity: 0 },
              { transform: "translateX(0)", opacity: 1 },
            ],
            { duration: 220, easing: "cubic-bezier(0.2, 0.8, 0.2, 1)" },
          );
        }
      } else if (wasOpen && opener?.isConnected) {
        opener.focus({ preventScroll: true });
      }
    });
    return () => {
      cancelled = true;
      entrance?.cancel();
    };
  });
</script>

<svelte:window bind:innerWidth={width} />
<dialog
  bind:this={detail}
  id={detailID}
  style:--detail-panel-width={`${panelWidth}px`}
  class="detail-view {mode}"
  class:embedded
  aria-label={title || `${label} details`}
  oncancel={(e) => {
    e.preventDefault();
    close();
  }}
>
  {#if selected && mode === "panel"}
    <TaskPanelResizer
      {label}
      bind:preview={panelPreview}
      width={panelWidth}
      minimum={panelMinimum}
      maximum={panelMaximum}
      defaultWidth={panelDefault}
      controls={detailID}
      onchange={resizePanel}
    />
  {/if}
  <header>
    <div>
      <button
        bind:this={closeButton}
        class="view-control"
        aria-label={width < 760 ? `Back from ${label}` : `Close ${label}`}
        title={`Close ${label}`}
        onclick={close}
      >
        <svg
          viewBox="0 0 24 24"
          width="20"
          height="20"
          fill="none"
          stroke="currentColor"
          stroke-width="1.6"
          stroke-linecap="round"
          aria-hidden="true"
          ><path
            d={width < 760 ? "m14 5-7 7 7 7M7 12h14" : "m6 6 12 12M18 6 6 18"}
          /></svg
        >
        {#if width < 760}<span>Back</span>{/if}
      </button>
      <strong>{title}</strong>
    </div>
    <div>
      {#if mode !== "full"}<button
          class="view-control"
          title="Open full screen"
          aria-label="Open full screen"
          onclick={() => fullscreen(true)}
          ><svg
            viewBox="0 0 24 24"
            width="19"
            height="19"
            fill="none"
            stroke="currentColor"
            stroke-width="1.6"
            stroke-linecap="round"
            stroke-linejoin="round"
            aria-hidden="true"
            ><path d="M14 4h6v6M20 4l-6 6M10 20H4v-6m0 6 6-6" /></svg
          ></button
        >{:else if forced}<button
          class="view-control"
          title="Return to automatic layout"
          aria-label="Return to automatic layout"
          onclick={() => fullscreen(false)}
          ><svg
            viewBox="0 0 24 24"
            width="19"
            height="19"
            fill="none"
            stroke="currentColor"
            stroke-width="1.6"
            stroke-linecap="round"
            stroke-linejoin="round"
            aria-hidden="true"
            ><path d="M4 10h6V4M4 4l6 6M20 14h-6v6m6 0-6-6" /></svg
          ></button
        >{/if}
      {@render actions?.()}
    </div>
  </header>
  <div class="detail-body">{@render children()}</div>
</dialog>

<style>
  .detail-view {
    padding: 0;
    background: var(--surface);
    color: var(--text);
    border: 1px solid var(--panel-border);
    border-radius: 20px;
    max-height: none;
    max-width: none;
  }
  .detail-view[open] {
    display: flex;
    flex-direction: column;
  }
  .detail-view::backdrop {
    background: rgb(0 0 0/0.45);
    backdrop-filter: blur(2px);
  }
  .detail-view.modal {
    width: min(840px, calc(100vw - 64px));
    height: calc(100dvh - 72px);
    margin: auto;
    box-shadow: 0 24px 80px #0003;
  }
  .detail-view.panel {
    position: sticky;
    top: 0;
    margin: 0;
    width: var(--detail-panel-width);
    overflow: visible;
    flex-shrink: 0;
    height: calc(100svh - 2 * var(--content-block-padding, 24px));
  }
  .detail-view.full {
    position: relative;
    margin: 0 auto;
    width: 100%;
    max-width: 1100px;
    flex: 1;
    min-height: calc(100dvh - 140px);
    border: 0;
    border-radius: 0;
  }
  .detail-view header {
    padding: 14px 22px;
    display: flex;
    justify-content: space-between;
    align-items: center;
    border-bottom: 1px solid var(--panel-border);
    flex-shrink: 0;
    font-size: 13px;
  }
  .detail-view header > div {
    display: flex;
    align-items: center;
    gap: 8px;
  }
  .detail-view header strong {
    color: var(--muted);
    font-size: 12px;
    font-weight: 500;
  }
  .view-control {
    display: inline-flex;
    align-items: center;
    justify-content: center;
    min-width: 36px;
    min-height: 36px;
    background: transparent;
    border: 0;
    color: var(--muted);
    padding: 6px 8px;
    border-radius: 8px;
  }
  .view-control:hover {
    background: var(--hover-surface);
    color: var(--text);
  }
  .detail-body {
    overflow: auto;
    min-height: 0;
    flex: 1;
  }
  .detail-view.full header {
    position: sticky;
    top: calc(-1 * var(--content-block-padding, 24px));
    z-index: 6;
    background: var(--surface);
  }
  .detail-view.full .detail-body {
    overflow: visible;
  }
  .detail-view.embedded.panel {
    height: 100%;
    min-height: 0;
  }
  .detail-view.embedded.full {
    position: fixed;
    inset: 0;
    margin: 0;
    width: 100%;
    max-width: none;
    height: 100dvh;
    min-height: 0;
  }
  .detail-view.embedded.full .detail-body {
    overflow: auto;
  }
  @media (max-width: 759px) {
    .detail-view.full,
    .detail-view.embedded.full {
      --task-content-inset: 16px;
      --task-description-inset: 16px;
      position: fixed;
      inset: auto 0;
      top: var(--mobile-viewport-top, 0px);
      margin: 0;
      width: 100%;
      max-width: none;
      height: var(--mobile-viewport-height, 100dvh);
      min-height: 0;
      padding-bottom: env(safe-area-inset-bottom);
    }
    .detail-view.full .detail-body {
      overflow: auto;
      overscroll-behavior: contain;
    }
    .detail-view.full header {
      position: relative;
      top: auto;
      padding: max(8px, env(safe-area-inset-top)) 12px 8px;
      gap: 8px;
    }
    .detail-view header > div {
      min-width: 0;
    }
    .detail-view header strong {
      overflow: hidden;
      text-overflow: ellipsis;
      white-space: nowrap;
    }
    .view-control {
      gap: 4px;
      flex-shrink: 0;
    }
  }
</style>
