// Only connection flows can resume after sign-in. Never accept an arbitrary
// redirect from a query parameter or browser storage.
export function authReturn(value: string | null, fallback = "/"): string {
  if (!value?.startsWith("/")) return fallback;
  try {
    const url = new URL(value, "https://acta.invalid");
    if (
      url.origin === "https://acta.invalid" &&
      ["/login/device", "/login/oauth"].includes(url.pathname)
    )
      return url.pathname + url.search;
  } catch {
    /* Invalid return paths use the ordinary landing page. */
  }
  return fallback;
}
export function rememberAuthReturn(value: string) {
  try {
    sessionStorage.setItem("acta.auth-return", authReturn(value));
  } catch {}
}
export function consumeAuthReturn(fallback: string): string {
  try {
    const next = sessionStorage.getItem("acta.auth-return");
    sessionStorage.removeItem("acta.auth-return");
    return authReturn(next, fallback);
  } catch {
    return fallback;
  }
}
