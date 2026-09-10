<script lang="ts">
  import { onMount } from "svelte";
  import {
    SIDEBAR_DEFAULT,
    SIDEBAR_MIN,
    SIDEBAR_MAX,
    SIDEBAR_RAIL,
    clampSidebarWidth,
    resizeSidebar,
    restoreSidebarSize,
  } from "$lib/sidebar-size";

  let {
    width = $bindable(SIDEBAR_DEFAULT),
    collapsed = $bindable(false),
    dragging = $bindable(false),
    controls,
  }: {
    width?: number;
    collapsed?: boolean;
    dragging?: boolean;
    controls: string;
  } = $props();
  const helpId = $props.id();
  const storageKey = "acta.sidebar";
  let ready = $state(false);
  let handle = $state<HTMLDivElement>();
  let gesture: {
    id: number;
    x: number;
    renderedWidth: number;
    width: number;
    collapsed: boolean;
  } | null = null;

  onMount(() => {
    try {
      const saved = restoreSidebarSize(localStorage.getItem(storageKey));
      width = saved.width;
      collapsed = saved.collapsed;
    } catch {
      /* Storage may be blocked; resizing still works for this page. */
    }
    ready = true;
    return () => finish(true);
  });
  $effect(() => {
    if (ready && !dragging) {
      try {
        localStorage.setItem(storageKey, JSON.stringify({ width, collapsed }));
      } catch {
        /* Nonessential preference. */
      }
    }
  });

  function start(event: PointerEvent) {
    if (event.button !== 0 || !event.isPrimary || gesture || !handle) return;
    event.preventDefault();
    handle.focus();
    gesture = {
      id: event.pointerId,
      x: event.clientX,
      renderedWidth:
        handle.parentElement?.getBoundingClientRect().width ?? width,
      width,
      collapsed,
    };
    handle.setPointerCapture(event.pointerId);
    dragging = true;
  }
  function move(event: PointerEvent) {
    if (!gesture || event.pointerId !== gesture.id) return;
    const candidate = gesture.renderedWidth + event.clientX - gesture.x;
    const next = resizeSidebar({ width, collapsed }, candidate, gesture.width);
    width = next.width;
    collapsed = next.collapsed;
  }
  function finish(cancel = false) {
    if (!gesture) return;
    const previous = gesture;
    gesture = null;
    if (cancel) {
      width = previous.width;
      collapsed = previous.collapsed;
    }
    dragging = false;
    if (handle?.hasPointerCapture(previous.id))
      handle.releasePointerCapture(previous.id);
  }
  function reset() {
    width = SIDEBAR_DEFAULT;
    collapsed = false;
  }
  function keydown(event: KeyboardEvent) {
    if (event.key === "Escape" && gesture) {
      event.preventDefault();
      finish(true);
      return;
    }
    if (gesture) return;
    const step = event.shiftKey ? 32 : 16;
    switch (event.key) {
      case "ArrowLeft":
        if (!collapsed && width <= SIDEBAR_MIN) collapsed = true;
        else if (!collapsed) width = clampSidebarWidth(width - step);
        break;
      case "ArrowRight":
        if (collapsed) collapsed = false;
        else width = clampSidebarWidth(width + step);
        break;
      case "Home":
        collapsed = true;
        break;
      case "End":
        width = SIDEBAR_MAX;
        collapsed = false;
        break;
      case "Enter":
        collapsed = !collapsed;
        break;
      default:
        return;
    }
    event.preventDefault();
  }
</script>

<svelte:window onresize={() => finish(true)} />
<!-- svelte-ignore a11y_no_noninteractive_tabindex, a11y_no_noninteractive_element_interactions (A focusable separator is the ARIA window-splitter widget: https://www.w3.org/WAI/ARIA/apg/patterns/windowsplitter/.) -->
<div
  bind:this={handle}
  class="resize-handle"
  class:dragging
  role="separator"
  tabindex="0"
  aria-label="Resize sidebar"
  aria-orientation="vertical"
  aria-controls={controls}
  aria-valuemin={SIDEBAR_RAIL}
  aria-valuemax={SIDEBAR_MAX}
  aria-valuenow={collapsed ? SIDEBAR_RAIL : width}
  aria-valuetext={collapsed ? "Collapsed" : `${width} pixels`}
  aria-describedby={helpId}
  onpointerdown={start}
  onpointermove={move}
  onpointerup={(event) => {
    if (event.pointerId === gesture?.id) finish();
  }}
  onpointercancel={(event) => {
    if (event.pointerId === gesture?.id) finish(true);
  }}
  onlostpointercapture={(event) => {
    if (event.pointerId === gesture?.id) finish(true);
  }}
  onkeydown={keydown}
  ondblclick={reset}
></div>
<span id={helpId} class="resize-help"
  >Drag to resize. Drag further left to collapse, or right to expand. Use Left
  and Right arrow keys to adjust, Enter to collapse or expand, and Home or End
  for collapsed or maximum size. Double-click to reset. Escape cancels a drag.</span
>

<style>
  .resize-handle {
    position: absolute;
    z-index: 2;
    top: 0;
    right: -5px;
    bottom: 0;
    width: 10px;
    cursor: col-resize;
    touch-action: none;
  }
  .resize-handle::after {
    content: "";
    position: absolute;
    top: 0;
    bottom: 0;
    left: 4px;
    width: 2px;
    background: var(--accent);
    opacity: 0;
    transition: opacity 120ms ease;
  }
  .resize-handle:hover::after,
  .resize-handle:focus-visible::after,
  .resize-handle.dragging::after {
    opacity: 0.8;
  }
  .resize-handle:focus-visible {
    outline: none;
  }
  .resize-help {
    position: absolute;
    top: 0;
    left: 0;
    width: 1px;
    height: 1px;
    padding: 0;
    overflow: hidden;
    clip-path: inset(50%);
    white-space: nowrap;
  }
</style>
