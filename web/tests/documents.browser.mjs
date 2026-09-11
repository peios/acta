import { checkAccessibility } from "./mobile-accessibility.mjs";
import { build } from "vite";
import { svelte } from "@sveltejs/vite-plugin-svelte";
import { mkdtempSync, rmSync, readFileSync, readdirSync } from "node:fs";
import { tmpdir } from "node:os";
import { fileURLToPath } from "node:url";
import { join } from "node:path";
import assert from "node:assert/strict";
const { chromium, firefox, webkit } = await import(
  process.env.ACTA_PLAYWRIGHT_MODULE || "playwright"
);
const mobile = process.env.ACTA_MOBILE_AUDIT === "1";
const root = fileURLToPath(new URL("../", import.meta.url));
const out = mkdtempSync(join(tmpdir(), "acta-documents-test-"));
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
      entry: join(root, "tests/fixtures/task-documents-entry.js"),
      name: "DocumentsFixture",
      formats: ["iife"],
      fileName: () => "fixture.js",
    },
    cssCodeSplit: false,
  },
});
const browser = await { chromium, firefox, webkit }[
  process.env.ACTA_MOBILE_BROWSER || "chromium"
].launch({
  headless: true,
  executablePath:
    process.env.ACTA_MOBILE_EXECUTABLE ||
    process.env.ACTA_CHROMIUM_EXECUTABLE ||
    undefined,
});
try {
  const page = await browser.newPage({
    viewport: { width: mobile ? 390 : 1100, height: 820 },
    hasTouch: mobile,
  });
  const errors = [];
  page.on("pageerror", (e) => errors.push(e.message));
  const version = (revision) => ({
    id: "doc",
    document_id: "doc",
    file_id: `file-${revision}`,
    task_id: "review",
    revision,
    title: "Review brief",
    filename: "brief.md",
    media_type: "text/markdown; charset=utf-8",
    size: 30,
    can_write: true,
    created_at: new Date().toISOString(),
  });
  let docs = [],
    uploads = 0;
  await page.route("http://localhost:8081/**", async (route) => {
    const req = route.request(),
      u = new URL(req.url());
    if (!u.pathname.startsWith("/api/"))
      return route.fulfill({
        contentType: "text/html",
        body: '<!doctype html><html lang="en"><head><title>Acta documents audit</title></head><body><div id="app"></div></body></html>',
      });
    if (u.pathname === "/api/tasks/review/documents") {
      if (req.method() === "POST") {
        assert.match(req.headers()["content-type"], /^multipart\/form-data/);
        uploads++;
        docs = [version(uploads)];
        return route.fulfill({ json: docs[0] });
      }
      return route.fulfill({ json: { documents: docs } });
    }
    if (u.pathname === "/api/documents/doc/versions")
      return route.fulfill({
        json: {
          versions: Array.from({ length: uploads }, (_, i) =>
            version(uploads - i),
          ),
        },
      });
    if (u.pathname.endsWith("/file"))
      return route.fulfill({
        contentType: "text/markdown",
        body: u.pathname.includes("/2/")
          ? "# Revised brief\nSecond version."
          : "# Initial brief\nFirst version.",
      });
    if (u.pathname.endsWith("/delete")) {
      assert.equal(req.postDataJSON().revision, 2);
      docs = [];
      return route.fulfill({ json: { deleted: true } });
    }
    throw new Error(`Unexpected request ${u.pathname}`);
  });
  await page.goto(
    "http://localhost:8081/_review/documents" + (mobile ? "?mobile-audit" : ""),
  );
  await page.addStyleTag({
    content:
      readFileSync(
        join(
          out,
          readdirSync(out).find((f) => f.endsWith(".css")),
        ),
        "utf8",
      ) + `body{margin:${mobile ? 12 : 40}px}`,
  });
  await page.evaluate(() => {
    document.documentElement.dataset.theme = "dark";
  });
  await page.addScriptTag({ path: join(out, "fixture.js") });
  await page
    .getByRole("button", { name: "Upload document", exact: true })
    .click();
  const upload = page.getByRole("dialog", {
    name: "Upload document",
    exact: true,
  });
  if (mobile) {
    await page.evaluate(() => {
      Object.defineProperty(visualViewport, "height", {
        configurable: true,
        value: 380,
      });
      visualViewport.dispatchEvent(new Event("resize"));
    });
    await page.waitForTimeout(100);
    const bounds = await page
      .getByRole("dialog", { name: "Upload document", exact: true })
      .boundingBox();
    assert.ok(
      bounds.y >= 0 && bounds.y + bounds.height <= 380,
      `dialog exceeds keyboard viewport: ${JSON.stringify(bounds)}`,
    );
    await page.evaluate(() => {
      delete visualViewport.height;
      visualViewport.dispatchEvent(new Event("resize"));
    });
  }
  await upload.locator("input[type=file]").setInputFiles({
    name: "brief.md",
    mimeType: "text/markdown",
    buffer: Buffer.from("# Initial brief"),
  });
  await upload.getByLabel("Title").fill("Review brief");
  await upload.getByRole("button", { name: "Upload", exact: true }).click();
  await page.getByRole("button", { name: "Open Review brief" }).click();
  await page
    .getByRole("heading", { name: "Initial brief", exact: true })
    .waitFor();
  await page.getByRole("button", { name: "Close document preview" }).click();
  await page
    .getByRole("button", { name: "Upload new version of Review brief" })
    .click();
  const replace = page.getByRole("dialog", {
    name: "Upload new version",
    exact: true,
  });
  await replace.locator("input[type=file]").setInputFiles({
    name: "brief.md",
    mimeType: "text/markdown",
    buffer: Buffer.from("# Revised brief"),
  });
  await replace.getByRole("button", { name: "Save new version" }).click();
  await page.getByRole("button", { name: "Open Review brief" }).click();
  await page
    .getByRole("heading", { name: "Revised brief", exact: true })
    .waitFor();
  await page.getByRole("button", { name: /Version 1/ }).click();
  await page
    .getByRole("heading", { name: "Initial brief", exact: true })
    .waitFor();
  await page.screenshot({
    path: join(root, "../bin/acta96-documents-desktop.png"),
  });
  await page.setViewportSize({ width: 390, height: 844 });
  assert.equal(
    await page.evaluate(
      () => document.documentElement.scrollWidth <= innerWidth,
    ),
    true,
  );
  await page.screenshot({
    path: join(root, "../bin/acta96-documents-mobile.png"),
  });
  await page.getByRole("button", { name: "Close document preview" }).click();
  await page
    .getByRole("button", { name: "Delete Review brief", exact: true })
    .click();
  await page
    .getByRole("dialog", { name: "Delete document", exact: true })
    .getByRole("button", { name: "Delete document", exact: true })
    .click();
  await page.getByText("No documents yet", { exact: true }).waitFor();
  await checkAccessibility(page, "documents");
  assert.deepEqual(errors, []);
  console.log(
    "PASS documents: multipart uploads, versions, Markdown preview, history, deletion and mobile layout",
  );
} finally {
  await browser.close();
  rmSync(out, { recursive: true, force: true });
}
