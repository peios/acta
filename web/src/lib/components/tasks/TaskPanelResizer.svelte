<script lang="ts">
  import { onMount } from "svelte";

  let {
    label = "task",
    width,
    minimum,
    maximum,
    defaultWidth,
    controls,
    onchange,
    preview = $bindable<number | null>(null),
  }: {
    label?: string;
    width: number;
    preview?: number | null;
    minimum: number;
    maximum: number;
    defaultWidth: number;
    controls: string;
    onchange: (width: number) => void;
  } = $props();
  const helpID = $props.id();
  let handle: HTMLDivElement;
  let dragging = $state(false);
  let gesture: {
    id: number;
    x: number;
    width: number;
    candidate: number;
  } | null = null;
  let previousCursor = "",
    previousSelection = "";
  const clamp = (value: number) =>
    Math.round(Math.max(minimum, Math.min(maximum, value)));

  function start(event: PointerEvent) {
    if (event.button !== 0 || !event.isPrimary || gesture) return;
    event.preventDefault();
    handle.focus();
    gesture = {
      id: event.pointerId,
      x: event.clientX,
      width,
      candidate: width,
    };
    handle.setPointerCapture(event.pointerId);
    previousCursor = document.body.style.cursor;
    previousSelection = document.body.style.userSelect;
    document.body.style.cursor = "col-resize";
    document.body.style.userSelect = "none";
    dragging = true;
  }
  function move(event: PointerEvent) {
    if (!gesture || event.pointerId !== gesture.id) return;
    gesture.candidate = clamp(gesture.width + gesture.x - event.clientX);
    // The task panel is anchored on the right: moving left makes it wider.
    preview = gesture.candidate;
  }
  function finish(cancel = false) {
    if (!gesture) return;
    const previous = gesture;
    gesture = null;
    if (!cancel) onchange(previous.candidate);
    preview = null;
    document.body.style.cursor = previousCursor;
    document.body.style.userSelect = previousSelection;
    dragging = false;
    if (handle?.hasPointerCapture(previous.id))
      handle.releasePointerCapture(previous.id);
  }
  function keydown(event: KeyboardEvent) {
    if (event.key === "Escape" && gesture) {
      event.preventDefault();
      event.stopPropagation();
      finish(true);
      return;
    }
    if (gesture) return;
    const step = event.shiftKey ? 32 : 16;
    let next: number;
    switch (event.key) {
      case "ArrowLeft":
        next = width + step;
        break;
      case "ArrowRight":
        next = width - step;
        break;
      case "Home":
        next = minimum;
        break;
      case "End":
        next = maximum;
        break;
      default:
        return;
    }
    event.preventDefault();
    onchange(clamp(next));
  }
  onMount(() => () => finish(true));
</script>

<svelte:window onresize={() => finish(true)} />
<!-- svelte-ignore a11y_no_noninteractive_tabindex, a11y_no_noninteractive_element_interactions (Focusable separator implements the window-splitter pattern.) -->
<div
  bind:this={handle}
  class="resize-handle"
  class:dragging
  role="separator"
  tabindex="0"
  aria-label={`Resize ${label} panel`}
  aria-orientation="vertical"
  aria-controls={controls}
  aria-valuemin={minimum}
  aria-valuemax={maximum}
  aria-valuenow={width}
  aria-valuetext={`${width} pixels`}
  aria-describedby={helpID}
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
  ondblclick={() => onchange(clamp(defaultWidth))}
></div>
<span id={helpID} class="resize-help"
  >Drag left to widen the {label} panel or right to narrow it. Use Left and Right
  arrow keys to adjust, and Home or End for minimum or maximum width. Double-click
  to reset. Escape cancels a drag.</span
>

<style>
  .resize-handle {
    position: absolute;
    z-index: 8;
    top: 16px;
    bottom: 16px;
    left: -5px;
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
    width: 1px;
    height: 1px;
    overflow: hidden;
    clip-path: inset(50%);
    white-space: nowrap;
  }
  @media (prefers-reduced-motion: reduce) {
    .resize-handle::after {
      transition: none;
    }
  }
</style>
