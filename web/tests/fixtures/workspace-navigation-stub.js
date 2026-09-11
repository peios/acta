/** @param {string} url */
export async function goto(url) {
  window.location.href = url;
}

export function beforeNavigate() {}
