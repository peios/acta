/** @typedef {import('./thread-approval-reviews.js').ApprovalReviewItem} ApprovalReviewItem */
/** @typedef {import("./thread-tool-calls.js").ToolCallItem} ToolCallItem */
/** @typedef {import("./thread-thinking.js").ThinkingItem} ThinkingItem */
/** @typedef {import('./threads.svelte').ThreadFrame} ThreadFrame */
/** @typedef {import('./thread-turns.js').TurnEnding} TurnEnding */
/** @typedef {{name: string, ready: boolean}} ToolServer */
/** @typedef {{kind: 'tools', id: string, frame: ThreadFrame, servers: ToolServer[], stacked: boolean, sealed: boolean}} ToolBatch */
/** @typedef {{kind: 'tool-error', id: string, frame: ThreadFrame, name: string, cancelled: boolean, message: string, reason: string}} ToolError */
/** @typedef {{kind: 'user-message', id: string, frame: ThreadFrame, text: string, images?: unknown[], draftImages?:import("./thread-image-input.js").DraftImage[], completed: boolean, sending?: string, deliveryError?: string}} UserMessage */
/** @typedef {{kind: 'assistant-message', id: string, frame: ThreadFrame, text: string, completed: boolean, phase: string | null}} AssistantMessage */
/** @typedef {{kind: 'hook', id: string, key: string, frame: ThreadFrame, name: string, event: string, completed: boolean}} HookItem */
/** @typedef {{kind: 'subagent', id:string, frame:ThreadFrame, data:Record<string,unknown>}} SubagentItem */
/** @typedef {SubagentItem | ApprovalReviewItem | ToolCallItem | ThinkingItem | HookItem | ToolBatch | ToolError | UserMessage | AssistantMessage | TurnEnding | {kind: 'frame', id: string, frame: ThreadFrame}} FeedItem */

/** @param {FeedItem[]} feed @param {import('./thread-sending.js').Submission|null} pending @param {ThreadFrame|undefined} anchor @returns {FeedItem[]} */
export function withPendingMessage(feed, pending, anchor) {
  if (
    !pending ||
    !anchor ||
    feed.some((item) => item.id === `submission:${pending.id}`)
  )
    return feed;
  const items = [...feed];
  const index = items.findIndex((item) => item.frame.sequence > pending.after);
  items.splice(index < 0 ? items.length : index, 0, {
    kind: "user-message",
    id: `submission:${pending.id}`,
    frame: anchor,
    text: pending.text,
    draftImages: pending.images,
    completed: false,
    sending: pending.state,
    deliveryError: pending.error,
  });
  return items;
}
