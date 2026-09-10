import test from "node:test";
import assert from "node:assert/strict";
import { MarkdownManager } from "@tiptap/markdown";
import { taskExtensions, needsSourceMode } from "../src/lib/task-markdown.js";
const manager = new MarkdownManager({ extensions: taskExtensions() });
test("supported rich/Markdown formatting and stable references round-trip semantically", () => {
  const input =
    '# Heading\n\n**Bold**, *italic*, ~~struck~~ and `code`.\n\n[ACT-151](/tasks/8887271d-47e8-4645-bb4f-517aac18aa6b) [@jack](/references/accounts/8887271d-47e8-4645-bb4f-517aac18aa6b)\n\n> A quote\n\n- a list\n  - nested\n\n1. Ordered\n2. List\n\n- [x] Done\n- [ ] To do\n\n```go\nfmt.Println("hello")\n```\n\n| A | B |\n|---|---|\n| 1 | 2 |';
  const first = manager.parse(input);
  const markdown = manager.serialize(first);
  assert.deepEqual(manager.parse(markdown), first);
  assert.match(markdown, /\/tasks\/8887271d/);
  assert.match(markdown, /\/references\/accounts\/8887271d/);
});

test("unsupported content is retained in source mode, while code stays editable", () => {
  assert.equal(needsSourceMode("![image](https://example.org/a.png)"), true);
  assert.equal(needsSourceMode("<div>HTML</div>"), true);
  assert.equal(needsSourceMode("```html\n<div>code</div>\n```"), false);
});
