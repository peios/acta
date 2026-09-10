/** Refresh every already-loaded activity entry without losing insertions at page
 * boundaries. Entry ordering uses immutable first-event positions; extensions
 * keep their entry identity. All positions stay strings to avoid precision loss.
 * @param {(cursor:string)=>Promise<import('./activity').ActivityPage>} request
 * @param {{oldest?:string,cursor?:string}} options
 */
export async function loadActivityWindow(
  request,
  { oldest, cursor = "" } = {},
) {
  /** @type {import('./activity').ActivityEntry[]} */
  const entries = [];
  const visited = new Set();
  let next = cursor;
  for (;;) {
    if (visited.has(next))
      throw new Error("Acta repeated an activity cursor. Please retry.");
    visited.add(next);
    const page = await request(next);
    entries.push(...page.entries);
    const last = entries.at(-1);
    if (
      !oldest ||
      !page.more ||
      (last && BigInt(last.first_event) <= BigInt(oldest))
    )
      return { ...page, entries };
    next = page.cursor;
    if (!next)
      throw new Error(
        "Acta returned an incomplete activity page. Please retry.",
      );
  }
}
