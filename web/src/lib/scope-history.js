const roots = { agents: "/my-agents", workspace: "/workspaces" };
/** @typedef {keyof typeof roots} RememberedScope */
/** Only in-app paths belonging to the requested scope can be restored.
 * @param {unknown} value @param {RememberedScope} scope
 */
function validPath(value, scope) {
  if (
    typeof value !== "string" ||
    !value.startsWith("/") ||
    value.startsWith("//") ||
    value.includes("\\") ||
    /[\u0000-\u001f\u007f]/.test(value)
  )
    return false;
  const url = new URL(value, "https://acta.invalid");
  return (
    url.origin === "https://acta.invalid" &&
    (url.pathname === roots[scope] ||
      url.pathname.startsWith(roots[scope] + "/"))
  );
}
export class ScopeHistory {
  /** @param {()=>Storage} storage */
  constructor(storage) {
    this.storage = storage;
    this.paths = new Map();
  }
  /** @param {string} account @param {string} path */
  remember(account, path) {
    for (const scope of /** @type {RememberedScope[]} */ (Object.keys(roots))) {
      if (!validPath(path, scope)) continue;
      const key = `acta.scope-location:${account}:${scope}`;
      this.paths.set(key, path);
      try {
        this.storage().setItem(key, path);
      } catch {
        /* Navigation still works without browser storage. */
      }
    }
  }
  /** @param {string} account @param {RememberedScope} scope */
  destination(account, scope) {
    const key = `acta.scope-location:${account}:${scope}`;
    let saved = this.paths.get(key);
    try {
      saved = this.storage().getItem(key) ?? saved;
    } catch {
      /* Use session memory. */
    }
    return validPath(saved, scope) ? saved : roots[scope];
  }
}
