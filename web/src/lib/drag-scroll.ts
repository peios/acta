// Keeps native touch/trackpad scrolling and adds mouse dragging without turning
// clicks or double-clicks on tabs into accidental selections after a drag.
export function dragScroll(node: HTMLElement) {
  let gesture:
    { id: number; x: number; scroll: number; dragging: boolean } | undefined;
  let suppressClick = false;
  let frame = 0;
  const measure = () => {
    frame = 0;
    const end = Math.max(0, node.scrollWidth - node.clientWidth);
    node.dataset.scrollable = String(end > 1);
    node.style.setProperty(
      "--scroll-fade-start",
      node.scrollLeft > 1 ? "18px" : "0px",
    );
    node.style.setProperty(
      "--scroll-fade-end",
      node.scrollLeft < end - 1 ? "18px" : "0px",
    );
  };
  const schedule = () => {
    if (!frame) frame = requestAnimationFrame(measure);
  };
  const resize = new ResizeObserver(schedule);
  const observe = () => {
    resize.disconnect();
    resize.observe(node);
    for (const child of node.children) resize.observe(child);
    schedule();
  };
  const mutations = new MutationObserver(observe);
  mutations.observe(node, {
    childList: true,
    subtree: true,
    characterData: true,
  });
  observe();

  function down(event: PointerEvent) {
    suppressClick = false;
    if (event.pointerType !== "mouse" || event.button !== 0 || !event.isPrimary)
      return;
    if (node.scrollWidth <= node.clientWidth + 1) return;
    // Editing and action buttons retain their own pointer interactions.
    if (
      event.target instanceof Element &&
      event.target.closest(
        'input, textarea, a, [contenteditable], [role="separator"], [draggable="true"], button:not([role="tab"])',
      )
    )
      return;
    gesture = {
      id: event.pointerId,
      x: event.clientX,
      scroll: node.scrollLeft,
      dragging: false,
    };
  }
  function move(event: PointerEvent) {
    if (!gesture || event.pointerId !== gesture.id) return;
    const delta = event.clientX - gesture.x;
    if (!gesture.dragging && Math.abs(delta) < 5) return;
    if (!gesture.dragging) {
      gesture.dragging = true;
      suppressClick = true;
      node.dataset.dragging = "true";
      node.setPointerCapture(event.pointerId);
    }
    event.preventDefault();
    node.scrollLeft = gesture.scroll - delta;
    schedule();
  }
  function finish(event: PointerEvent) {
    if (!gesture || event.pointerId !== gesture.id) return;
    gesture = undefined;
    delete node.dataset.dragging;
    if (node.hasPointerCapture(event.pointerId))
      node.releasePointerCapture(event.pointerId);
  }
  function click(event: MouseEvent) {
    if (suppressClick && event.detail !== 0) {
      event.preventDefault();
      event.stopImmediatePropagation();
    }
  }
  node.addEventListener("pointerdown", down);
  window.addEventListener("pointermove", move, { passive: false });
  window.addEventListener("pointerup", finish);
  window.addEventListener("pointercancel", finish);
  node.addEventListener("lostpointercapture", finish);
  node.addEventListener("click", click, true);
  node.addEventListener("dblclick", click, true);
  node.addEventListener("scroll", schedule, { passive: true });
  return {
    destroy() {
      if (gesture && node.hasPointerCapture(gesture.id))
        node.releasePointerCapture(gesture.id);
      resize.disconnect();
      mutations.disconnect();
      cancelAnimationFrame(frame);
      node.removeEventListener("pointerdown", down);
      window.removeEventListener("pointermove", move);
      window.removeEventListener("pointerup", finish);
      window.removeEventListener("pointercancel", finish);
      node.removeEventListener("lostpointercapture", finish);
      node.removeEventListener("click", click, true);
      node.removeEventListener("dblclick", click, true);
      node.removeEventListener("scroll", schedule);
    },
  };
}
