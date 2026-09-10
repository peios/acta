import { test } from "node:test";
import assert from "node:assert/strict";
import {
  validateImageFiles,
  MAX_IMAGE_BYTES,
  imageRefs,
} from "../src/lib/thread-image-input.js";
test("image picker enforces aggregate count, size and raster formats", () => {
  const png = { size: 1024, type: "image/png" };
  assert.doesNotThrow(() => validateImageFiles([png]));
  assert.throws(() => validateImageFiles(Array(5).fill(png)));
  assert.throws(() => validateImageFiles([{ ...png, type: "image/svg+xml" }]));
  assert.throws(() =>
    validateImageFiles(
      [{ ...png, size: MAX_IMAGE_BYTES }],
      [{ id: "a", name: "a", size: 1, media_type: "image/png" }],
    ),
  );
  assert.deepEqual(imageRefs([{}, null]), []);
});
