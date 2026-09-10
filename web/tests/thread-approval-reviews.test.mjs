import assert from "node:assert/strict";
import { threadFeed } from "./reference/thread-feed.js";
const start = {
  thread_id: "thread",
  run_id: "run",
  sequence: 1,
  output_index: 1,
  kind: "approval/review",
  data: {
    review_id: "review",
    turn_id: "turn",
    tool_id: "tool",
    status: "in_progress",
    title: "Change files",
    details: {},
  },
};
const end = {
  ...start,
  sequence: 4,
  data: {
    ...start.data,
    status: "approved",
    rationale: "User approved the edit.",
  },
};
const frames = [start, { ...start, sequence: 2, kind: "debug/resolved" }, end];
let reviews = threadFeed(frames).filter((i) => i.kind === "approval-review");
assert.equal(reviews.length, 1);
assert.equal(reviews[0].frame.sequence, 1);
assert.equal(reviews[0].data.status, "approved");
// Replay cannot reopen a completed review, even with a later duplicate start.
reviews = threadFeed([...frames, end, { ...start, sequence: 5 }]).filter(
  (i) => i.kind === "approval-review",
);
assert.equal(reviews.length, 1);
assert.equal(reviews[0].data.status, "approved");
// Two reviews of the same tool and the same review ID in another run are distinct.
assert.equal(
  threadFeed([
    start,
    { ...start, sequence: 2, data: { ...start.data, review_id: "other" } },
    { ...start, sequence: 3, run_id: "new" },
  ]).filter((i) => i.kind === "approval-review").length,
  3,
);
assert.equal(threadFeed([end])[0].data.status, "approved");
const completed = {
  ...start,
  sequence: 3,
  kind: "turn/completed",
  data: { turn_id: "turn", status: "completed" },
};
assert.equal(threadFeed([start, completed])[0].interrupted, true);
assert.equal(
  threadFeed([start, { ...completed, run_id: "different" }])[0].interrupted,
  false,
);
assert.equal(threadFeed([start, completed, end])[0].interrupted, false);
