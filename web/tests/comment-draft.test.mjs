import { test } from "node:test";
import assert from "node:assert/strict";
import {
  emptyCommentDraft,
  restoreCommentDraft,
} from "../src/lib/comment-draft.js";
test("an uncertain post retains the exact request across reload or composer movement", () => {
  const pending = {
    body: "Original submission",
    reply_to: "reply",
    request_id: "same-request",
  };
  const restored = restoreCommentDraft(
    JSON.stringify({ body: "Original submission", pending }),
  );
  assert.deepEqual(restored.pending, pending);
  assert.equal(restored.open, true);
  assert.equal(restored.body, "Original submission");
  assert.equal(restored.loaded, true);
});
test("bad browser draft data cannot become a malformed retry", () => {
  assert.deepEqual(restoreCommentDraft("{broken"), {
    ...emptyCommentDraft(),
    loaded: true,
  });
  const restored = restoreCommentDraft(
    JSON.stringify({ body: "Keep my text", pending: { body: 17 } }),
  );
  assert.equal(restored.body, "Keep my text");
  assert.equal(restored.pending, null);
});
