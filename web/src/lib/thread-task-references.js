/** Recognise structured task references only. Names and references are hints;
 * UUIDs must still be authorised by the local task API before becoming links. */
import { referenceUUID as taskUUID } from "./thread-reference-resolver.js";
export { referenceUUID as taskUUID } from "./thread-reference-resolver.js";
/** @param {unknown} value */
const object = (value) =>
  value && typeof value === "object" && !Array.isArray(value)
    ? /** @type {Record<string,unknown>} */ (value)
    : null;
/** @typedef {{id:string, reference:string, title:string}} TaskReference */
/** @param {import('./thread-tool-calls.js').ToolCallItem} call @returns {TaskReference[]} */
export function taskReferences(call) {
  const name = typeof call.data.name === "string" ? call.data.name : "";
  const server = name.startsWith("mcp__")
    ? name.split("__")[1]
    : name.includes("/")
      ? name.split("/")[0]
      : "";
  if (!server.toLowerCase().includes("acta")) return [];
  /** @type {Map<string,TaskReference>} */
  const found = new Map();
  /** @param {unknown} id @param {Record<string,unknown>|null} [value] */
  function add(id, value = null) {
    if (!taskUUID(id) || found.size >= 128) return;
    const key = String(id).toLowerCase();
    const previous = found.get(key);
    found.set(key, {
      id: key,
      reference:
        typeof value?.reference === "string"
          ? value.reference
          : previous?.reference || "",
      title:
        typeof value?.title === "string" ? value.title : previous?.title || "",
    });
  }
  const args = object(call.data.arguments);
  for (const key of ["task", "task_id", "parent", "parent_id"])
    add(args?.[key]);
  if (args?.field === "parent_id") add(args.value);
  let visits = 0;
  /** @param {unknown} value @param {number} depth */
  function visit(value, depth) {
    if (depth > 10 || ++visits > 2000) return;
    if (Array.isArray(value)) {
      for (const child of value) visit(child, depth + 1);
      return;
    }
    const row = object(value);
    if (!row) return;
    add(row.task_id);
    // Tasks and ancestor summaries have this shape; an arbitrary `id` does not.
    if (typeof row.reference === "string" && typeof row.title === "string")
      add(row.id, row);
    for (const key of [
      "data",
      "result",
      "task",
      "tasks",
      "entries",
      "items",
      "subtasks",
      "ancestors",
      "sources",
      "structuredContent",
    ])
      if (row[key] !== undefined) visit(row[key], depth + 1);
    // MCP text content is a JSON envelope, never search prose for IDs.
    if (row.type === "text" && typeof row.text === "string")
      parse(row.text, depth + 1);
    if (Array.isArray(row.content)) visit(row.content, depth + 1);
  }
  /** @param {string} text @param {number} depth */
  function parse(text, depth) {
    try {
      visit(JSON.parse(text), depth);
    } catch {
      /* Plain output is not a task reference. */
    }
  }
  parse(call.output, 0);
  // The Codex adapter appends structuredContent when it differs from text output.
  const marker = "\n\nStructured result:\n";
  const index = call.output.lastIndexOf(marker);
  if (index >= 0) parse(call.output.slice(index + marker.length), 0);
  return [...found.values()];
}

export { ReferenceResolver as TaskReferenceResolver } from "./thread-reference-resolver.js";
