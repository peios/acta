export type TouchDragPoint = { x: number; y: number };
export type TouchDragOptions = {
  enabled: () => boolean;
  start: (point: TouchDragPoint) => boolean;
  move: (point: TouchDragPoint) => void;
  finish: (cancelled: boolean) => void;
};

// Delay whole-card dragging so normal taps and scrolling remain native.
export function touchDrag(node: HTMLElement, options: TouchDragOptions) {
  let touchID: number | undefined;
  let origin: TouchDragPoint;
  let timer: ReturnType<typeof setTimeout> | undefined;
  let active = false;
  let suppressClickUntil = 0;
  function finish(cancelled: boolean) {
    clearTimeout(timer);
    timer = undefined;
    touchID = undefined;
    if (!active) return;
    active = false;
    delete node.dataset.touchDragging;
    suppressClickUntil = Date.now() + 500;
    options.finish(cancelled);
  }
  function begin() {
    timer = undefined;
    if (touchID === undefined || !options.enabled()) return finish(true);
    active = options.start(origin);
    if (active) node.dataset.touchDragging = "true";
  }
  function start(event: TouchEvent) {
    finish(true);
    if (
      event.touches.length !== 1 ||
      !options.enabled() ||
      (event.target as Element).closest("[data-no-drag]")
    )
      return;
    const touch = event.changedTouches[0];
    touchID = touch.identifier;
    origin = { x: touch.clientX, y: touch.clientY };
    timer = setTimeout(begin, 350);
  }
  function move(event: TouchEvent) {
    if (touchID === undefined) return;
    if (event.touches.length !== 1) return finish(true);
    const touch = Array.from(event.touches).find(
      (t) => t.identifier === touchID,
    );
    if (!touch) return;
    const point = { x: touch.clientX, y: touch.clientY };
    if (!active) {
      if (Math.hypot(point.x - origin.x, point.y - origin.y) > 8) finish(true);
      return;
    }
    // If the browser has already taken this gesture, never accidentally drop.
    if (!event.cancelable) return finish(true);
    event.preventDefault();
    options.move(point);
  }
  function end(event: TouchEvent) {
    if (!Array.from(event.changedTouches).some((t) => t.identifier === touchID))
      return;
    if (active && event.cancelable) event.preventDefault();
    finish(event.type === "touchcancel");
  }
  function click(event: MouseEvent) {
    if (event.detail && Date.now() < suppressClickUntil) {
      event.preventDefault();
      event.stopImmediatePropagation();
    }
  }
  function context(event: Event) {
    if (touchID !== undefined) event.preventDefault();
  }
  const cancel = () => finish(true);
  const additionalTouch = (event: TouchEvent) => {
    if (event.touches.length > 1) finish(true);
  };
  const key = (event: KeyboardEvent) => {
    if (event.key === "Escape") finish(true);
  };
  node.addEventListener("touchstart", start, { passive: false });
  node.addEventListener("touchmove", move, { passive: false });
  node.addEventListener("touchend", end, { passive: false });
  node.addEventListener("touchcancel", end);
  node.addEventListener("click", click, true);
  node.addEventListener("contextmenu", context);
  window.addEventListener("blur", cancel);
  window.addEventListener("resize", cancel);
  window.addEventListener("keydown", key);
  window.addEventListener("touchstart", additionalTouch, { passive: true });
  document.addEventListener("visibilitychange", cancel);
  return {
    destroy() {
      finish(true);
      node.removeEventListener("touchstart", start);
      node.removeEventListener("touchmove", move);
      node.removeEventListener("touchend", end);
      node.removeEventListener("touchcancel", end);
      node.removeEventListener("click", click, true);
      node.removeEventListener("contextmenu", context);
      window.removeEventListener("blur", cancel);
      window.removeEventListener("resize", cancel);
      window.removeEventListener("keydown", key);
      window.removeEventListener("touchstart", additionalTouch);
      document.removeEventListener("visibilitychange", cancel);
    },
  };
}
