// Frozen pre-migration assembler used only as a behaviour oracle in tests.
import { frameId, turnKey, uniqueFrames } from "./thread-frames.js";
import { toolCallItems } from "./thread-tool-calls.js";
import { approvalReviewItems } from "./thread-approval-reviews.js";
/** @typedef {import('./thread-approval-reviews.js').ApprovalReviewItem} ApprovalReviewItem */
/** @typedef {import("./thread-tool-calls.js").ToolCallItem} ToolCallItem */
import { thinkingItems } from "./thread-thinking.js";
/** @typedef {import("./thread-thinking.js").ThinkingItem} ThinkingItem */
import { turnEndings } from "./thread-turns.js";
/** @typedef {import('../../src/lib/threads.svelte').ThreadFrame} ThreadFrame */
/** @typedef {import('./thread-turns.js').TurnEnding} TurnEnding */
/** @typedef {{name: string, ready: boolean}} ToolServer */
/** @typedef {{kind: 'tools', id: string, frame: ThreadFrame, servers: ToolServer[], stacked: boolean, sealed: boolean}} ToolBatch */
/** @typedef {{kind: 'tool-error', id: string, frame: ThreadFrame, name: string, cancelled: boolean, message: string, reason: string}} ToolError */
/** @typedef {{kind: 'user-message', id: string, frame: ThreadFrame, text: string, completed: boolean, sending?: string, deliveryError?: string}} UserMessage */
/** @typedef {{kind: 'assistant-message', id: string, frame: ThreadFrame, text: string, completed: boolean, phase: string | null}} AssistantMessage */
/** @typedef {{kind: 'hook', id: string, key: string, frame: ThreadFrame, name: string, event: string, completed: boolean}} HookItem */
/** @typedef {ApprovalReviewItem | ToolCallItem | ThinkingItem | HookItem | ToolBatch | ToolError | UserMessage | AssistantMessage | TurnEnding | {kind: 'frame', id: string, frame: ThreadFrame}} FeedItem */

/** Read the latest status snapshot from the current provider run.
 * Turn lifecycle frames do not determine thread activity.
 * @param {ThreadFrame[]} frames
 * @param {string | undefined} runId
 */
export function isThreadActive(frames, runId) {
  if (!runId) return false;
  return (
    frames.findLast(
      (frame) => frame.run_id === runId && frame.kind === "thread/status",
    )?.data?.status === "active"
  );
}

/** Project ordered captures into the feed. Rebuilding from history makes pagination and replay deterministic.
 * @param {ThreadFrame[]} frames
 * @returns {FeedItem[]}
 */
export function threadFeed(frames) {
  const endings = turnEndings(frames);
  const thinking = thinkingItems(frames);
  const calls = toolCallItems(frames);
  const reviews = approvalReviewItems(frames);
  const endedTurns = new Set();
  /** @type {FeedItem[]} */
  const items = [];
  /** @type {Map<string, ToolBatch>} */
  const open = new Map();
  /** @type {Map<string, ToolBatch>} */
  const pending = new Map();
  /** @type {Map<string, string>} */
  const terminal = new Map();
  /** @type {Map<string, UserMessage>} */
  const messages = new Map();
  /** @type {Map<string, AssistantMessage>} */
  const assistantMessages = new Map();
  const completedHooks = new Set();
  const startedHooks = new Set();
  /** @param {ToolBatch} batch */
  function seal(batch) {
    if (batch.servers.every((server) => server.ready)) {
      batch.sealed = true;
      open.delete(batch.frame.run_id);
    }
  }
  for (const frame of uniqueFrames(frames)) {
    const id = frameId(frame);
    if (frame.kind === "approval/review") {
      const review = reviews.get(id);
      if (review) items.push(review);
      continue;
    }
    if (
      ["tool/call", "tool/arguments/delta", "tool/output/delta"].includes(
        frame.kind,
      )
    ) {
      const call = calls.get(id);
      if (call) items.push(call);
      continue;
    }
    if (
      ["thinking/started", "thinking/delta", "thinking/completed"].includes(
        frame.kind,
      )
    ) {
      const item = thinking.get(id);
      if (item) items.push(item);
      continue;
    }
    if (frame.kind === "hook/started" || frame.kind === "hook/completed") {
      const data = frame.data ?? {};
      if (
        typeof data.hook_id === "string" &&
        typeof data.name === "string" &&
        typeof data.event === "string"
      ) {
        const key = JSON.stringify([
          frame.thread_id,
          frame.run_id,
          data.hook_id,
        ]);
        const completed = frame.kind === "hook/completed";
        const observed = completed ? completedHooks : startedHooks;
        if (observed.has(key)) continue;
        observed.add(key);
        items.push({
          kind: "hook",
          id,
          key,
          frame,
          name: data.name,
          event: data.event,
          completed,
        });
        continue;
      }
    }
    if (
      frame.kind === "turn/diff" &&
      typeof frame.data?.turn_id === "string" &&
      typeof frame.data?.diff === "string"
    )
      continue;
    if (frame.kind === "turn/completed") {
      const key = turnKey(frame);
      if (endedTurns.has(key)) continue;
      const ending = endings.get(id);
      if (ending) {
        items.push(ending);
        endedTurns.add(key);
        continue;
      }
    }
    if (
      frame.kind === "message/assistant" ||
      frame.kind === "message/assistant/delta"
    ) {
      const data = frame.data ?? {};
      const delta = frame.kind === "message/assistant/delta";
      if (
        typeof data.message_id === "string" &&
        typeof data.text === "string" &&
        (delta || data.state === "in_progress" || data.state === "completed")
      ) {
        const key = JSON.stringify([frame.thread_id, data.message_id]);
        let message = assistantMessages.get(key);
        if (!message) {
          message = {
            kind: "assistant-message",
            id,
            frame,
            text: "",
            completed: false,
            phase: null,
          };
          assistantMessages.set(key, message);
          items.push(message);
        }
        if (delta) {
          if (!message.completed) message.text += data.text;
        } else if (!message.completed || data.state === "completed") {
          message.text = data.text;
          message.completed = data.state === "completed";
          message.phase = typeof data.phase === "string" ? data.phase : null;
        }
        continue;
      }
    }
    if (frame.kind === "message/user") {
      const data = frame.data ?? {};
      const content = data.content;
      if (
        typeof data.message_id === "string" &&
        ["in_progress", "completed"].includes(String(data.state)) &&
        Array.isArray(content) &&
        content.every(
          (part) =>
            part && part.type === "text" && typeof part.text === "string",
        )
      ) {
        const key = JSON.stringify([frame.thread_id, data.message_id]);
        let message = messages.get(key);
        if (!message) {
          message = {
            kind: "user-message",
            id:
              typeof data.submission_id === "string"
                ? `submission:${data.submission_id}`
                : id,
            frame,
            text: "",
            completed: false,
          };
          messages.set(key, message);
          items.push(message);
        }
        if (!message.completed || data.state === "completed") {
          message.text = content.map((part) => part.text).join("\n");
          message.completed = data.state === "completed";
        }
        continue;
      }
      // Keep unsupported content visible rather than silently dropping it.
    }
    if (frame.kind !== "mcp/server/status") {
      items.push({ kind: "frame", id, frame });
      continue;
    }
    const data = frame.data ?? {};
    const name = data.server_name;
    const status = data.status;
    if (
      typeof name !== "string" ||
      !["starting", "ready", "failed", "cancelled"].includes(String(status))
    ) {
      items.push({ kind: "frame", id, frame });
      continue;
    }
    const key = JSON.stringify([frame.run_id, name]);
    let batch = pending.get(key);
    if (status === "starting") {
      if (batch) continue; // Repeated observation of the same outstanding startup.
      terminal.delete(key);
      batch = open.get(frame.run_id);
      if (!batch) {
        batch = {
          kind: "tools",
          id,
          frame,
          servers: [],
          stacked: false,
          sealed: false,
        };
        open.set(frame.run_id, batch);
        items.push(batch);
      }
      const member = batch.servers.find((server) => server.name === name);
      if (member) member.ready = false;
      else batch.servers.push({ name, ready: false });
      batch.stacked ||= batch.servers.length > 1;
      pending.set(key, batch);
      continue;
    }
    const signature = JSON.stringify([status, data.error, data.failure_reason]);
    if (!batch && terminal.get(key) === signature) continue;
    terminal.set(key, signature);
    pending.delete(key);
    if (status === "ready") {
      if (batch) {
        const member = batch.servers.find((server) => server.name === name);
        if (member) member.ready = true;
        seal(batch);
      } else {
        // A terminal observation without a captured start still has a position.
        items.push({
          kind: "tools",
          id,
          frame,
          servers: [{ name, ready: true }],
          stacked: false,
          sealed: true,
        });
      }
    } else {
      if (batch) {
        batch.servers = batch.servers.filter((server) => server.name !== name);
        seal(batch);
      }
      const error = data.error;
      const message =
        error &&
        typeof error === "object" &&
        "message" in error &&
        typeof error.message === "string"
          ? error.message
          : "";
      items.push({
        kind: "tool-error",
        id,
        frame,
        name,
        cancelled: status === "cancelled",
        message,
        reason:
          typeof data.failure_reason === "string" ? data.failure_reason : "",
      });
    }
  }
  return items.filter(
    (item) =>
      (item.kind !== "hook" ||
        item.completed ||
        !completedHooks.has(item.key)) &&
      (item.kind !== "tools" || item.servers.length > 0) &&
      (item.kind !== "assistant-message" || item.text.trim().length > 0),
  );
}

/** @param {FeedItem[]} feed @param {import('../../src/lib/thread-sending.js').Submission|null} pending @param {ThreadFrame|undefined} anchor @returns {FeedItem[]} */
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
    completed: false,
    sending: pending.state,
    deliveryError: pending.error,
  });
  return items;
}
