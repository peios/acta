// Frozen pre-migration assembler used only as a behaviour oracle in tests.
import {
  frameId,
  runItemKey,
  sameRunTurn,
  uniqueFrames,
} from "./thread-frames.js";
/** @typedef {import('../../src/lib/threads.svelte').ThreadFrame} ThreadFrame */
/** @typedef {{kind: 'thinking', id: string, frame: ThreadFrame, turn: string, text: string, tokens: number | null, started: string | null, ended: string | null, completed: boolean, interrupted: boolean}} ThinkingItem */

/** Build stable thinking items from snapshot updates, independent of debug visibility.
 * @param {ThreadFrame[]} frames
 * @returns {Map<string, ThinkingItem>}
 */
export function thinkingItems(frames) {
  /** @type {Map<string, ThinkingItem>} */
  const items = new Map();
  /** @type {Map<string, {summary: Map<number,string>, content: Map<number,string>}>} */
  const parts = new Map();
  for (const frame of uniqueFrames(frames)) {
    const id = frameId(frame);
    const data = frame.data ?? {};
    if (frame.kind === "turn/completed") {
      for (const item of items.values()) {
        if (!item.completed && sameRunTurn(item.frame, frame, item.turn)) {
          item.interrupted = true;
          item.ended =
            typeof data.completed_at === "string"
              ? data.completed_at
              : frame.received_at;
        }
      }
    }
    if (
      !["thinking/started", "thinking/delta", "thinking/completed"].includes(
        frame.kind,
      )
    )
      continue;
    if (
      typeof data.thinking_id !== "string" ||
      typeof data.turn_id !== "string" ||
      typeof data.text !== "string"
    )
      continue;
    const key = runItemKey(frame, data.thinking_id);
    let item = items.get(key);
    if (!item) {
      item = {
        kind: "thinking",
        id,
        frame,
        turn: data.turn_id,
        text: "",
        tokens: null,
        started: typeof data.started_at === "string" ? data.started_at : null,
        ended: null,
        completed: false,
        interrupted: false,
      };
      items.set(key, item);
      parts.set(key, { summary: new Map(), content: new Map() });
    }
    const complete = frame.kind === "thinking/completed";
    if ((item.completed || item.interrupted) && !complete) continue;
    if (frame.kind === "thinking/delta") {
      const sections = parts.get(key);
      if (
        sections &&
        (data.channel === "summary" || data.channel === "content") &&
        typeof data.section_index === "number"
      ) {
        const target = sections[data.channel];
        target.set(
          data.section_index,
          (target.get(data.section_index) ?? "") + data.text,
        );
        /** @param {Map<number,string>} map */
        const text = (map) =>
          [...map.entries()]
            .sort(([a], [b]) => a - b)
            .map(([, v]) => v)
            .join("\n\n");
        item.text = text(sections.summary) || text(sections.content);
      }
    } else {
      item.text = data.text;
      if (!complete) parts.get(key)?.content.set(0, data.text);
    }
    item.tokens =
      typeof data.estimated_tokens === "number" ? data.estimated_tokens : null;
    item.started =
      typeof data.started_at === "string" ? data.started_at : item.started;
    if (complete) {
      item.completed = true;
      item.interrupted = false;
      item.ended =
        typeof data.completed_at === "string"
          ? data.completed_at
          : frame.received_at;
    }
  }
  return new Map([...items.values()].map((item) => [item.id, item]));
}
