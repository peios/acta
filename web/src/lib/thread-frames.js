import { groupThinking } from "./thread-thinking.js";
/** Common identity rules for persisted thread frames. Capture identity is stable
 * across provider restarts; logical run items additionally belong to a run.
 * @typedef {import('./threads.svelte').ThreadFrame} ThreadFrame
 */
/** @param {ThreadFrame} frame */
export const frameId = (frame) =>
  `${frame.thread_id}:${frame.sequence}:${frame.output_index}`;
/** @param {ThreadFrame} frame @param {unknown} id */
export const runItemKey = (frame, id) =>
  JSON.stringify([frame.thread_id, frame.run_id, id]);
/** @param {ThreadFrame} frame */
export const runTurnKey = (frame) => runItemKey(frame, frame.data?.turn_id);
/** @param {ThreadFrame} frame */
export const turnKey = (frame) =>
  JSON.stringify([frame.thread_id, frame.data?.turn_id]);
/** @param {ThreadFrame} a @param {ThreadFrame} b @param {unknown} turn */
export const sameRunTurn = (a, b, turn) =>
  a.thread_id === b.thread_id &&
  a.run_id === b.run_id &&
  turn === b.data?.turn_id;
/** @param {ThreadFrame[]} frames */
export function uniqueFrames(frames) {
  const seen = new Set();
  return frames.filter((f) => {
    const id = frameId(f);
    if (seen.has(id)) return false;
    seen.add(id);
    return true;
  });
}
const hiddenStateFrames = new Set([
  "approval/resolved",
  "thread/configuration",
  "thread/status",
  "turn/started",
  "usage/context",
  "usage/account",
]);
/** Unknown frames are always visible. State-only frames feed controls, not cards.
 * @param {import('./thread-feed.js').FeedItem[]} feed @param {boolean} showDebug
 */
export const visibleFeed = (feed, showDebug) =>
  groupThinking(
    feed.filter(
      (item) =>
        item.kind !== "frame" ||
        (!hiddenStateFrames.has(item.frame.kind) &&
          (showDebug ||
            item.frame.kind === "debug/unknown" ||
            !item.frame.kind.startsWith("debug/"))),
    ),
  );
