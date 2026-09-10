import { popoverPosition } from "./popover-position.js";

type Options = {
  anchor: HTMLElement | undefined;
  width: number;
  height: number;
  align?: "start" | "end";
  margin?: number;
  gap?: number;
};

/** Shared placement for native popovers, including nested menus and mobile keyboards. */
export function anchoredPopover(node: HTMLElement, options: () => Options) {
  let opened = false;
  let frame = 0;
  function position() {
    const { anchor, ...settings } = options();
    if (!anchor || !opened) return;
    // Measure against the requested limit, not the previous content height.
    // Search results may grow after an initial loading or empty state.
    node.style.maxHeight = `${settings.height}px`;
    if (node.matches(":popover-open") && node.scrollHeight) {
      settings.height = Math.min(
        settings.height,
        node.scrollHeight + node.offsetHeight - node.clientHeight,
      );
    }
    const viewport = window.visualViewport;
    const box = popoverPosition(
      anchor.getBoundingClientRect(),
      {
        width: viewport?.width ?? window.innerWidth,
        height: viewport?.height ?? window.innerHeight,
        left: viewport?.offsetLeft ?? 0,
        top: viewport?.offsetTop ?? 0,
      },
      settings,
    );
    node.style.left = `${box.left}px`;
    node.style.top = `${box.top}px`;
    node.style.bottom = "auto";
    node.style.right = "auto";
    node.style.width = `${box.width}px`;
    node.style.maxHeight = `${box.maxHeight}px`;
  }
  function schedule(event?: Event) {
    if (
      !opened ||
      (event?.target instanceof Node && node.contains(event.target))
    )
      return;
    cancelAnimationFrame(frame);
    frame = requestAnimationFrame(position);
  }
  function toggle(event: Event) {
    opened = (event as ToggleEvent).newState === "open";
    if (opened) {
      position();
      schedule();
    }
  }
  const observer = new MutationObserver(() => schedule());
  observer.observe(node, {
    childList: true,
    subtree: true,
    characterData: true,
  });
  node.addEventListener("beforetoggle", toggle);
  document.addEventListener("scroll", schedule, true);
  window.addEventListener("resize", schedule);
  window.visualViewport?.addEventListener("resize", schedule);
  window.visualViewport?.addEventListener("scroll", schedule);
  return {
    update(next: () => Options) {
      options = next;
      schedule();
    },
    destroy() {
      cancelAnimationFrame(frame);
      observer.disconnect();
      node.removeEventListener("beforetoggle", toggle);
      document.removeEventListener("scroll", schedule, true);
      window.removeEventListener("resize", schedule);
      window.visualViewport?.removeEventListener("resize", schedule);
      window.visualViewport?.removeEventListener("scroll", schedule);
    },
  };
}
