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
const mobile = process.env.ACTA_MOBILE_AUDIT === "1";
const root = fileURLToPath(new URL("../", import.meta.url)),
  out = mkdtempSync(join(tmpdir(), "acta-search-"));
await build({
  configFile: false,
  root,
  logLevel: "error",
  plugins: [svelte({ configFile: false })],
  resolve: { alias: { $lib: join(root, "src/lib") } },
  build: {
    outDir: out,
    emptyOutDir: true,
    lib: {
      entry: join(root, "tests/fixtures/task-search-entry.js"),
      name: "SearchReview",
      formats: ["iife"],
      fileName: () => "fixture.js",
      cssFileName: "fixture",
    },
    cssCodeSplit: false,
  },
});
const browser = await { chromium, firefox, webkit }[
  process.env.ACTA_SEARCH_BROWSER || "chromium"
].launch({
  headless: true,
  executablePath: process.env.ACTA_MOBILE_EXECUTABLE || undefined,
});
try {
  const context = await browser.newContext({
    viewport: { width: mobile ? 390 : 1100, height: 800 },
    hasTouch: mobile,
  });
  let fail = false,
    queries = [];
  const errors = [];
  const result = (id, source = "title") => ({
    id,
    reference: "ACT-" + id,
    title: "Improve task discovery",
    status: { id: "status", name: "In progress" },
    workspace_id: "acta",
    workspace_name: "Acta",
    workspace_slug: "acta",
    ancestors: [{ id: "p", reference: "ACT-1", title: "Production readiness" }],
    source,
    comment_id: source === "comment" ? "comment-id" : "",
    excerpt: [
      { text: "A <script>window.bad=true</script> " },
      { text: "search", match: true },
      { text: " should find nested tasks." },
    ],
  });
  await context.route("http://localhost:8081/**", async (route) => {
    const url = new URL(route.request().url());
    const json = (v, status = 200) =>
      route.fulfill({
        status,
        contentType: "application/json",
        body: JSON.stringify(v),
      });
    if (url.pathname === "/api/workspaces")
      return json({
        workspaces: [
          { id: "acta", name: "Acta" },
          { id: "peios", name: "Peios" },
          { id: "research", name: "Research and development" },
          { id: "personal", name: "Personal" },
        ],
        more: false,
      });
    if (url.pathname === "/api/tasks/search") {
      queries.push(url.searchParams);
      if (fail)
        return json(
          { error: { message: "Search is temporarily unavailable" } },
          503,
        );
      if (url.searchParams.get("q") === "empty")
        return json({ tasks: [], more: false, cursor: "" });
      return json(
        url.searchParams.get("cursor")
          ? { tasks: [result("3")], more: false, cursor: "" }
          : {
              tasks: [result("1"), result("2", "comment")],
              more: true,
              cursor: "next",
            },
      );
    }
    if (url.pathname === "/api/tasks/task/comments/root/replies") {
      const older = !!url.searchParams.get("cursor");
      await new Promise((resolve) => setTimeout(resolve, 40));
      return json({
        entries: [
          {
            id: older ? "old-reply" : "new-reply",
            first_event: older ? "2" : "3",
            last_event: older ? "2" : "3",
            actor: { id: "user", username: "Reviewer", display_name: null },
            kind: "comment",
            before: {},
            after: {},
            count: 1,
            unread: false,
            started_at: "2026-09-01T00:00:00Z",
            updated_at: "2026-09-01T00:00:00Z",
            comment: {
              body: older ? "The matching older reply" : "Newest reply",
              version: 1,
              deleted: false,
              edited: false,
              can_edit: false,
              can_delete: false,
            },
          },
        ],
        more: !older,
        cursor: older ? "" : "older",
        unread: false,
      });
    }
    if (url.pathname === "/fixture.js")
      return route.fulfill({
        contentType: "text/javascript; charset=utf-8",
        body: readFileSync(join(out, "fixture.js")),
      });
    if (url.pathname === "/fixture.css")
      return route.fulfill({
        contentType: "text/css",
        body: readFileSync(join(out, "fixture.css")),
      });
    return route.fulfill({
      contentType: "text/html",
      body: '<html lang="en"><head><title>Acta mobile audit</title><meta charset="utf-8"><link rel="stylesheet" href="/fixture.css"></head><body><div id="app"></div><script src="/fixture.js"></script></body></html>',
    });
  });
  const page = await context.newPage();
  page.on("pageerror", (e) => errors.push(String(e)));
  await page.goto("http://localhost:8081" + (mobile ? "?mobile-audit" : ""));
  // Model a task card behind the backdrop. It must stay inert for the entire
  // dismissal tap, and only a subsequent independent tap may activate it.
  await page.evaluate(() => {
    const button = document.createElement("button");
    button.id = "under-search";
    button.textContent = "Underlying task";
    button.style.cssText =
      "position:fixed;bottom:12px;left:40px;width:200px;height:44px";
    window.underSearchClicks = 0;
    button.onclick = () => window.underSearchClicks++;
    document.body.append(button);
    document.addEventListener(
      "pointerup",
      () => {
        window.searchOpenAtRelease = !!document.querySelector(
          "dialog.task-search[open]",
        );
      },
      { capture: true, once: true },
    );
  });
  await page.evaluate(() => window.review.open());
  const underneath = await page.locator("#under-search").boundingBox();
  const tap = async () => {
    const x = underneath.x + underneath.width / 2,
      y = underneath.y + underneath.height / 2;
    if (mobile) await page.touchscreen.tap(x, y);
    else await page.mouse.click(x, y);
  };
  await tap();
  assert.equal(
    await page.evaluate(() => window.searchOpenAtRelease),
    true,
    "search remains modal until release, preventing touch click-through",
  );
  assert.equal(await page.evaluate(() => window.underSearchClicks), 0);
  if (mobile) {
    await page
      .getByRole("dialog", { name: "Search tasks", exact: true })
      .waitFor();
    await page.getByRole("button", { name: "Close search", exact: true }).tap();
  }
  await page
    .getByRole("dialog", { name: "Search tasks", exact: true })
    .waitFor({ state: "hidden" });
  await tap();
  assert.equal(await page.evaluate(() => window.underSearchClicks), 1);
  await page.locator("#under-search").evaluate((el) => el.remove());
  await page.getByRole("button", { name: "Search tasks", exact: true }).click();
  const input = page.getByRole("combobox", { name: "Search tasks" });
  await input.fill("search");
  await page.getByRole("option").nth(1).waitFor();
  assert.equal(await page.getByRole("option").count(), 2);
  assert.equal(await page.evaluate(() => window.bad), undefined);
  await page.screenshot({ path: "/tmp/acta98-search.png" });
  const selectedIndex = async () =>
    (await input.getAttribute("aria-activedescendant")).split("-").at(-1);
  await input.press("Control+p");
  assert.equal(await selectedIndex(), "1");
  await input.press("Control+n");
  assert.equal(await selectedIndex(), "0");
  assert.equal(await input.inputValue(), "search");
  await input.press("ArrowDown");
  await input.press("Enter");
  await page.waitForFunction(
    () =>
      document.querySelector("[data-testid=selected]").textContent ===
      "2:comment-id",
  );
  assert.equal(await page.getByRole("dialog").count(), 0);
  await page.getByRole("button", { name: "Search tasks", exact: true }).click();
  await page.getByRole("option").nth(1).waitFor();
  await page.getByRole("button", { name: "Load more results" }).click();
  await page.getByRole("option").nth(2).waitFor();
  assert.equal(queries.at(-1).get("cursor"), "next");
  if (mobile) {
    const pills = page.getByRole("group", {
      name: "Search workspace",
      exact: true,
    });
    await pills.getByRole("button", { name: "Peios", exact: true }).tap();
    assert.equal(
      await pills
        .getByRole("button", { name: "Peios", exact: true })
        .getAttribute("aria-pressed"),
      "true",
    );
    assert.equal(await page.getByLabel("Include archived").isVisible(), false);
    assert.equal(
      await input.evaluate((el) => document.activeElement === el),
      true,
    );
    const bar = await pills.boundingBox();
    assert.ok(
      await pills.evaluate((el) => el.scrollWidth > el.clientWidth),
      "workspace pills scroll horizontally",
    );
    const results = await page.locator(".search-body").boundingBox();
    assert.ok(bar.y < results.y && bar.y + bar.height <= results.y);
  } else {
    await page
      .getByRole("button", { name: "Search workspace: All workspaces" })
      .click();
    await page.getByRole("option", { name: "Peios", exact: true }).click();
  }
  await page.waitForFunction(
    () => document.querySelectorAll(".results>[role=option]").length === 2,
  );
  assert.equal(queries.at(-1).get("workspace"), "peios");
  assert.equal(queries.at(-1).get("cursor"), "");
  await input.fill("empty");
  await page
    .getByText("No matching tasks. Try different words or another workspace.")
    .waitFor();
  fail = true;
  await input.fill("failure");
  await page.getByRole("alert").waitFor();
  fail = false;
  await page.getByRole("button", { name: "Try again" }).click();
  await page.getByRole("option").nth(1).waitFor();
  await page.setViewportSize({ width: 390, height: 780 });
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
      .getByRole("dialog", { name: "Search tasks", exact: true })
      .boundingBox();
    assert.ok(
      bounds.y >= 0 && bounds.y + bounds.height <= 380,
      `dialog exceeds keyboard viewport: ${JSON.stringify(bounds)}`,
    );
    assert.equal(bounds.height, 380);
    assert.equal(bounds.width, 390);
    const field = await input.boundingBox();
    const body = await page.locator(".search-body").boundingBox();
    const close = await page
      .getByRole("button", { name: "Close search", exact: true })
      .boundingBox();
    assert.ok(
      body.y + body.height <= field.y,
      "results are above the search input",
    );
    assert.ok(
      field.y > 280 && field.y + field.height <= 380,
      "input stays above keyboard",
    );
    assert.ok(close.width >= 44 && close.height >= 44, "close touch target");
    await page.evaluate(() => {
      delete visualViewport.height;
      visualViewport.dispatchEvent(new Event("resize"));
    });
    await page.waitForFunction(
      () =>
        Math.abs(
          document.querySelector("dialog.task-search").getBoundingClientRect()
            .height - visualViewport.height,
        ) < 1,
    );
  }
  await page.screenshot({ path: "/tmp/acta98-search-mobile.png" });
  assert.ok(
    await page.evaluate(
      () =>
        document.querySelector("dialog").getBoundingClientRect().right <=
        innerWidth,
    ),
  );
  await input.press("Escape");
  assert.equal(await page.getByRole("dialog").count(), 0);
  await page.getByRole("button", { name: "Open matching reply" }).click();
  await page.getByText("The matching older reply", { exact: true }).waitFor();
  await page.waitForFunction(
    () => document.activeElement?.getAttribute("data-read-id") === "old-reply",
  );
  await checkAccessibility(page, "task-search");
  assert.deepEqual(errors, []);
  console.log(
    "PASS: search results, escaped highlights, keyboard comment selection, pagination, workspace reset, empty/error recovery, mobile fit, Escape and older reply focus",
  );
} finally {
  await browser.close();
  rmSync(out, { recursive: true, force: true });
}
