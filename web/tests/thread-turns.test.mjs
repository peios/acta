import test from "node:test";
import assert from "node:assert/strict";
import { threadFeed } from "./reference/thread-feed.js";

function fixture() {
  let sequence = 0;
  return (kind, turn_id, data = {}, run_id = "run") => ({
    thread_id: "thread",
    run_id,
    sequence: ++sequence,
    output_index: 1,
    kind,
    data: { turn_id, ...data },
    received_at: "2026-09-07T20:00:00Z",
  });
}
const endings = (frames) =>
  threadFeed(frames).filter((item) => item.kind === "turn-ended");

test("ending uses reported duration and only the last model call from the same turn and run", () => {
  const f = fixture();
  const usage = f("usage/context", "a", {
    last_request: {
      input_tokens: 20,
      cached_input_tokens: 10,
      output_tokens: 3,
      total_tokens: 23,
    },
    cumulative: { total_tokens: 1000 },
  });
  const other = f("usage/context", "b", {
    last_request: { total_tokens: 900 },
  });
  const wrongRun = f(
    "usage/context",
    "a",
    { last_request: { total_tokens: 800 } },
    "old",
  );
  const done = f("turn/completed", "a", {
    outcome: "completed",
    duration_ms: 4364,
  });
  const [ending] = endings([usage, other, wrongRun, done]);
  assert.equal(ending.durationMs, 4364);
  assert.equal(ending.label, "Turn ended");
  assert.equal(ending.tokens.find((x) => x.label === "Total tokens").value, 23);
  assert.equal(ending.frame, done);
});

test("late usage enriches the anchored ending without duplicating it or summing snapshots", () => {
  const f = fixture();
  const done = f("turn/completed", "a", { outcome: "completed" });
  const unrelated = f("debug/resolved", null);
  const old = f("usage/context", "a", { last_request: { total_tokens: 10 } });
  const latest = f("usage/context", "a", {
    last_request: { total_tokens: 15 },
  });
  const duplicate = f("turn/completed", "a", { outcome: "completed" });
  const feed = threadFeed([done, unrelated, old, latest, duplicate]);
  assert.equal(feed[0].kind, "turn-ended");
  assert.equal(endings([done, old, latest, duplicate]).length, 1);
  assert.equal(feed[0].tokens[0].value, 15);
  assert.equal(feed[0].durationMs, null);
});

test("unknown metrics remain absent while failures and interruptions retain their outcomes", () => {
  const f = fixture();
  const usage = f("usage/context", "a", {
    last_request: { total_tokens: null, input_tokens: -1, output_tokens: 0 },
    cumulative: { total_tokens: 999 },
  });
  const failed = f("turn/completed", "a", {
    outcome: "failed",
    duration_ms: -1,
    error: { message: "Provider error" },
  });
  const stopped = f("turn/completed", "b", { outcome: "interrupted" });
  const [a, b] = endings([usage, failed, stopped]);
  assert.equal(a.label, "Turn failed");
  assert.equal(a.durationMs, null);
  assert.equal(a.error, "Provider error");
  assert.deepEqual(a.tokens, [{ label: "Output tokens", value: 0 }]);
  assert.equal(b.label, "Turn interrupted");
  assert.deepEqual(b.tokens, []);
});

test("unsupported completion stays visible and does not block a later valid ending", () => {
  const f = fixture();
  const unknown = f("turn/completed", "a", { outcome: "future-outcome" });
  const valid = f("turn/completed", "a", { outcome: "completed" });
  assert.deepEqual(
    threadFeed([unknown, valid]).map((item) => item.kind),
    ["frame", "turn-ended"],
  );
});

test("latest collected diff enriches the turn ending, including late updates and undo", () => {
  const f = fixture();
  const old = f("turn/diff", "a", { diff: "old patch" });
  const done = f("turn/completed", "a", { outcome: "completed" });
  const latest = f("turn/diff", "a", { diff: "combined patch" });
  const other = f("turn/diff", "b", { diff: "wrong turn" });
  const wrongRun = f("turn/diff", "a", { diff: "wrong run" }, "other");
  const feed = threadFeed([old, done, latest, old, other, wrongRun]);
  assert.equal(feed.length, 1);
  assert.equal(feed[0].frame, done);
  assert.equal(feed[0].diff, "combined patch");
  const [cleared] = endings([
    old,
    done,
    latest,
    f("turn/diff", "a", { diff: "" }),
  ]);
  assert.equal(cleared.diff, "");
  assert.equal(endings([done])[0].diff, null);
});
