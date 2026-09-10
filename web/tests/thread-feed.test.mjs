import test from "node:test";
import assert from "node:assert/strict";
import { threadFeed, isThreadActive } from "./reference/thread-feed.js";

test("working follows latest thread status in the current run", () => {
  const { frame } = fixture();
  const active = frame("thread/status", { status: "active", waiting_for: [] });
  const idle = frame("thread/status", { status: "idle", waiting_for: [] });
  assert.equal(isThreadActive([], "run"), false);
  assert.equal(isThreadActive([active], "run"), true);
  assert.equal(isThreadActive([active, idle], "run"), false);
  assert.equal(isThreadActive([idle, active], "run"), true);
  assert.equal(isThreadActive([active], "other"), false);
  assert.equal(isThreadActive([active], undefined), false);
  const other = frame("thread/status", { status: "idle" }, "other");
  assert.equal(isThreadActive([active, other], "run"), true);
});

test("turn starts and completions never override thread status", () => {
  const { frame } = fixture();
  const start = frame("turn/started", { turn_id: "a" });
  const done = frame("turn/completed", { turn_id: "a", outcome: "completed" });
  const active = frame("thread/status", { status: "active" });
  const idle = frame("thread/status", { status: "idle" });
  assert.equal(isThreadActive([start], "run"), false);
  assert.equal(isThreadActive([active, done], "run"), true);
  assert.equal(isThreadActive([idle, start], "run"), false);
  assert.equal(
    isThreadActive(
      [active, frame("thread/status", { status: "unknown" })],
      "run",
    ),
    false,
  );
});

function fixture() {
  let sequence = 0;
  const frame = (kind, data = {}, run_id = "run") => ({
    thread_id: "thread",
    run_id,
    sequence: ++sequence,
    output_index: 1,
    received_at: "2026-09-07T20:00:00Z",
    provider: "codex",
    kind,
    data,
    raw_json: JSON.stringify(data),
  });
  return {
    frame,
    status: (name, status, extra = {}, run = "run") =>
      frame("mcp/server/status", { server_name: name, status, ...extra }, run),
  };
}

test("user message completion updates its original bubble with final text", () => {
  const { frame } = fixture();
  const user = (state, text) =>
    frame("message/user", {
      message_id: "message",
      state,
      content: [{ type: "text", text }],
    });
  const start = user("in_progress", "Draft");
  const initial = threadFeed([start]);
  assert.equal(initial[0].completed, false);
  const intervening = frame("debug/resolved");
  const done = user("completed", "Final\nmessage");
  const result = threadFeed([start, intervening, done, start]);
  assert.deepEqual(
    result.map((item) => item.kind),
    ["user-message", "frame"],
  );
  assert.equal(result[0].id, initial[0].id);
  assert.equal(result[0].text, "Final\nmessage");
  assert.equal(result[0].completed, true);
  assert.equal(initial[0].text, "Draft");
  assert.equal(
    threadFeed([done, user("in_progress", "Stale")])[0].text,
    "Final\nmessage",
  );
});

test("message identity is scoped to the Acta thread, not matching text", () => {
  const { frame } = fixture();
  const data = {
    message_id: "one",
    state: "completed",
    content: [{ type: "text", text: "Same" }],
  };
  const a = frame("message/user", data);
  const b = frame("message/user", { ...data, message_id: "two" });
  const resumed = frame("message/user", data, "new-run");
  const otherThread = { ...frame("message/user", data), thread_id: "other" };
  assert.equal(threadFeed([a, b, resumed, otherThread]).length, 3);
});

test("unsupported user content stays visible as a raw card", () => {
  const { frame } = fixture();
  const raw = frame("message/user", {
    message_id: "a",
    state: "completed",
    content: [{ type: "image", url: "example" }],
  });
  assert.equal(threadFeed([raw])[0].kind, "frame");
});

test("assistant deltas grow one anchored message and completion replaces the text", () => {
  const { frame } = fixture();
  const start = frame("message/assistant", {
    message_id: "a",
    state: "in_progress",
    text: "",
    phase: "commentary",
  });
  const unrelated = frame("debug/resolved");
  const first = frame("message/assistant/delta", {
    message_id: "a",
    text: "**Hello",
  });
  const second = frame("message/assistant/delta", {
    message_id: "a",
    text: " world**",
  });
  assert.deepEqual(threadFeed([start]), []);
  const partial = threadFeed([start, unrelated, first, first, second]);
  assert.deepEqual(
    partial.map((item) => item.kind),
    ["assistant-message", "frame"],
  );
  assert.equal(partial[0].frame, start);
  assert.equal(partial[0].text, "**Hello world**");
  assert.equal(partial[0].phase, "commentary");
  assert.equal(partial[0].completed, false);
  const done = frame("message/assistant", {
    message_id: "a",
    state: "completed",
    text: "Final answer.",
    phase: "final_answer",
  });
  const result = threadFeed([start, unrelated, first, second, done]);
  assert.equal(result[0].id, partial[0].id);
  assert.equal(result[0].text, "Final answer.");
  assert.equal(result[0].phase, "final_answer");
  assert.equal(result[0].completed, true);
  assert.equal(partial[0].text, "**Hello world**");
});

test("distinct identical deltas count, but late deltas and starts cannot regress completion", () => {
  const { frame } = fixture();
  const delta = () =>
    frame("message/assistant/delta", { message_id: "a", text: "ha" });
  const one = delta(),
    two = delta();
  assert.equal(threadFeed([one, two, one])[0].text, "haha");
  const done = frame("message/assistant", {
    message_id: "a",
    state: "completed",
    text: "Done",
    phase: "final_answer",
  });
  const lateStart = frame("message/assistant", {
    message_id: "a",
    state: "in_progress",
    text: "",
    phase: "commentary",
  });
  const result = threadFeed([one, two, done, delta(), lateStart]);
  assert.equal(result.length, 1);
  assert.equal(result[0].text, "Done");
  assert.equal(result[0].phase, "final_answer");
});

test("assistant messages remain separate by identity and tolerate missing start frames", () => {
  const { frame } = fixture();
  const a = frame("message/assistant/delta", {
    message_id: "a",
    text: "First",
  });
  const b = frame("message/assistant", {
    message_id: "b",
    state: "completed",
    text: "Second",
    phase: "final_answer",
  });
  const resumed = frame(
    "message/assistant",
    {
      message_id: "a",
      state: "completed",
      text: "First complete",
      phase: "commentary",
    },
    "other-run",
  );
  const result = threadFeed([a, b, resumed]);
  assert.deepEqual(
    result.map((item) => item.text),
    ["First complete", "Second"],
  );
  assert.equal(result[0].frame, a);
  assert.equal(
    threadFeed([frame("message/assistant/delta", { text: "No identity" })])[0]
      .kind,
    "frame",
  );
});

test("starts collect at the first frame position and seal before later starts", () => {
  const { frame, status } = fixture();
  const frames = [frame("message/user"), status("a", "starting")];
  const singleton = threadFeed(frames)[1];
  assert.equal(singleton.stacked, false);
  assert.equal(singleton.sealed, false);
  frames.push(
    frame("turn/started"),
    status("b", "starting"),
    status("a", "ready"),
  );
  const partial = threadFeed(frames);
  assert.deepEqual(
    partial.map((x) => x.kind),
    ["frame", "tools", "frame"],
  );
  assert.equal(partial[1].id, singleton.id);
  assert.equal(partial[1].stacked, true);
  assert.deepEqual(partial[1].servers, [
    { name: "a", ready: true },
    { name: "b", ready: false },
  ]);
  frames.push(status("b", "ready"), status("c", "starting"));
  const finished = threadFeed(frames);
  assert.equal(finished[1].sealed, true);
  assert.equal(finished[3].sealed, false);
  assert.deepEqual(finished[3].servers, [{ name: "c", ready: false }]);
  // The projection does not mutate old views or captured input.
  assert.equal(partial[1].sealed, false);
  const frozen = structuredClone(frames);
  frozen.forEach((f) => {
    Object.freeze(f.data);
    Object.freeze(f);
  });
  assert.deepEqual(threadFeed(Object.freeze(frozen)), finished);
});

test("failures leave their batch and appear at the failure frame position", () => {
  const { frame, status } = fixture();
  const frames = [
    status("a", "starting"),
    status("b", "starting"),
    status("a", "ready"),
    frame("message/user"),
  ];
  const failed = status("b", "failed", {
    error: { message: "Connection refused" },
    failure_reason: "transport",
  });
  frames.push(failed, status("c", "starting"));
  const result = threadFeed(frames);
  assert.deepEqual(
    result.map((x) => x.kind),
    ["tools", "frame", "tool-error", "tools"],
  );
  assert.equal(result[0].sealed, true);
  assert.equal(result[0].stacked, true);
  assert.deepEqual(result[0].servers, [{ name: "a", ready: true }]);
  assert.equal(result[2].frame, failed);
  assert.equal(result[2].message, "Connection refused");
  assert.equal(result[2].reason, "transport");
});

test("empty failed batches disappear and a retry starts a fresh batch", () => {
  const { frame, status } = fixture();
  const frames = [
    status("a", "starting"),
    frame("message/user"),
    status("a", "failed"),
    status("a", "starting"),
    status("a", "ready"),
  ];
  const result = threadFeed(frames);
  assert.deepEqual(
    result.map((x) => x.kind),
    ["frame", "tool-error", "tools"],
  );
  assert.equal(result[2].frame, frames[3]);
  assert.equal(result[2].sealed, true);
});

test("failure during a pending stack removes only that member", () => {
  const { status } = fixture();
  const result = threadFeed([
    status("a", "starting"),
    status("b", "starting"),
    status("a", "cancelled"),
  ]);
  assert.deepEqual(result[0].servers, [{ name: "b", ready: false }]);
  assert.equal(result[0].sealed, false);
  assert.equal(result[0].stacked, true);
  assert.equal(result[1].cancelled, true);
});

test("duplicate captures and repeated status observations do not inflate the feed", () => {
  const { status } = fixture();
  const start = status("a", "starting");
  const ready = status("a", "ready");
  const failure = status("b", "failed", { error: { message: "Oops" } });
  const result = threadFeed([
    start,
    start,
    status("a", "starting"),
    ready,
    ready,
    status("a", "ready"),
    failure,
    failure,
    status("b", "failed", { error: { message: "Oops" } }),
  ]);
  assert.equal(result.length, 2);
  assert.equal(result[0].servers.length, 1);
  assert.equal(result[0].sealed, true);
  assert.equal(result[1].kind, "tool-error");
});

test("the same server in different provider runs is independent", () => {
  const { status } = fixture();
  const result = threadFeed([
    status("a", "starting"),
    status("a", "starting", {}, "other"),
    status("a", "ready"),
  ]);
  assert.equal(result.length, 2);
  assert.equal(result[0].sealed, true);
  assert.equal(result[1].sealed, false);
});

test("unmatched ready is visible and unknown MCP states retain their raw card", () => {
  const { status } = fixture();
  const result = threadFeed([
    status("a", "ready"),
    status("a", "ready"),
    status("a", "future-status"),
  ]);
  assert.equal(result.length, 2);
  assert.equal(result[0].sealed, true);
  assert.equal(result[1].kind, "frame");
});

test("debug captures keep their positions while statuses are projected", () => {
  const { frame, status } = fixture();
  const debug = frame("debug/resolved");
  const start = { ...status("a", "starting"), sequence: debug.sequence };
  debug.output_index = 0;
  const result = threadFeed([debug, start]);
  assert.deepEqual(
    result.map((x) => x.kind),
    ["frame", "tools"],
  );
  assert.notEqual(result[0].id, result[1].id);
});

test("hook response replaces the start at the response position, scoped to a run", () => {
  const hook = (sequence, kind, run_id = "run") => ({
    thread_id: "thread",
    run_id,
    sequence,
    output_index: 1,
    kind,
    data: {
      hook_id: "hook",
      name: "Load notes",
      event: "SessionStart",
      status: "completed",
    },
  });
  const start = hook(1, "hook/started");
  const middle = hook(2, "debug/unknown");
  const done = hook(3, "hook/completed");
  const nextRun = hook(4, "hook/started", "new-run");
  assert.deepEqual(
    threadFeed([start, middle]).map((i) => i.kind),
    ["hook", "frame"],
  );
  const feed = threadFeed([start, middle, done, done, nextRun]);
  assert.deepEqual(
    feed.map((i) => i.frame.sequence),
    [2, 3, 4],
  );
  assert.equal(feed[1].completed, true);
  assert.equal(feed[2].completed, false);
  assert.equal(threadFeed([done])[0].completed, true);
});

test("thinking reconciles snapshots in place without duplicating text or estimates", () => {
  const { frame } = fixture();
  const data = {
    thinking_id: "r",
    turn_id: "turn",
    text: "",
    estimated_tokens: null,
    started_at: "2026-09-07T20:00:00Z",
    completed_at: null,
  };
  const start = frame("thinking/started", data);
  const delta = frame("thinking/delta", {
    ...data,
    text: "Checking",
    estimated_tokens: 50,
  });
  const done = frame("thinking/completed", {
    ...data,
    text: "Checked",
    estimated_tokens: 141,
    completed_at: "2026-09-07T20:00:05Z",
  });
  const late = frame("thinking/delta", {
    ...data,
    text: "stale",
    estimated_tokens: 50,
  });
  const items = threadFeed([start, delta, delta, done, late]);
  assert.equal(items.length, 1);
  assert.equal(items[0].kind, "thinking");
  assert.equal(items[0].frame, start);
  assert.equal(items[0].text, "Checked");
  assert.equal(items[0].tokens, 141);
  assert.equal(items[0].ended, "2026-09-07T20:00:05Z");
  assert.equal(items[0].completed, true);
});

test("thinking completion can render without loaded start; runs remain distinct", () => {
  const { frame } = fixture();
  const d = {
    thinking_id: "r",
    turn_id: "turn",
    text: "Summary",
    estimated_tokens: null,
    started_at: null,
    completed_at: null,
  };
  const done = frame("thinking/completed", d);
  const items = threadFeed([done, frame("thinking/started", d, "other")]);
  assert.equal(items.length, 2);
  assert.equal(items[0].started, null);
  assert.equal(items[0].completed, true);
  assert.equal(items[1].completed, false);
});

test("a terminal turn freezes unfinished thinking and ignores late deltas", () => {
  const { frame } = fixture();
  const d = {
    thinking_id: "r",
    turn_id: "turn",
    text: "Working",
    estimated_tokens: null,
    started_at: "2026-09-07T19:59:59Z",
    completed_at: null,
  };
  const items = threadFeed([
    frame("thinking/started", d),
    frame("turn/completed", { turn_id: "turn", outcome: "interrupted" }),
    frame("thinking/delta", { ...d, text: "late" }),
  ]);
  const item = items.find((i) => i.kind === "thinking");
  assert.equal(item.interrupted, true);
  assert.equal(item.completed, false);
  assert.equal(item.text, "Working");
  assert.equal(item.ended, "2026-09-07T20:00:00Z");
});

test("thinking deltas preserve sections and prefer summaries without duplicating content", () => {
  const { frame } = fixture();
  const d = {
    thinking_id: "r",
    turn_id: "t",
    text: "",
    estimated_tokens: null,
    started_at: null,
    completed_at: null,
  };
  const delta = (text, channel, section_index) =>
    frame("thinking/delta", { ...d, text, channel, section_index });
  const items = threadFeed([
    frame("thinking/started", d),
    delta("Con", "summary", 0),
    delta("sider", "summary", 0),
    delta("Next", "summary", 1),
    delta("Other", "content", 0),
  ]);
  assert.equal(items[0].text, "Consider\n\nNext");
});
