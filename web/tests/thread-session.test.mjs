import test from "node:test";
import assert from "node:assert/strict";
import { ThreadSession } from "../src/lib/thread-session.js";
const deferred = () => {
  let resolve;
  const promise = new Promise((r) => (resolve = r));
  return { promise, resolve };
};
const item = (sequence, revision = sequence, id = String(sequence)) => ({
  id,
  sequence,
  output_index: 1,
  revision,
  visibility: "normal",
  deleted: false,
  payload: {
    id,
    kind: "frame",
    frame: { thread_id: "a", run_id: "r", sequence, kind: "debug/unknown" },
  },
});
const page = (items, revision = 100, more = false, next = "older") => ({
  thread_id: "a",
  items,
  current: { run_id: "r", frames: {} },
  revision,
  cursor_revision: revision,
  has_more: more,
  next,
});
function setup(request) {
  const changes = [];
  let refreshes = 0;
  const session = new ThreadSession({
    id: "a",
    request,
    changed: (s) => changes.push(s),
    refreshThreads: async () => {
      refreshes++;
    },
    uuid: () => "command",
  });
  return { session, changes, refreshes: () => refreshes };
}
test("unchanged polls and replayed rows leave the transcript and singleton untouched", async () => {
  const replies = [page([item(1)]), page([]), page([item(1)])];
  const { session, changes } = setup(async () => replies.shift());
  await session.refreshFrames();
  const items = session.state.items;
  const current = session.state.current;
  const publications = changes.length;
  await session.refreshFrames();
  await session.refreshFrames();
  assert.equal(session.state.items, items);
  assert.equal(session.state.current, current);
  assert.equal(
    changes.length,
    publications,
    "idle polling must not trigger rendering",
  );
  session.close();
});
test("singleton-only updates and recovering from a poll failure still publish", async () => {
  let count = 0;
  const { session, changes } = setup(async () => {
    if (++count === 2) throw Error("temporary failure");
    return {
      ...page(count === 1 ? [item(1)] : [], count < 4 ? 100 : 101),
      current: { run_id: count < 4 ? "r" : "new" },
    };
  });
  await session.refreshFrames();
  const items = session.state.items;
  await session.refreshFrames();
  assert.equal(changes.at(-1).error, "temporary failure");
  await session.refreshFrames();
  assert.equal(changes.at(-1).error, "");
  await session.refreshFrames();
  assert.equal(changes.at(-1).current.run_id, "new");
  assert.equal(session.state.items, items);
  session.close();
});
test("initial load asks for newest items; older pages prepend and updates retain first position", async () => {
  const paths = [];
  const replies = [
    page([item(51), item(52)], 100, true),
    page([item(51, 101)], 101),
    page([item(49), item(50)], 101, false),
  ];
  const { session } = setup(async (path) => {
    paths.push(path);
    return replies.shift();
  });
  await session.refreshFrames();
  assert.equal(paths[0], "threads/a/conversation?lane=&debug=false");
  await session.refreshFrames();
  assert.equal(paths[1], "threads/a/conversation?lane=&debug=false&after=100");
  assert.equal(session.state.items[0].revision, 101);
  await session.loadOlder();
  assert.match(paths[2], /before=older/);
  assert.deepEqual(
    session.state.items.map((i) => i.sequence),
    [49, 50, 51, 52],
  );
  assert.equal(session.state.hasMore, false);
  session.close();
});
test("delayed older page cannot overwrite newer items or singleton", async () => {
  const older = deferred();
  let requests = 0;
  const { session } = setup((path) => {
    if (path.includes("before=")) return older.promise;
    return Promise.resolve(
      ++requests === 1
        ? page([item(50)], 100, true)
        : { ...page([item(49, 110)], 110), current: { run_id: "new" } },
    );
  });
  await session.refreshFrames();
  const wait = session.loadOlder();
  await session.refreshFrames();
  assert.equal(session.state.items.length, 1); // offscreen change is a revision guard, not a history hole
  older.resolve(page([item(49, 90)], 100, false));
  await wait;
  assert.equal(session.state.items[0].revision, 110);
  assert.equal(session.state.current.run_id, "new");
  assert.equal(session.after, 110);
  session.close();
});
test("deleted items disappear and replay cannot resurrect them", async () => {
  const replies = [
    page([item(1)]),
    page([{ ...item(1, 101), deleted: true }], 101),
    page([item(1, 100)], 101),
  ];
  const { session } = setup(async () => replies.shift());
  await session.refreshFrames();
  await session.refreshFrames();
  assert.equal(session.state.items.length, 0);
  await session.refreshFrames();
  assert.equal(session.state.items.length, 0);
  session.close();
});
test("closed route ignores late pages and lifecycle acknowledgements", async () => {
  for (const op of ["refreshFrames", "control"]) {
    const waiting = deferred();
    let signal;
    const { session, changes, refreshes } = setup((p, b, s) => {
      signal = s;
      return waiting.promise;
    });
    const work =
      op === "control" ? session.control("kill") : session.refreshFrames();
    session.close();
    const n = changes.length;
    assert.equal(signal.aborted, true);
    waiting.resolve(page([item(1)]));
    await work;
    assert.equal(changes.length, n);
    assert.equal(session.state.items.length, 0);
    assert.equal(refreshes(), 0);
  }
});
test("wrong thread cannot consume a cursor or partially publish", async () => {
  const { session } = setup(async () =>
    page([item(1), { ...item(2), payload: { frame: { thread_id: "wrong" } } }]),
  );
  await session.refreshFrames();
  assert.equal(session.after, 0);
  assert.equal(session.records.size, 0);
  assert.match(session.state.error, /different thread/);
  session.close();
});
test("debug-mode change invalidates in-flight history responses", async () => {
  const waiting = deferred();
  let count = 0;
  const { session } = setup(() =>
    ++count === 1 ? waiting.promise : Promise.resolve(page([item(2)])),
  );
  const first = session.refreshFrames();
  session.setDebug(true);
  waiting.resolve(page([item(1)]));
  await first;
  await session.refreshFrames();
  assert.deepEqual(
    session.state.items.map((i) => i.id),
    ["2"],
  );
  session.close();
});
test("lifecycle submission and result polling serialize independently", async () => {
  const submission = deferred(),
    result = deferred();
  const calls = [];
  const { session } = setup((path, body) => {
    calls.push({ path, body });
    return body ? submission.promise : result.promise;
  });
  const first = session.control("kill");
  await session.control("resume");
  assert.equal(calls.length, 1);
  submission.resolve({});
  await first;
  assert.equal(session.state.pending.action, "kill");
  const poll = session.pollControl();
  await session.pollControl();
  assert.equal(calls.length, 2);
  result.resolve({ result: { error: "" } });
  await poll;
  assert.equal(session.state.pending, null);
  session.close();
});

test("interrupt targets the current run and exposes provider rejection", async () => {
  const calls = [];
  const { session } = setup(async (path, body) => {
    calls.push({ path, body });
    return body
      ? { id: body.id }
      : { result: { outcome: "rejected", error: "No active turn" } };
  });
  await session.control("interrupt", "run-a");
  assert.deepEqual(calls[0].body, {
    id: "command",
    action: "interrupt",
    run_id: "run-a",
  });
  assert.equal(session.state.pending.action, "interrupt");
  await session.pollControl();
  assert.equal(session.state.pending, null);
  assert.equal(session.state.controlError, "No active turn");
  session.close();
});

test("lane switching isolates history and ignores an in-flight page from the old lane", async () => {
  const old = deferred();
  let requests = 0;
  const { session } = setup(async (path) => {
    requests++;
    if (requests === 1) return page([item(1)], 10, true);
    if (requests === 2) return old.promise;
    const lane = path.includes("lane=child") ? "child" : "";
    const i = { ...item(lane ? 20 : 2), lane_id: lane };
    return { ...page([i], 30), lane_id: lane };
  });
  await session.refreshFrames();
  const pending = session.refreshFrames();
  session.setLane("child");
  old.resolve(page([item(3)], 20));
  await pending;
  await new Promise((r) => setImmediate(r));
  assert.deepEqual(
    session.state.items.map((i) => i.sequence),
    [20],
  );
  session.setLane("");
  assert.deepEqual(
    session.state.items.map((i) => i.sequence),
    [1],
  );
  await new Promise((r) => setImmediate(r));
  assert.deepEqual(
    session.state.items.map((i) => i.sequence),
    [1, 2],
  );
  session.close();
});
test("a wrong lane page is rejected before advancing the cursor", async () => {
  const { session } = setup(async () => ({
    ...page([item(1)]),
    lane_id: "wrong",
  }));
  await session.refreshFrames();
  assert.match(session.state.error, /invalid conversation/);
  assert.equal(session.after, 0);
  assert.equal(session.state.items.length, 0);
  session.close();
});

test("older pages and debug reloads stay scoped when switching lanes", async () => {
  const delayed = deferred();
  const { session } = setup(async (path) => {
    if (path.includes("before=")) return delayed.promise;
    const lane = path.includes("lane=child") ? "child" : "";
    const row = { ...item(lane ? 80 : 50), lane_id: lane };
    return { ...page([row], 100, true), lane_id: lane };
  });
  await session.refreshFrames();
  const older = session.loadOlder();
  session.setLane("child");
  await new Promise((r) => setImmediate(r));
  session.setDebug(true);
  await new Promise((r) => setImmediate(r));
  delayed.resolve(page([item(40)], 90, false));
  await older;
  assert.deepEqual(
    session.state.items.map((i) => i.sequence),
    [80],
  );
  assert.equal(session.state.hasMore, true);
  session.setLane("");
  await new Promise((r) => setImmediate(r));
  assert.deepEqual(
    session.state.items.map((i) => i.sequence),
    [50],
  );
  session.close();
});

test("server projection upgrade invalidates every cached lane", async () => {
  let version = 4;
  const { session } = setup(async (path) => {
    const lane = path.includes("lane=child") ? "child" : "";
    return {
      ...page([{ ...item(10, 10, `${version}-${lane}`), lane_id: lane }], 10),
      lane_id: lane,
      projection_version: version,
    };
  });
  await session.refreshFrames();
  session.setLane("child");
  await new Promise((r) => setImmediate(r));
  version = 5;
  await session.refreshFrames();
  await new Promise((r) => setImmediate(r));
  assert.deepEqual(
    session.state.items.map((i) => i.id),
    ["5-child"],
  );
  session.setLane("");
  assert.equal(session.state.items.length, 0);
  await new Promise((r) => setImmediate(r));
  assert.deepEqual(
    session.state.items.map((i) => i.id),
    ["5-"],
  );
  session.close();
});

test("rename targets the main thread from a child lane and waits for its receipt", async () => {
  const calls = [];
  const { session, refreshes } = setup(async (path, body) => {
    calls.push({ path, body });
    return path.endsWith("control/command")
      ? { result: {} }
      : { id: "command" };
  });
  session.lane = "child";
  await session.control("rename", undefined, "New title");
  assert.deepEqual(calls[0].body, {
    id: "command",
    action: "rename",
    name: "New title",
  });
  assert.equal(session.state.pending.action, "rename");
  await session.pollControl();
  assert.equal(session.state.pending, null);
  assert.equal(refreshes(), 2);
  session.close();
});
