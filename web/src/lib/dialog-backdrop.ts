// Close on the completed click, not pointer-down: the modal must keep the page
// inert until the finger/mouse is released, or its click can reach content below.
export function dismissDialogBackdrop(
  event: MouseEvent,
  dialog: HTMLDialogElement,
  close: () => void,
  content: Element = dialog,
) {
  if (event.target !== dialog) return;
  // Drawers can use a full-screen transparent dialog around a narrower panel.
  const box = content.getBoundingClientRect();
  if (
    event.clientX >= box.left &&
    event.clientX <= box.right &&
    event.clientY >= box.top &&
    event.clientY <= box.bottom
  )
    return;
  event.preventDefault();
  event.stopPropagation();
  close();
}
