// Frozen pre-migration assembler used only as a behaviour oracle in tests.
import { frameId, runTurnKey, turnKey, uniqueFrames } from "./thread-frames.js";
/** @typedef {import('../../src/lib/threads.svelte').ThreadFrame} ThreadFrame */
/** @typedef {{kind: 'turn-ended', id: string, frame: ThreadFrame, label: string, durationMs: number | null, tokens: {label: string, value: number}[], error: string, diff: string | null}} TurnEnding */
/** @param {unknown} value @returns {Record<string, unknown>} */
function object(value) {
  return value && typeof value === "object" && !Array.isArray(value)
    ? /** @type {Record<string, unknown>} */ (value)
    : {};
}

/** Index completions and associated usage separately so late usage can enrich the
 * original divider without moving it. Cumulative counters are not turn totals.
 * @param {ThreadFrame[]} frames
 * @returns {Map<string, TurnEnding>}
 */
export function turnEndings(frames) {
  /** @type {Map<string, ThreadFrame>} */
  const usage = new Map();
  /** @type {Map<string, ThreadFrame>} */
  const diffs = new Map();
  /** @type {Map<string, ThreadFrame>} */
  const completions = new Map();
  for (const frame of uniqueFrames(frames)) {
    if (typeof frame.data?.turn_id !== "string") continue;
    if (frame.kind === "turn/diff" && typeof frame.data.diff === "string")
      diffs.set(runTurnKey(frame), frame);
    if (frame.kind === "usage/context") usage.set(runTurnKey(frame), frame);
    if (
      frame.kind === "turn/completed" &&
      ["completed", "failed", "interrupted"].includes(
        String(frame.data.outcome),
      ) &&
      !completions.has(turnKey(frame))
    ) {
      completions.set(turnKey(frame), frame);
    }
  }
  /** @type {Map<string, TurnEnding>} */
  const result = new Map();
  for (const frame of completions.values()) {
    const data = frame.data ?? {};
    if (!["completed", "failed", "interrupted"].includes(String(data.outcome)))
      continue;
    const call = object(usage.get(runTurnKey(frame))?.data?.last_request);
    const tokenFields = [
      ["total_tokens", "Total tokens"],
      ["input_tokens", "Input tokens"],
      ["cached_input_tokens", "Cached input"],
      ["cache_write_input_tokens", "Cache writes"],
      ["output_tokens", "Output tokens"],
      ["reasoning_output_tokens", "Reasoning tokens"],
    ];
    const tokens = tokenFields.flatMap(([key, label]) => {
      const value = call[key];
      return typeof value === "number" && Number.isFinite(value) && value >= 0
        ? [{ label, value }]
        : [];
    });
    const error = object(data.error);
    const id = frameId(frame);
    result.set(id, {
      kind: "turn-ended",
      id,
      frame,
      label:
        data.outcome === "failed"
          ? "Turn failed"
          : data.outcome === "interrupted"
            ? "Turn interrupted"
            : "Turn ended",
      durationMs:
        typeof data.duration_ms === "number" &&
        Number.isFinite(data.duration_ms) &&
        data.duration_ms >= 0
          ? data.duration_ms
          : null,
      tokens,
      diff:
        typeof diffs.get(runTurnKey(frame))?.data?.diff === "string"
          ? String(diffs.get(runTurnKey(frame))?.data?.diff)
          : null,
      error:
        typeof error.message === "string"
          ? error.message
          : data.error
            ? JSON.stringify(data.error, null, 2)
            : "",
    });
  }
  return result;
}
