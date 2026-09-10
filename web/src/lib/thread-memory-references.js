import { taskUUID } from "./thread-task-references.js";
/** @typedef {{id:string,key:string,scope:string,summary:string}} MemoryReference */
const object = (/** @type {unknown} */ value) =>
  value && typeof value === "object" && !Array.isArray(value)
    ? /** @type {Record<string,unknown>} */ (value)
    : null;
/** Only reviewed memory operations and structured UUIDs; never infer a link from prose or a scope ID.
 * @param {import('./thread-tool-calls.js').ToolCallItem} call
 * @returns {MemoryReference[]} */
export function memoryReferences(call) {
  const name = typeof call.data.name === "string" ? call.data.name : "";
  const parts = name.startsWith("mcp__")
    ? name.slice(5).split("__")
    : name.split("/");
  if (
    parts.length !== 2 ||
    !parts[0].toLowerCase().includes("acta") ||
    !["memory_get", "memory_save", "memory_recall", "memory_delete"].includes(
      parts[1],
    )
  )
    return [];
  /** @type {Map<string,MemoryReference>} */
  const found = new Map();
  function add(
    /** @type {unknown} */ id,
    /** @type {Record<string,unknown>|null} */ row = null,
  ) {
    if (!taskUUID(id) || found.size >= 128) return;
    const key = String(id).toLowerCase(),
      old = found.get(key);
    found.set(key, {
      id: key,
      key: typeof row?.key === "string" ? row.key : old?.key || "",
      scope: typeof row?.scope === "string" ? row.scope : old?.scope || "",
      summary:
        typeof row?.summary === "string" ? row.summary : old?.summary || "",
    });
  }
  const args = object(call.data.arguments);
  if (parts[1] !== "memory_recall") add(args?.id);
  let visits = 0;
  function visit(/** @type {unknown} */ value, /** @type {number} */ depth) {
    if (depth > 10 || ++visits > 2000) return;
    if (Array.isArray(value)) {
      for (const child of value) visit(child, depth + 1);
      return;
    }
    const row = object(value);
    if (!row) return;
    if (
      typeof row.key === "string" &&
      typeof row.summary === "string" &&
      ["site", "workspace", "user", "agent"].includes(String(row.scope))
    )
      add(row.id, row);
    for (const key of [
      "data",
      "result",
      "memory",
      "memories",
      "entries",
      "items",
      "structuredContent",
    ])
      if (row[key] !== undefined) visit(row[key], depth + 1);
    if (row.type === "text" && typeof row.text === "string")
      parse(row.text, depth + 1);
    if (Array.isArray(row.content)) visit(row.content, depth + 1);
  }
  function parse(/** @type {string} */ text, /** @type {number} */ depth) {
    try {
      visit(JSON.parse(text), depth);
    } catch {
      /* Non-JSON output is not a reference. */
    }
  }
  parse(call.output, 0);
  const marker = "\n\nStructured result:\n",
    index = call.output.lastIndexOf(marker);
  if (index >= 0) parse(call.output.slice(index + marker.length), 0);
  return [...found.values()];
}
