import test from "node:test";
import assert from "node:assert/strict";
import { pdfAttachments } from "../src/lib/thread-attachments.js";

test("PDF attachment URL preserves bytes and has the correct content type", async () => {
  const bytes = "%PDF-1.4\nQA fixture\n";
  const files = pdfAttachments([
    { name: "sample.pdf", media_type: "application/pdf", base64: btoa(bytes) },
  ]);
  assert.equal(files.length, 1);
  const file = files[0];
  try {
    assert.equal(file.name, "sample.pdf");
    assert.equal(file.size, bytes.length);
    const response = await fetch(file.url);
    assert.equal(response.headers.get("content-type"), "application/pdf");
    assert.equal(await response.text(), bytes);
  } finally {
    URL.revokeObjectURL(file.url);
  }
  await assert.rejects(fetch(file.url));
});

test("unsupported or malformed attachments never become executable URLs", () => {
  for (const value of [
    null,
    {},
    [
      {
        name: "x",
        media_type: "text/html",
        base64: btoa("<script>bad</script>"),
      },
    ],
    [{ name: "x", media_type: "application/pdf", base64: "!broken" }],
  ]) {
    assert.deepEqual(pdfAttachments(value), []);
  }
});
