/** A latest-request boundary: replaced or disposed reads cannot update the UI. */
export class LatestRequest {
  /** @type {AbortController | null} */
  controller = null;
  disposed = false;
  begin() {
    this.controller?.abort();
    const controller = new AbortController();
    this.controller = controller;
    if (this.disposed) controller.abort();
    return {
      signal: controller.signal,
      current: () =>
        !controller.signal.aborted && this.controller === controller,
    };
  }
  dispose() {
    this.disposed = true;
    this.controller?.abort();
  }
}

/** @param {number} milliseconds @param {AbortSignal} signal @returns {Promise<void>} */
export function abortableDelay(milliseconds, signal) {
  return new Promise((resolve) => {
    if (signal.aborted) {
      resolve();
      return;
    }
    const finish = () => {
      clearTimeout(timer);
      signal.removeEventListener("abort", finish);
      resolve();
    };
    const timer = setTimeout(finish, milliseconds);
    signal.addEventListener("abort", finish, { once: true });
  });
}
