import { thinkingTurnKey } from "./thread-thinking.js";
/** @typedef {import('./thread-feed.js').FeedItem} FeedItem */
/** @typedef {{kind:'worked',id:string,items:FeedItem[],memberIds:string[],durationMs:number|null}} WorkedGroup */
/** @param {FeedItem} item */
const turn = (item) =>
  "turn" in item
    ? item.turn
    : typeof item.frame.data?.turn_id === "string"
      ? item.frame.data.turn_id
      : "";
/** @param {FeedItem} item */
const key = (item) => thinkingTurnKey(item.frame, turn(item));
/** @param {FeedItem} item */
const eligible = (item) =>
  item.kind === "thinking"
    ? item.completed && !item.interrupted
    : item.kind === "tool-call" &&
      item.status === "completed" &&
      !item.interruptedByTurn;
/** @param {FeedItem[]} items */
function elapsed(items) {
  const valid = (/** @type {unknown} */ value) =>
    typeof value === "string" && Number.isFinite(Date.parse(value));
  const ranges = items.map((item) => {
    if (item.kind !== "thinking" && item.kind !== "tool-call") return [];
    const provider =
      item.kind === "thinking"
        ? [item.started, item.ended]
        : [item.data.started_at, item.data.completed_at];
    // Keep each interval on one clock. Claude often lacks provider start times,
    // but the persisted conversation item retains its observed lifecycle span.
    return provider.every(valid)
      ? provider
      : [item.started_at, item.completed_at];
  });
  if (ranges.some((pair) => pair.length !== 2 || !pair.every(valid)))
    return null;
  return Math.max(
    0,
    Math.max(...ranges.map((pair) => Date.parse(String(pair[1])))) -
      Math.min(...ranges.map((pair) => Date.parse(String(pair[0])))),
  );
}
/** @param {FeedItem[]} items @param {import('./threads.svelte').ThreadFrame|undefined} latestStart
 * @returns {(FeedItem|WorkedGroup)[]}
 */
export function groupOlderWork(items, latestStart) {
  let latest = "";
  for (const item of items) if (turn(item)) latest = key(item);
  if (typeof latestStart?.data?.turn_id === "string")
    latest = thinkingTurnKey(latestStart, latestStart.data.turn_id);
  /** @type {(FeedItem|WorkedGroup)[]} */
  const result = [];
  for (const item of items) {
    if (!turn(item) || key(item) === latest || !eligible(item)) {
      result.push(item);
      continue;
    }
    const previous = result.at(-1);
    const members =
      previous?.kind === "worked" && key(previous.items[0]) === key(item)
        ? [...previous.items, item]
        : [item];
    const group = {
      kind: /** @type {const} */ ("worked"),
      id: `worked:${members[0].id}`,
      items: members,
      memberIds: members.flatMap((member) => [
        `worked:${member.id}`,
        ...(member.kind === "thinking"
          ? member.memberIds || [member.id]
          : [member.id]),
      ]),
      durationMs: elapsed(members),
    };
    if (members.length > 1) result[result.length - 1] = group;
    else result.push(group);
  }
  return result;
}
