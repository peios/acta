import fs from "node:fs";
import assert from "node:assert/strict";
import { threadFeed } from "./reference/thread-feed.js";
import { visibleFeed } from "./reference/thread-frames.js";
const path = process.argv[2];
if (!path)
  throw Error(
    "Pass the local normalized frame capture path. Run the Go replay test first.",
  );
const frames = JSON.parse(fs.readFileSync(path));
const got = JSON.parse(fs.readFileSync(path + ".items.json"));
const groups = Object.groupBy(frames, (f) => f.thread_id);
const changes = [];
for (const [id, fs] of Object.entries(groups)) {
  const expected = visibleFeed(threadFeed(fs), false),
    actual = got[id]
      .filter((i) => !i.deleted && i.visibility === "normal")
      .map((i) => i.payload);
  assert.equal(actual.length, expected.length, id);
  for (let n = 0; n < expected.length; n++) {
    const a = actual[n],
      e = expected[n];
    assert.equal(a.kind, e.kind, id + ":" + n);
    for (const k of Object.keys(e)) {
      if (["id", "frame", "key"].includes(k)) continue;
      let av = a[k],
        ev = e[k];
      if (k === "argumentsText") {
        try {
          av = JSON.parse(av);
          ev = JSON.parse(ev);
        } catch {}
      }
      try {
        assert.deepEqual(av, ev);
      } catch {
        changes.push({ id, n, kind: e.kind, k, expected: ev, actual: av });
      }
    }
  }
  console.log(id, fs.length, "frames", actual.length, "visible items");
}
fs.writeFileSync(path + ".comparison.json", JSON.stringify(changes, null, 2), {
  mode: 0o600,
});
console.log("Differences:", changes.length);
if (changes.length) process.exitCode = 1;
