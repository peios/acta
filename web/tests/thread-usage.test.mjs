import test from "node:test";
import assert from "node:assert/strict";
import { threadUsage } from "../src/lib/thread-usage.js";
const frame = (kind, data, run_id = "run") => ({
  kind,
  data,
  run_id,
  received_at: "2026-09-07T20:00:00Z",
});

test("context fullness uses only reported current context usage", () => {
  const unknown = frame("usage/context", {
    context: { used_tokens: null, capacity_tokens: 258400 },
    cumulative: { total_tokens: 20908 },
    last_request: { input_tokens: 20901 },
  });
  assert.equal(threadUsage([unknown], "run")[0].percent, null);
  const known = frame("usage/context", {
    context: { used_tokens: 500, capacity_tokens: 1000 },
  });
  assert.equal(threadUsage([unknown, known], "run")[0].percent, 50);
  assert.equal(threadUsage([known, unknown], "run")[0].percent, null);
  assert.equal(threadUsage([known], "new-run")[0].percent, null);
  assert.equal(threadUsage([known], undefined)[0].percent, null);
});

test("zero usage is known, zero capacity and invalid numbers are unknown", () => {
  const context = (used_tokens, capacity_tokens) =>
    frame("usage/context", { context: { used_tokens, capacity_tokens } });
  assert.equal(threadUsage([context(0, 1000)], "run")[0].percent, 0);
  assert.equal(threadUsage([context(100, 0)], "run")[0].percent, null);
  assert.equal(threadUsage([context(-1, 1000)], "run")[0].percent, null);
  assert.equal(threadUsage([context(Infinity, 1000)], "run")[0].percent, null);
});

test("account snapshots replace windows per bucket without losing other buckets", () => {
  const account = (bucket_id, windows) =>
    frame("usage/account", { bucket_id, windows });
  const weekly = {
    duration_minutes: 10080,
    used_percent: 85,
    resets_at: "2026-09-13T19:38:00Z",
  };
  const hourly = { duration_minutes: 300, used_percent: 0 };
  const a = account("a", [weekly, hourly]),
    b = account("b", [hourly]);
  const gauges = threadUsage([a, b, account("a", [weekly])], "run");
  assert.deepEqual(
    gauges.map((g) => g.label),
    ["Ctx", "7d", "5h"],
  );
  assert.deepEqual(
    gauges.map((g) => g.percent),
    [null, 85, 0],
  );
  assert.notEqual(gauges[1].id, gauges[2].id);
  assert.equal(threadUsage([a, account("a", [])], "run").length, 1);
});

test("unknown account usage never becomes zero and overage is retained", () => {
  const f = frame("usage/account", {
    windows: [
      { used_percent: null },
      { used_percent: 105 },
      { used_percent: "80" },
    ],
  });
  assert.deepEqual(
    threadUsage([f], "run")
      .slice(1)
      .map((g) => g.percent),
    [null, 105, null],
  );
  assert.equal(threadUsage([f], "new-run").length, 1);
});
