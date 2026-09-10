// Frozen pre-migration assembler used only as a behaviour oracle in tests.
import {
  frameId,
  runItemKey,
  sameRunTurn,
  uniqueFrames,
} from "./thread-frames.js";
/** @typedef {import('../../src/lib/threads.svelte').ThreadFrame} ThreadFrame */
/** @typedef {{kind:'tool-call', id:string, frame:ThreadFrame, toolId:string, turn:string, data:Record<string,unknown>, argumentsText:string, output:string, status:string, interruptedByTurn:boolean}} ToolCallItem */
/** @param {string} status */
export const terminalTool = (status) =>
  [
    "completed",
    "failed",
    "interrupted",
    "declined",
    "permission_denied",
  ].includes(status);
/** @param {ThreadFrame[]} frames @returns {Map<string,ToolCallItem>} */
export function toolCallItems(frames) {
  /** @type {Map<string,ToolCallItem>} */
  const items = new Map();
  for (const frame of uniqueFrames(frames)) {
    const id = frameId(frame);
    const d = frame.data ?? {};
    if (frame.kind === "turn/completed") {
      for (const item of items.values()) {
        if (
          sameRunTurn(item.frame, frame, item.turn) &&
          !terminalTool(item.status)
        ) {
          item.status = "interrupted";
          item.interruptedByTurn = true;
        }
      }
    }
    if (
      !["tool/call", "tool/arguments/delta", "tool/output/delta"].includes(
        frame.kind,
      ) ||
      typeof d.tool_id !== "string" ||
      typeof d.turn_id !== "string"
    )
      continue;
    const key = runItemKey(frame, d.tool_id);
    let item = items.get(key);
    if (!item) {
      item = {
        kind: "tool-call",
        id,
        frame,
        toolId: d.tool_id,
        turn: d.turn_id,
        data: {},
        argumentsText: "",
        output: "",
        status: "pending",
        interruptedByTurn: false,
      };
      items.set(key, item);
    }
    if (
      terminalTool(item.status) &&
      !(
        (item.interruptedByTurn ||
          (d.status === "permission_denied" &&
            ["failed", "permission_denied"].includes(item.status))) &&
        frame.kind === "tool/call" &&
        terminalTool(String(d.status))
      )
    )
      continue;
    if (frame.kind === "tool/call") {
      item.data = d;
      item.interruptedByTurn = false;
      item.status = String(d.status);
      item.argumentsText = d.arguments
        ? JSON.stringify(d.arguments, null, 2)
        : "";
      if (typeof d.output === "string") item.output = d.output;
    } else if (typeof d.text === "string") {
      if (frame.kind === "tool/output/delta") item.output += d.text;
      else if (item.status === "preparing" || !Object.keys(item.data).length) {
        // Empty input objects on a start frame are placeholders, not fragments.
        if (item.argumentsText === "{}") item.argumentsText = "";
        item.argumentsText += d.text;
      }
    }
  }
  return new Map([...items.values()].map((item) => [item.id, item]));
}
