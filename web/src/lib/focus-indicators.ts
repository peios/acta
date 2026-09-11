/** Suppress mobile button rings after touch, retaining them for keyboard navigation. */
export function focusIndicators() {
  const root = document.documentElement;
  const pointer = () => {
    root.dataset.keyboardFocus = "false";
  };
  const keyboard = (event: KeyboardEvent) => {
    if (!event.metaKey && !event.ctrlKey && !event.altKey)
      root.dataset.keyboardFocus = "true";
  };
  document.addEventListener("pointerdown", pointer, true);
  document.addEventListener("keydown", keyboard, true);
  return () => {
    document.removeEventListener("pointerdown", pointer, true);
    document.removeEventListener("keydown", keyboard, true);
    delete root.dataset.keyboardFocus;
  };
}
