import test from "node:test";
import assert from "node:assert/strict";
import { diffLines, fileChanges } from "../src/lib/thread-diffs.js";
test("unified diff headers and content remain distinct and text is preserved", () => {
  const text =
    "diff --git a/a b/a\n--- a/a\n+++ b/a\n@@ -1 +1 @@\n-old\n+<script>new</script>\n same\n\\ No newline at end of file";
  const lines = diffLines(text);
  assert.equal(lines.map((l) => l.text).join(""), text);
  assert.deepEqual(
    lines.map((l) => l.kind),
    [
      "header",
      "header",
      "header",
      "hunk",
      "deletion",
      "addition",
      "context",
      "context",
    ],
  );
});
test("whole-file contents are coloured without interpreting patch-looking source", () => {
  const text = "+++ ordinary source\n\n@@ source with no final newline";
  for (const [format, kind] of [
    ["added_content", "addition"],
    ["removed_content", "deletion"],
  ]) {
    const lines = diffLines(text, format);
    assert.equal(lines.map((l) => l.text).join(""), text);
    assert.ok(lines.every((l) => l.kind === kind));
  }
  assert.deepEqual(diffLines(""), []);
  assert.deepEqual(fileChanges(null), []);
});
