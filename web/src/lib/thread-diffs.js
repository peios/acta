/** @typedef {{path:string, kind:'add'|'update'|'delete', move_path:string|null, diff:string, format:'unified'|'added_content'|'removed_content'}} FileChange */
/** @param {unknown} value @returns {FileChange[]} */
export function fileChanges(value) {
  if (!Array.isArray(value)) return [];
  return value.filter(
    (c) =>
      c &&
      typeof c.path === "string" &&
      typeof c.diff === "string" &&
      ["add", "update", "delete"].includes(c.kind) &&
      ["unified", "added_content", "removed_content"].includes(c.format),
  );
}
/** Preserve provider text verbatim; decoration never attempts to apply a patch.
 * @param {string} text @param {string} format
 */
export function diffLines(text, format = "unified") {
  return (text.match(/[^\n]*\n|[^\n]+$/g) ?? []).map((text) => ({
    text,
    kind:
      format === "added_content"
        ? "addition"
        : format === "removed_content"
          ? "deletion"
          : /^(diff --git |index |--- |\+\+\+ |new file |deleted file |rename |similarity )/.test(
                text,
              )
            ? "header"
            : text.startsWith("@@")
              ? "hunk"
              : text.startsWith("+")
                ? "addition"
                : text.startsWith("-")
                  ? "deletion"
                  : "context",
  }));
}
