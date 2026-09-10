import test from "node:test";
import assert from "node:assert/strict";
import { LatestRequest, abortableDelay } from "../src/lib/requests.js";
import { createTaskFeed } from "../src/lib/task-feed.js";
const turn = () => new Promise((resolve) => setImmediate(resolve));
function fixture(options = {}) {
  const pending = [],
    configs = [],
    errors = [];
  const feed = createTaskFeed({
    workspace: "w",
    request: (path, body, { signal }) =>
      new Promise((resolve, reject) =>
        pending.push({ path, signal, resolve, reject }),
      ),
    onConfig: (value, changed) => configs.push({ value, changed }),
    onError: (e) => errors.push(e),
    ...options,
  });
  return { feed, pending, configs, errors };
}
test("replacing and disposing reads aborts transport and blocks stale callbacks", () => {
  const gate = new LatestRequest();
  const first = gate.begin(),
    second = gate.begin();
  assert.equal(first.current(), false);
  assert.equal(first.signal.aborted, true);
  assert.equal(second.current(), true);
  gate.dispose();
  assert.equal(second.current(), false);
  assert.equal(gate.begin().current(), false);
});
test("stale long poll cannot regress a newer focus refresh", async () => {
  const f = fixture();
  f.pending[0].resolve({ revision: 3 });
  await turn();
  assert.match(f.pending[1].path, /after=3$/);
  const refresh = f.feed.refresh();
  f.pending[2].resolve({ revision: 5 });
  await refresh;
  f.pending[1].resolve({ revision: 4 });
  await turn();
  assert.deepEqual(
    f.configs.map((x) => x.value.revision),
    [3, 5],
  );
  assert.match(f.pending[3].path, /after=5$/);
  f.feed.stop();
  f.pending[3].resolve({ revision: 6 });
  await turn();
  assert.equal(f.configs.length, 2);
  assert.equal(f.pending[3].signal.aborted, true);
});
test("offline polling retains the last revision and resumes from it", async () => {
  let resume;
  const f = fixture({
    wait: () =>
      new Promise((r) => {
        resume = r;
      }),
  });
  f.pending[0].resolve({ revision: 9 });
  await turn();
  f.pending[1].reject(new Error("offline"));
  await turn();
  assert.equal(f.configs.at(-1).value.revision, 9);
  assert.equal(f.errors.at(-1).message, "offline");
  resume();
  await turn();
  assert.match(f.pending[2].path, /after=9$/);
  f.pending[2].resolve({ revision: 10 });
  await turn();
  assert.equal(f.errors.at(-1), null);
  f.feed.stop();
});
test("access loss stops retries and signals the workspace boundary", async () => {
  let lost = 0;
  const f = fixture({ onAccessLost: () => lost++ });
  f.pending[0].reject(Object.assign(new Error("revoked"), { status: 403 }));
  await turn();
  assert.equal(lost, 1);
  assert.equal(f.pending.length, 1);
  await f.feed.refresh();
  assert.equal(f.pending.length, 1);
});
test("unmount interrupts a retry delay immediately", async () => {
  const abort = new AbortController();
  const delay = abortableDelay(60_000, abort.signal);
  abort.abort();
  await delay;
});
