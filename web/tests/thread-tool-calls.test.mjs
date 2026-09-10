import test from "node:test";
import assert from "node:assert/strict";
import { threadFeed } from "./reference/thread-feed.js";
function fixture() {
  let n = 0;
  return (kind, data, run_id = "run") => ({
    thread_id: "thread",
    run_id,
    sequence: ++n,
    output_index: 1,
    provider: "claude",
    received_at: "2026-09-08T12:00:00Z",
    kind,
    data,
  });
}
const base = {
  tool_id: "call",
  turn_id: "turn",
  name: "Bash",
  category: "command",
  label: "Run command",
  status: "preparing",
  arguments: {},
  output: null,
  stdout: null,
  stderr: null,
  cwd: null,
  exit_code: null,
  duration_ms: null,
  started_at: null,
  completed_at: null,
};
test("arguments stream before pending and output reconciles without duplication", () => {
  const f = fixture();
  const start = f("tool/call", base);
  const a = f("tool/arguments/delta", {
    tool_id: "call",
    turn_id: "turn",
    text: '{"command":',
  });
  const b = f("tool/arguments/delta", {
    tool_id: "call",
    turn_id: "turn",
    text: '"ls"}',
  });
  assert.equal(
    threadFeed([start, a, a, b])[0].argumentsText,
    '{"command":"ls"}',
  );
  const pending = f("tool/call", {
    ...base,
    status: "pending",
    arguments: { command: "ls" },
  });
  const output = f("tool/output/delta", {
    tool_id: "call",
    turn_id: "turn",
    text: "file\n",
  });
  const done = f("tool/call", {
    ...base,
    status: "completed",
    arguments: { command: "ls" },
    output: "file\n",
    exit_code: 0,
  });
  const items = threadFeed([
    start,
    a,
    b,
    pending,
    output,
    output,
    done,
    done,
    f("tool/output/delta", { tool_id: "call", turn_id: "turn", text: "late" }),
  ]);
  assert.equal(items.length, 1);
  assert.equal(items[0].frame, start);
  assert.equal(items[0].output, "file\n");
  assert.equal(items[0].status, "completed");
  assert.deepEqual(JSON.parse(items[0].argumentsText), { command: "ls" });
});
test("failed calls retain exact error output and result-only histories render", () => {
  const f = fixture();
  const done = f("tool/call", {
    ...base,
    status: "failed",
    output: "File does not exist.\n",
  });
  const item = threadFeed([done])[0];
  assert.equal(item.status, "failed");
  assert.equal(item.output, "File does not exist.\n");
});
test("turn ending interrupts only outstanding calls in the corresponding run", () => {
  const f = fixture();
  const items = threadFeed([
    f("tool/call", { ...base, status: "running" }),
    f("tool/call", base, "other"),
    f("turn/completed", { turn_id: "turn", outcome: "interrupted" }),
  ]).filter((i) => i.kind === "tool-call");
  assert.equal(items[0].status, "interrupted");
  assert.equal(items[1].status, "preparing");
});

test("late authoritative result repairs a turn-end interruption placeholder", () => {
  const f = fixture();
  const items = threadFeed([
    f("tool/call", { ...base, status: "running" }),
    f("turn/completed", { turn_id: "turn", outcome: "completed" }),
    f("tool/call", { ...base, status: "completed", output: "done" }),
  ]);
  const item = items.find((i) => i.kind === "tool-call");
  assert.equal(item.status, "completed");
  assert.equal(item.output, "done");
});

test("denial and result stay in one row in either order, retaining reason and output", () => {
  for (const denialFirst of [true, false]) {
    const f = fixture();
    const start = f("tool/call", {
      ...base,
      name: "Write",
      category: "write",
      label: "Write check.txt",
    });
    const denied = {
      ...base,
      status: "permission_denied",
      permission_denial: {
        message: "Permission was not granted",
        reason: "Outside working directories",
        reason_type: "workingDir",
      },
    };
    const frames = denialFirst
      ? [
          f("tool/call", denied),
          f("tool/call", { ...denied, output: "Exact error result" }),
        ]
      : [
          f("tool/call", {
            ...base,
            status: "failed",
            output: "Exact error result",
          }),
          f("tool/call", { ...denied, output: "Exact error result" }),
        ];
    const items = threadFeed([
      start,
      ...frames,
      f("tool/call", { ...base, status: "pending" }),
      f("tool/output/delta", {
        tool_id: "call",
        turn_id: "turn",
        text: "stale",
      }),
    ]);
    assert.equal(items.length, 1);
    assert.equal(items[0].frame, start);
    assert.equal(items[0].status, "permission_denied");
    assert.equal(items[0].output, "Exact error result");
    assert.equal(items[0].data.permission_denial.reason_type, "workingDir");
  }
});
