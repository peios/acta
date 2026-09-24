/** Merge current-state upserts; position and change order are independent.
 * @param {import('./code-threads.svelte').CATFrame[]} current
 * @param {import('./code-threads.svelte').CATFrame[]} incoming
 * @param {number} first Oldest retained chronological position on the host.
 */
export function mergeCodeFrames(current, incoming, first) {
  const byPosition = new Map(
    current.filter((f) => f.seq >= first).map((f) => [f.seq, f]),
  );
  for (const frame of incoming) {
    if (frame.seq < first) continue;
    const old = byPosition.get(frame.seq);
    if (!old || frame.change_seq > old.change_seq)
      byPosition.set(frame.seq, frame);
  }
  const frames = [...byPosition.values()].sort((a, b) => a.seq - b.seq);
  const encoder = new TextEncoder();
  const sizes = frames.map(
    (frame) => encoder.encode(JSON.stringify(frame)).length,
  );
  let bytes = sizes.reduce((sum, size) => sum + size, 0),
    start = 0;
  while (frames.length - start > 1000 || bytes > 1024 * 1024)
    bytes -= sizes[start++];
  return { frames: frames.slice(start), dropped: first > 1 || start > 0 };
}
