// The visual viewport excludes the software keyboard. Keep the app and dialogs
// inside it without scrolling the document or interfering with pinch-to-zoom.
export function mobileViewport(_node: HTMLElement) {
  const viewport = window.visualViewport;
  const root = document.documentElement;
  let frame = 0;
  function update() {
    if (window.innerWidth >= 760 || !viewport) {
      root.style.removeProperty("--mobile-viewport-height");
      root.style.removeProperty("--mobile-viewport-top");
      return;
    }
    if (Math.abs(viewport.scale - 1) > 0.02) return;
    root.style.setProperty("--mobile-viewport-height", `${viewport.height}px`);
    root.style.setProperty("--mobile-viewport-top", `${viewport.offsetTop}px`);
  }
  function schedule() {
    cancelAnimationFrame(frame);
    frame = requestAnimationFrame(update);
  }
  update();
  viewport?.addEventListener("resize", schedule);
  viewport?.addEventListener("scroll", schedule);
  window.addEventListener("resize", schedule);
  return {
    destroy() {
      cancelAnimationFrame(frame);
      viewport?.removeEventListener("resize", schedule);
      viewport?.removeEventListener("scroll", schedule);
      window.removeEventListener("resize", schedule);
      root.style.removeProperty("--mobile-viewport-height");
      root.style.removeProperty("--mobile-viewport-top");
    },
  };
}
