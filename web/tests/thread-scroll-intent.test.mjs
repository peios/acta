import test from "node:test";
import assert from "node:assert/strict";
import { ThreadScrollIntent } from "../src/lib/thread-scroll-intent.js";
test("even a small upward scroll stops following before a queued render runs", () => {
  const s = new ThreadScrollIntent();
  s.reset(1000);
  const queued = s.revision;
  s.input(-1, 1000, 1000);
  assert.equal(s.following, false);
  assert.notEqual(s.revision, queued);
  s.observe(998, 1000);
  assert.equal(s.following, false);
  s.observe(999, 1000);
  assert.equal(s.following, false);
  s.input(1, 999, 1000);
  assert.equal(s.following, true);
});
test("stream growth and our anchor corrections cannot reenable following", () => {
  const s = new ThreadScrollIntent();
  s.reset(1000);
  s.observe(980, 1000);
  s.observe(980, 2000);
  assert.equal(s.following, false);
  s.wrote(1500);
  assert.equal(s.observe(1500, 1500), false);
  assert.equal(s.following, false);
  s.input(1, 1500, 1500);
  assert.equal(s.following, true);
});
test("scrollbar and keyboard movement only reenable when actually back at bottom", () => {
  const s = new ThreadScrollIntent();
  s.reset(1000);
  s.observe(700, 1000);
  s.input(1, 700, 1000);
  s.observe(950, 1000);
  assert.equal(s.following, false);
  s.observe(1000, 1000);
  assert.equal(s.following, true);
  s.input(-1, 1000, 1000);
  assert.equal(s.following, false);
});
test("user movement overrides a pending programmatic scroll event", () => {
  const s = new ThreadScrollIntent();
  s.wrote(1000);
  const queued = s.revision;
  assert.equal(s.observe(975, 1000), true);
  assert.equal(s.following, false);
  assert.notEqual(s.revision, queued);
});
test("programmatic scroll events do not invalidate renders or turn off following", () => {
  const s = new ThreadScrollIntent();
  const revision = s.revision;
  s.wrote(1000);
  assert.equal(s.observe(1000, 1100), false);
  assert.equal(s.revision, revision);
  assert.equal(s.following, true);
});
test("layout clamping and restored lane positions stay paused", () => {
  const s = new ThreadScrollIntent();
  s.reset(900, false);
  s.observe(850, 850);
  assert.equal(s.following, false);
  s.reset(300, false);
  s.wrote(300);
  s.observe(300, 300);
  assert.equal(s.following, false);
  s.reset(0);
  assert.equal(s.following, true);
});
test("End explicitly follows the moving end during a keyboard scroll animation", () => {
  const s = new ThreadScrollIntent();
  s.reset(700, false);
  const pending = s.revision;
  s.toBottom();
  assert.equal(s.following, true);
  assert.notEqual(s.revision, pending);
  s.observe(1000, 1030);
  assert.equal(s.following, true);
});
test("collapsing content at the live end preserves existing following, never restores paused following", () => {
  const s = new ThreadScrollIntent();
  s.reset(900);
  s.observe(700, 700);
  assert.equal(s.following, true);
  s.input(-1, 700, 700);
  s.observe(600, 600);
  assert.equal(s.following, false);
});

test("late downward offsets cannot undo an explicit upward gesture", () => {
  const s = new ThreadScrollIntent();
  s.reset(990);
  s.input(-1, 990, 1000);
  s.observe(1000, 1000); // Previous native motion arrives before the wheel movement.
  assert.equal(s.following, false);
  s.observe(950, 1200);
  s.wrote(1100); // Keep an older-page anchor, without resuming following.
  s.observe(1100, 1200);
  s.observe(1200, 1200);
  assert.equal(s.following, false);
  s.input(1, 1200, 1200);
  assert.equal(s.following, true);
});
test("a new scrollbar gesture can reach the bottom after an upward wheel", () => {
  const s = new ThreadScrollIntent();
  s.reset(1000);
  s.input(-1, 1000, 1000);
  s.observe(400, 1000);
  s.drag();
  s.observe(1000, 1000);
  assert.equal(s.following, true);
});
