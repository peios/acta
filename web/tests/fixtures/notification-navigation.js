/** @param {string | URL} url */
export async function goto(url) {
  /** @type {any} */ (window).reviewURL = String(url);
}
