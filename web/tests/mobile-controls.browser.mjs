import { checkAccessibility } from "./mobile-accessibility.mjs";
import { build } from "vite";
import { svelte } from "@sveltejs/vite-plugin-svelte";
import { mkdtempSync, rmSync, readFileSync } from "node:fs";
import { tmpdir } from "node:os";
import { fileURLToPath } from "node:url";
import { join } from "node:path";
import assert from "node:assert/strict";
const { chromium, firefox, webkit } = await import(
  process.env.ACTA_PLAYWRIGHT_MODULE || "playwright"
);
const root = fileURLToPath(new URL("../", import.meta.url));
const out = mkdtempSync(join(tmpdir(), "acta-mobile-"));
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
      entry: join(root, "tests/fixtures/mobile-controls-entry.js"),
      name: "MobileFixture",
      formats: ["iife"],
      fileName: () => "fixture.js",
      cssFileName: "fixture",
    },
    cssCodeSplit: false,
  },
});
const engine = process.env.ACTA_MOBILE_BROWSER || "chromium";
const browser = await { chromium, firefox, webkit }[engine].launch({
  headless: true,
  executablePath: process.env.ACTA_MOBILE_EXECUTABLE || undefined,
});
try {
  const page = await browser.newPage({
    viewport: { width: 390, height: 844 },
    hasTouch: true,
    ...(engine !== "firefox" ? { isMobile: true } : {}),
  });
  page.setDefaultTimeout(6000);
  const errors = [];
  page.on("pageerror", (e) => errors.push(e.message));
  let task = {
    id: "audit",
    reference: "QA-1",
    title: "Mobile task",
    description: "Initial description",
    versions: { title: 1, description: 1 },
  };
  let failure = false,
    posts = [];
  await page.route("http://localhost:8081/**", async (route) => {
    const request = route.request(),
      path = new URL(request.url()).pathname;
    if (path === "/api/tasks/audit") {
      if (request.method() === "GET") return route.fulfill({ json: task });
      const body = request.postDataJSON();
      assert.equal(body.version, task.versions[body.field]);
      task = {
        ...task,
        [body.field]: body.value,
        versions: { ...task.versions, [body.field]: body.version + 1 },
      };
      return route.fulfill({ json: task });
    }
    if (path.endsWith("task-people"))
      return route.fulfill({
        json: {
          people: [
            {
              id: "person",
              username: "reviewer",
              display_name: "Mobile Reviewer",
              available: true,
              agent: false,
              owner_id: "",
              sources: [],
            },
          ],
        },
      });
    if (path.includes("/threads/audit/control"))
      return route.fulfill({
        json: {
          result: {
            outcome: "accepted",
            models: [
              {
                id: "small",
                name: "Small model",
                description: "Fast review model",
                efforts: [
                  { id: "low", description: "Brief" },
                  { id: "high", description: "Thorough" },
                ],
                default_effort: "low",
                fast_mode: true,
              },
            ],
          },
        },
      });
    if (path.endsWith("/comments")) {
      posts.push(request.postDataJSON());
      if (failure) return route.abort("internetdisconnected");
      return route.fulfill(
        failure
          ? {
              status: 503,
              json: { error: { message: "Temporarily disconnected" } },
            }
          : { json: { id: "comment" } },
      );
    }
    if (path === "/fixture.js")
      return route.fulfill({
        contentType: "text/javascript; charset=utf-8",
        body: readFileSync(join(out, "fixture.js")),
      });
    if (path === "/fixture.css")
      return route.fulfill({
        contentType: "text/css",
        body: readFileSync(join(out, "fixture.css")),
      });
    return route.fulfill({
      contentType: "text/html",
      body: '<html lang="en"><head><title>Acta mobile audit</title><meta charset="utf-8"><meta name="viewport" content="width=device-width, initial-scale=1"><link rel="stylesheet" href="/fixture.css"></head><body><div id="app"></div><script src="/fixture.js"></script></body></html>',
    });
  });
  async function viewport(height) {
    await page.evaluate((height) => {
      if (height === null) delete visualViewport.height;
      else
        Object.defineProperty(visualViewport, "height", {
          configurable: true,
          value: height,
        });
      visualViewport.dispatchEvent(new Event("resize"));
    }, height);
    await page.waitForTimeout(100);
  }
  async function fits(locator, height) {
    const visible = await page.evaluate(() => ({
      width: innerWidth,
      height: visualViewport.height,
    }));
    height ??= visible.height;
    const b = await locator.boundingBox();
    assert.ok(
      b &&
        b.x >= 0 &&
        b.y >= 0 &&
        b.x + b.width <= visible.width &&
        b.y + b.height <= height + 1,
      JSON.stringify(b),
    );
  }
  await page.goto("http://localhost:8081");
  await page
    .getByRole("textbox", { name: "Title", exact: true })
    .fill("Renamed on a phone");
  await page.waitForTimeout(800);
  assert.equal(task.title, "Renamed on a phone");
  await page
    .getByRole("button", { name: "Edit description", exact: true })
    .tap();
  await page
    .getByRole("textbox", { name: "Task description", exact: true })
    .fill("A description edited on mobile");
  await page.waitForTimeout(800);
  assert.equal(task.description, "A description edited on mobile");
  // Comments: real rich editor, source mode, draft recovery, idempotent failed post.
  await page
    .getByRole("button", { name: "Write a comment…", exact: true })
    .tap();
  await page.getByRole("button", { name: "Markdown", exact: true }).tap();
  await page
    .getByRole("textbox", { name: "Comment Markdown", exact: true })
    .fill("A mobile **comment**");
  await page.reload();

  assert.equal(
    await page
      .getByRole("textbox", { name: "Comment Markdown", exact: true })
      .inputValue(),
    "A mobile **comment**",
  );
  failure = true;
  await page.getByRole("button", { name: "Post", exact: true }).tap();
  await page.getByRole("alert").waitFor();
  assert.equal(
    await page
      .getByRole("textbox", { name: "Comment Markdown", exact: true })
      .inputValue(),
    "A mobile **comment**",
  );
  failure = false;
  await page.getByRole("button", { name: "Retry post", exact: true }).tap();
  await page.getByText("Comment posted", { exact: true }).waitFor();
  assert.deepEqual(posts[0], posts[1]);
  await page
    .getByRole("button", { name: "Write a comment…", exact: true })
    .tap();
  await page.getByRole("button", { name: "Rich text", exact: true }).tap();
  const rich = page.getByRole("textbox", { name: "Comment", exact: true });
  await rich.fill("A rich mobile comment");
  // Establish a DOM selection: mobile WebKit ignores desktop Select All shortcuts.
  // The subsequent toolbar interaction is a browser touch tap.
  await rich.evaluate((element) => {
    element.focus();
    const range = document.createRange();
    range.selectNodeContents(element);
    const selection = getSelection();
    selection.removeAllRanges();
    selection.addRange(range);
    document.dispatchEvent(new Event("selectionchange"));
  });
  await page.waitForTimeout(100);
  assert.equal(
    await page.evaluate(() => getSelection()?.toString()),
    "A rich mobile comment",
    "formatting test selects the full comment",
  );
  await page.getByRole("button", { name: "Bold", exact: true }).tap();
  await page.getByRole("button", { name: "Post", exact: true }).tap();
  await page
    .getByRole("button", { name: "Write a comment…", exact: true })
    .waitFor();
  assert.equal(posts.at(-1).body, "**A rich mobile comment**");

  await page.getByRole("button", { name: "Filter", exact: true }).tap();
  const filter = page.getByRole("dialog", {
    name: "Filter tasks",
    exact: true,
  });
  await checkAccessibility(page, "filter dialog");
  await filter.getByRole("button", { name: "Urgent", exact: true }).tap();
  await filter
    .getByLabel("Search assignees to filter", { exact: true })
    .fill("review");
  const filterTarget = await filter
    .getByText("Unassigned", { exact: true })
    .locator("..")
    .boundingBox();
  assert.ok(
    filterTarget.height >= 44,
    `filter touch target ${filterTarget.height}px`,
  );
  await viewport(380);
  await fits(filter, 380);
  await filter.getByRole("button", { name: "Done", exact: true }).tap();
  await viewport(null);
  await page
    .getByRole("button", { name: "Change assignees", exact: true })
    .tap();
  const assign = page.getByRole("dialog", { name: "Assign task", exact: true });
  await assign.getByLabel("Search assignees", { exact: true }).fill("review");
  await assign.getByText("Mobile Reviewer", { exact: true }).waitFor();
  await viewport(380);
  await fits(assign, 380);
  await assign.getByText("Mobile Reviewer", { exact: true }).tap();
  await page.keyboard.press("Escape");
  await viewport(null);
  // Composer popups remain reachable above the keyboard, including their last action.
  await page.getByRole("button", { name: "Model settings", exact: true }).tap();
  const models = page.getByRole("dialog", {
    name: "Model settings",
    exact: true,
  });
  await models.getByText("Small model", { exact: true }).waitFor();
  const slider = await models.getByRole("slider").boundingBox();
  assert.ok(slider.height >= 44, `effort touch target ${slider.height}px`);
  await viewport(380);
  await fits(models, 380);
  await checkAccessibility(page, "model popup");
  await page.keyboard.press("Escape");
  await viewport(null);
  await page
    .getByRole("button", { name: "Request approval", exact: true })
    .tap();
  const popup = page.locator(".approval-popup:popover-open");
  await popup.waitFor();
  await viewport(380);
  await fits(popup, 380);
  await checkAccessibility(page, "approval/question popup");
  await page.screenshot({ path: `/tmp/acta-audit-popup-${engine}.png` });
  await popup.getByRole("button", { name: "Approve", exact: true }).tap();
  assert.equal(await page.locator("output").textContent(), "approve{}");
  await viewport(null);
  await page.getByRole("button", { name: "Ask question", exact: true }).tap();
  await popup.waitFor();
  await popup
    .getByRole("textbox", { name: /Custom answer/ })
    .fill("Use my mobile answer");
  await viewport(380);
  await fits(popup, 380);
  await checkAccessibility(page, "approval/question popup");
  await popup.getByRole("button", { name: "Answer", exact: true }).tap();
  assert.match(
    await page.locator("output").textContent(),
    /Use my mobile answer/,
  );
  await viewport(null);
  await page
    .getByRole("button", { name: "Request approval", exact: true })
    .tap();
  await page.keyboard.press("Escape");
  await page
    .getByRole("button", { name: "Toggle connection", exact: true })
    .tap();
  await page.getByRole("button", { name: "Needs approval", exact: true }).tap();
  assert.equal(
    await popup
      .getByRole("button", { name: "Approve", exact: true })
      .isEnabled(),
    false,
  );
  await page.keyboard.press("Escape");
  await page
    .getByRole("button", { name: "Toggle connection", exact: true })
    .tap();
  await page.getByRole("button", { name: "Needs approval", exact: true }).tap();
  assert.equal(
    await popup
      .getByRole("button", { name: "Approve", exact: true })
      .isEnabled(),
    true,
  );
  await popup.getByRole("button", { name: "Deny", exact: true }).tap();
  // Long URLs, code and tables must stay inside the phone's content area.
  await page.setViewportSize({ width: 320, height: 844 });
  await page
    .getByRole("button", { name: "Edit description", exact: true })
    .tap();
  await page.getByRole("button", { name: "Markdown", exact: true }).tap();
  const longDescription =
    "# Long content\n\nhttps://example.test/" +
    "a".repeat(240) +
    "\n\n```text\n" +
    "b".repeat(240) +
    "\n```\n\n| A | B | C |\n| --- | --- | --- |\n| " +
    "c".repeat(100) +
    " | value | value |";
  await page
    .getByRole("textbox", { name: "Task description Markdown", exact: true })
    .fill(longDescription);
  await page.waitForTimeout(800);
  assert.equal(task.description, longDescription);
  await page.getByRole("button", { name: "Model settings", exact: true }).tap();
  await page.keyboard.press("Escape");
  assert.ok(
    await page
      .locator("main")
      .evaluate((e) => e.scrollWidth <= e.clientWidth + 1),
    "long rendered content does not widen the page",
  );
  for (const width of [320, 390, 720, 844]) {
    await page.setViewportSize({ width, height: width === 844 ? 390 : 844 });
    await page
      .getByRole("button", { name: "Model settings", exact: true })
      .tap();
    await fits(models);
    await page.keyboard.press("Escape");
    assert.ok(
      await page.evaluate(
        () => document.documentElement.scrollWidth <= innerWidth,
      ),
    );
  }
  await page.screenshot({ path: `/tmp/acta-audit-controls-${engine}.png` });
  await checkAccessibility(page, "mobile-controls");
  assert.deepEqual(errors, []);
  console.log(
    `PASS ${engine}: comment editing/draft/retry, filters/assignees, model/approval/question keyboard bounds, disconnect/reconnect controls, rotation fit`,
  );
} finally {
  await browser.close();
  rmSync(out, { recursive: true, force: true });
}
