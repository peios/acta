import { build } from "vite";
import { svelte } from "@sveltejs/vite-plugin-svelte";
import { mkdtempSync, rmSync, readFileSync } from "node:fs";
import { tmpdir } from "node:os";
import { fileURLToPath } from "node:url";
import { join } from "node:path";
import assert from "node:assert/strict";
const { chromium } = await import(
  process.env.ACTA_PLAYWRIGHT_MODULE || "playwright"
);
const root = fileURLToPath(new URL("../", import.meta.url));
const out = mkdtempSync(join(tmpdir(), "acta-composer-test-"));
await build({
  configFile: false,
  root,
  logLevel: "error",
  plugins: [svelte({ configFile: false })],
  resolve: {
    alias: {
      $lib: join(root, "src/lib"),
      "$app/navigation": join(root, "tests/fixtures/navigation-stub.js"),
    },
  },
  build: {
    outDir: out,
    emptyOutDir: true,
    lib: {
      entry: join(root, "tests/fixtures/thread-composer-entry.js"),
      name: "PropertiesFixture",
      formats: ["iife"],
      fileName: () => "fixture.js",
    },
    cssCodeSplit: false,
  },
});
const browser = await chromium.launch({ headless: true });
try {
  const page = await browser.newPage({ viewport: { width: 900, height: 500 } });
  const errors = [];
  page.on("pageerror", (e) => errors.push(e.message));
  await page.route("http://localhost:8081/**", (route) =>
    route.fulfill({ contentType: "text/html", body: '<div id="app"></div>' }),
  );
  await page.goto("http://localhost:8081/_review/composer-test");
  await page.addStyleTag({
    content: readFileSync(join(out, "acta2-web.css"), "utf8"),
  });
  await page.addScriptTag({
    content: readFileSync(join(out, "fixture.js"), "utf8"),
  });
  const input = page.getByRole("textbox", { name: "Message", exact: true });
  const stop = page.getByRole("button", { name: "Stop turn", exact: true });
  await stop.waitFor();
  // Permissions are live controls, independent of the idle-only model picker.
  await page
    .getByRole("button", { name: "Permission mode", exact: true })
    .click();
  await page.getByRole("button", { name: /^Unrestricted/ }).click();
  assert.equal(
    await page
      .getByRole("button", { name: "Permission mode", exact: true })
      .getAttribute("title"),
    "Unrestricted",
  );
  assert.ok(await stop.isVisible(), "permission change must not end the turn");
  await page
    .getByRole("button", { name: "Toggle connection", exact: true })
    .click();
  await page
    .getByRole("button", { name: "Permission mode", exact: true })
    .click();
  assert.equal(
    await page.getByRole("button", { name: /^Ask for approval/ }).isEnabled(),
    false,
  );
  await page
    .getByRole("button", { name: "Permission mode", exact: true })
    .click();

  assert.equal(
    await page
      .getByRole("button", { name: "Send message", exact: true })
      .count(),
    0,
  );
  await input.fill("While working");
  await page.getByRole("button", { name: "Send message", exact: true }).click();
  assert.equal(await input.inputValue(), "");
  assert.ok(await stop.isVisible(), "sending must not remove Stop");
  assert.equal(await page.locator("output").textContent(), "While working");
  await input.fill("Keyboard send");
  await input.press("Enter");
  assert.equal(
    await page.locator("output").textContent(),
    "While working|Keyboard send",
  );
  assert.ok(await input.evaluate((el) => document.activeElement === el));
  await input.fill("Do not submit on newline");
  await input.press("Shift+Enter");
  assert.equal(
    await page.locator("output").textContent(),
    "While working|Keyboard send",
  );
  await page.setViewportSize({ width: 390, height: 700 });
  assert.ok(await stop.isVisible());
  const sendBox = await page
    .getByRole("button", { name: "Send message", exact: true })
    .boundingBox();
  assert.ok(
    sendBox && sendBox.x >= 0 && sendBox.x + sendBox.width <= 390,
    "mobile send control remains in view",
  );
  await stop.click();
  await stop.waitFor({ state: "hidden" });
  assert.equal(await input.inputValue(), "Do not submit on newline\n");
  assert.deepEqual(errors, []);
  console.log(
    "PASS: mid-turn send and Stop coexist, click and Enter send, draft clears and Shift+Enter inserts newline",
  );
} finally {
  await browser.close();
  rmSync(out, { recursive: true, force: true });
}
