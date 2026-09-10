/** @typedef {import('./threads.svelte').ThreadFrame} ThreadFrame */
/** @typedef {{kind: 'thinking', id: string, started_at?: string, completed_at?: string | null, memberIds?: string[], frame: ThreadFrame, turn: string, text: string, tokens: number | null, started: string | null, ended: string | null, completed: boolean, interrupted: boolean}} ThinkingItem */
/** Group only adjacent visible entries from the same turn and lane. Persisted
 * snapshots remain independent, so later updates and older pages reconcile normally.
 * @param {import('./thread-feed.js').FeedItem[]} items
 * @returns {import('./thread-feed.js').FeedItem[]}
 */
export function groupThinking(items) {
  /** @type {import('./thread-feed.js').FeedItem[]} */
  const grouped = [];
  for (const item of items) {
    const previous = grouped.at(-1);
    if (
      item.kind !== "thinking" ||
      previous?.kind !== "thinking" ||
      !item.turn ||
      previous.turn !== item.turn ||
      previous.frame.thread_id !== item.frame.thread_id ||
      previous.frame.run_id !== item.frame.run_id ||
      previous.frame.lane_id !== item.frame.lane_id
    ) {
      grouped.push(item);
      continue;
    }
    const completed = previous.completed && item.completed;
    const active =
      (!previous.completed && !previous.interrupted) ||
      (!item.completed && !item.interrupted);
    grouped[grouped.length - 1] = {
      ...previous,
      started_at:
        previous.started_at && item.started_at
          ? Date.parse(previous.started_at) <= Date.parse(item.started_at)
            ? previous.started_at
            : item.started_at
          : undefined,
      completed_at:
        !active && previous.completed_at && item.completed_at
          ? Date.parse(previous.completed_at) >= Date.parse(item.completed_at)
            ? previous.completed_at
            : item.completed_at
          : null,
      memberIds: [
        ...(previous.memberIds || [previous.id]),
        ...(item.memberIds || [item.id]),
      ],
      text: [previous.text, item.text]
        .filter((text) => text.trim())
        .join("\n\n"),
      tokens:
        previous.tokens !== null && item.tokens !== null
          ? previous.tokens + item.tokens
          : null,
      started:
        previous.started && item.started
          ? Date.parse(previous.started) <= Date.parse(item.started)
            ? previous.started
            : item.started
          : null,
      ended:
        !active && previous.ended && item.ended
          ? Date.parse(previous.ended) >= Date.parse(item.ended)
            ? previous.ended
            : item.ended
          : null,
      completed,
      interrupted: !completed && !active,
    };
  }
  return grouped;
}

/** @param {ThreadFrame} frame @param {string} turn */
export const thinkingTurnKey = (frame, turn) =>
  JSON.stringify([frame.thread_id, frame.run_id, frame.lane_id || "", turn]);

/** Turn completion, rather than completion of a thinking block, closes its text.
 * @param {import('./thread-feed.js').FeedItem[]} items
 */
export function finishedThinkingTurns(items) {
  return new Set(
    items
      .filter((item) => item.kind === "turn-ended")
      .flatMap((item) =>
        typeof item.frame.data?.turn_id === "string"
          ? [thinkingTurnKey(item.frame, item.frame.data.turn_id)]
          : [],
      ),
  );
}
