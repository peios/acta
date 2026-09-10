// Frozen pre-migration assembler used only as a behaviour oracle in tests.
import {
  frameId,
  runItemKey,
  sameRunTurn,
  uniqueFrames,
} from "./thread-frames.js";
/** @typedef {import('../../src/lib/threads.svelte').ThreadFrame} Frame */
/** @typedef {{kind:'approval-review',id:string,frame:Frame,data:Record<string,any>,interrupted:boolean}} ApprovalReviewItem */
/** @param {Frame[]} frames @returns {Map<string,ApprovalReviewItem>} */
export function approvalReviewItems(frames) {
  /** @type {Map<string,ApprovalReviewItem>} */
  const items = new Map();
  for (const frame of uniqueFrames(frames)) {
    const d = frame.data ?? {};
    if (frame.kind === "turn/completed") {
      for (const item of items.values())
        if (
          item.data.status === "in_progress" &&
          sameRunTurn(item.frame, frame, item.data.turn_id)
        )
          item.interrupted = true;
    }
    if (frame.kind !== "approval/review" || typeof d.review_id !== "string")
      continue;
    const key = runItemKey(frame, d.review_id);
    let item = items.get(key);
    if (!item) {
      item = {
        kind: "approval-review",
        id: frameId(frame),
        frame,
        data: d,
        interrupted: false,
      };
      items.set(key, item);
    } else if (
      d.status !== "in_progress" ||
      (item.data.status === "in_progress" && !item.interrupted)
    ) {
      item.data = d;
      item.interrupted = false;
    }
  }
  return new Map([...items.values()].map((item) => [item.id, item]));
}
