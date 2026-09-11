// Reserve the left edge against native history navigation, including overlays.
// Only unobstructed content opens the drawer; leftward drawer gestures close it.
export function swipeNavigation(
  node: HTMLElement,
  drawer: () => HTMLDialogElement | undefined,
) {
  let gesture:
    | {
        id: number;
        x: number;
        y: number;
        closing: boolean;
        claimed: boolean;
        dx: number;
      }
    | undefined;
  function start(event: TouchEvent) {
    gesture = undefined;
    if (
      event.defaultPrevented ||
      window.innerWidth > 720 ||
      event.touches.length !== 1
    )
      return;
    const panel = drawer();
    const target = event.target instanceof Element ? event.target : null;
    if (
      !panel ||
      !target ||
      target.closest("input, textarea, select, [contenteditable]")
    )
      return;
    const t = event.touches[0];
    // iOS can claim history navigation before touchmove. This must happen even
    // when an overlay prevents drawer gestures. Controls remain tappable and
    // scrolling outside the narrow edge stays native.
    if (
      t.clientX <= 28 &&
      event.cancelable &&
      !target.closest("a, button, [role='button'], label")
    )
      event.preventDefault();
    if (
      document.querySelector(":popover-open") ||
      Array.from(document.querySelectorAll("dialog[open]")).some(
        (d) => d !== panel,
      )
    )
      return;
    if (!panel.open && t.clientX > 28) return;
    if (panel.open && !panel.contains(target)) return;
    gesture = {
      id: t.identifier,
      x: t.clientX,
      y: t.clientY,
      closing: panel.open,
      claimed: false,
      dx: 0,
    };
  }
  function move(event: TouchEvent) {
    if (!gesture) return;
    // A held task card (or another child gesture) owns its touch sequence.
    if (event.defaultPrevented) {
      gesture = undefined;
      return;
    }
    if (event.touches.length !== 1) {
      gesture = undefined;
      return;
    }
    const t = Array.from(event.touches).find(
      (t) => t.identifier === gesture!.id,
    );
    if (!t) return;
    const dx = t.clientX - gesture.x,
      dy = t.clientY - gesture.y;
    if (!gesture.claimed) {
      if (Math.abs(dy) > 10 && Math.abs(dy) >= Math.abs(dx)) {
        gesture = undefined;
        return;
      }
      if (Math.abs(dx) < 12) return;
      if (
        (gesture.closing ? dx >= 0 : dx <= 0) ||
        Math.abs(dx) < Math.abs(dy) * 1.5
      ) {
        gesture = undefined;
        return;
      }
      gesture.claimed = true;
    }
    if (!event.cancelable) {
      gesture = undefined;
      return;
    }
    event.preventDefault();
    gesture.dx = dx;
  }
  function end(event: TouchEvent) {
    const g = gesture;
    gesture = undefined;
    if (!g || event.type === "touchcancel" || !g.claimed || Math.abs(g.dx) < 64)
      return;
    if (event.cancelable) event.preventDefault();
    if (g.closing) drawer()?.close();
    else drawer()?.showModal();
  }
  const cancel = () => {
    gesture = undefined;
  };
  node.addEventListener("touchstart", start, { passive: false });
  node.addEventListener("touchmove", move, { passive: false });
  node.addEventListener("touchend", end, { passive: false });
  node.addEventListener("touchcancel", end);
  window.addEventListener("blur", cancel);
  window.addEventListener("resize", cancel);
  return {
    destroy() {
      node.removeEventListener("touchstart", start);
      node.removeEventListener("touchmove", move);
      node.removeEventListener("touchend", end);
      node.removeEventListener("touchcancel", end);
      window.removeEventListener("blur", cancel);
      window.removeEventListener("resize", cancel);
    },
  };
}
