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
const out = mkdtempSync(join(tmpdir(), "acta-notifications-"));
await build({
  configFile: false,
  root,
  logLevel: "error",
  plugins: [svelte({ configFile: false })],
  resolve: {
    alias: {
      $lib: join(root, "src/lib"),
      "$app/navigation": join(
        root,
        "tests/fixtures/notification-navigation.js",
      ),
    },
  },
  build: {
    outDir: out,
    emptyOutDir: true,
    lib: {
      entry: join(root, "tests/fixtures/notification-entry.js"),
      name: "NotificationReview",
      formats: ["iife"],
      fileName: () => "fixture.js",
    },
    cssCodeSplit: false,
  },
});
const browser = await chromium.launch({ headless: true });
try {
  const context = await browser.newContext({
    viewport: { width: 1100, height: 800 },
  });
  let following = true,
    failFollow = false;
  let records = [],
    failRead = false,
    failGet = false;
  const errors = [];
  const notice = (id, rev = 1, lane = "") => ({
    id,
    revision: rev,
    thread_id: "thread",
    run_id: "run",
    lane_id: lane,
    sequence: rev,
    kind: "attention",
    title: "Approval needed",
    thread_name: "Code review",
    created_at: new Date().toISOString(),
  });
  records = [notice("initial")];
  await context.route("http://localhost:8081/**", async (route) => {
    const req = route.request(),
      url = new URL(req.url());
    if (url.pathname === "/api/tasks/review-task/following") {
      if (req.method() === "POST") {
        if (failFollow)
          return route.fulfill({
            status: 503,
            json: { error: { message: "Follow update failed" } },
          });
        following = req.postDataJSON().following;
      }
      return route.fulfill({ json: { following } });
    }
    if (url.pathname === "/api/push")
      return route.fulfill({ json: { public_key: btoa("key") } });
    if (url.pathname === "/api/push/subscribe")
      return route.fulfill({ json: { id: "subscription" } });
    if (url.pathname === "/api/push/unsubscribe")
      return route.fulfill({ json: { ok: true } });
    if (url.pathname === "/api/notifications/read") {
      if (failRead)
        return route.fulfill({
          status: 503,
          json: { error: { message: "Try again" } },
        });
      const reads = req.postDataJSON().items;
      records = records.filter(
        (n) => !reads.some((r) => r.id === n.id && r.revision === n.revision),
      );
      return route.fulfill({ json: { ok: true } });
    }
    if (url.pathname === "/api/notifications") {
      if (failGet)
        return route.fulfill({
          status: 503,
          json: { error: { message: "Offline" } },
        });
      const counts = {};
      for (const n of records)
        counts[n.task_id || n.thread_id] =
          (counts[n.task_id || n.thread_id] || 0) + 1;
      return route.fulfill({
        json: { items: records, counts, total: records.length },
      });
    }
    return route.fulfill({
      contentType: "text/html",
      body: '<div id="app"></div>',
    });
  });
  await context.addInitScript(() => {
    window.focused = true;
    document.hasFocus = () => window.focused;
    window.Notification = class {
      static permission = "default";
      static async requestPermission() {
        this.permission = "granted";
        return "granted";
      }
    };
    window.PushManager = class {};
    const worker = {
      postMessage(message, ports) {
        ports[0].postMessage({ ok: true });
      },
    };
    let sub = null;
    const reg = {
      active: worker,
      pushManager: {
        getSubscription: async () => sub,
        subscribe: async (options) => {
          sub = {
            endpoint: "https://push.example/test",
            options,
            toJSON: () => ({
              endpoint: "https://push.example/test",
              keys: { p256dh: "key", auth: "secret" },
            }),
            unsubscribe: async () => {
              sub = null;
              return true;
            },
          };
          return sub;
        },
      },
    };
    Object.defineProperty(navigator, "serviceWorker", {
      value: { register: async () => reg, ready: Promise.resolve(reg) },
    });
  });
  const mount = async () => {
    const page = await context.newPage();
    page.on("pageerror", (e) => errors.push(e.message));
    await page.goto("http://localhost:8081/_review/notifications");
    await page.addStyleTag({
      content: readFileSync(join(out, "acta-web.css"), "utf8"),
    });
    await page.addScriptTag({
      content: readFileSync(join(out, "fixture.js"), "utf8"),
    });
    await page.waitForFunction(() => !!window.review?.inbox.items.length);
    return page;
  };
  const page = await mount();
  await page
    .getByRole("button", { name: "Unfollow task", exact: true })
    .waitFor();
  await page
    .getByRole("button", { name: "Unfollow task", exact: true })
    .click();
  await page
    .getByRole("button", { name: "Follow task", exact: true })
    .waitFor();
  assert.equal(following, false);
  failFollow = true;
  await page.getByRole("button", { name: "Follow task", exact: true }).click();
  await page.getByText("Follow update failed", { exact: false }).waitFor();
  assert.equal(following, false, "failed update must preserve follow state");
  failFollow = false;
  await page.getByRole("button", { name: "Follow task", exact: true }).click();
  await page
    .getByRole("button", { name: "Unfollow task", exact: true })
    .waitFor();
  const bell = page.getByRole("button", { name: /^Notifications/ });
  await bell.click();
  await page.getByRole("button", { name: "Enable push notifications" }).click();
  records.push(notice("new", 2));
  await page.evaluate(() => window.review.refresh());
  await page.waitForFunction(() => window.review.enabled);
  await page.evaluate(() => window.review.refresh());

  // Seen only once its frames are loaded; no browser interruption while viewing.
  await page.evaluate(
    () =>
      (window.review.viewing = {
        thread: "thread",
        lane: "",
        sequence: 2,
        ready: true,
      }),
  );
  records.push(notice("visible", 3));
  await page.evaluate(() => window.review.refresh());
  assert.deepEqual(
    records.map((n) => n.id),
    ["visible"],
  );

  // A later frame is not read until it is actually loaded.
  await page.evaluate(() => (window.review.viewing.sequence = 3));
  await page.evaluate(() => window.review.readViewed());
  await page.waitForFunction(() => window.review.inbox.total === 0);
  await page.evaluate(() => (window.review.viewing = null));
  records.push(notice("lane", 4, "child/lane"));
  await page.evaluate(() => window.review.refresh());
  await page.getByRole("button", { name: /Approval needed.*Subagent/ }).click();
  await page.waitForFunction(() => window.review.inbox.total === 0);
  assert.equal(
    await page.evaluate(() => window.reviewURL),
    "/my-agents/thread?lane=child%2Flane",
  );
  // Failed acknowledgement must preserve the unread card and show a retryable error.
  records.push(notice("failure", 5));
  await page.evaluate(() => window.review.refresh());
  failRead = true;
  await bell.click();
  await page.getByRole("button", { name: "Mark all read" }).click();
  await page.getByRole("alert").waitFor();
  assert.equal(records.length, 1);
  assert.equal(await page.evaluate(() => window.review.inbox.total), 1);
  failRead = false;
  await page.getByRole("button", { name: "Mark all read" }).click();
  await page.waitForFunction(() => window.review.inbox.total === 0);
  // Old revision read cannot erase a newer state for the same event.
  records.push(notice("corrected", 6));
  await page.evaluate(() => window.review.refresh());
  records = [
    { ...notice("corrected", 7), kind: "failed", title: "Turn failed" },
  ];
  await page.getByRole("button", { name: "Mark all read" }).click();
  await page.waitForFunction(() => !window.review.reading);
  await page.evaluate(() => window.review.refresh());
  assert.equal(records.length, 1);
  await page.getByText("Turn failed", { exact: true }).waitFor();
  records.push({
    ...notice("task", 8),
    thread_id: "",
    kind: "task",
    title: "jack/helper · Mentioned you",
    task_id: "task-uuid",
    task_reference: "ACT-97",
    task_title: "Task notifications",
    workspace_slug: "workspace-review",
  });
  await page.evaluate(() => window.review.refresh());
  await page.getByRole("button", { name: /Mentioned you.*ACT-97/ }).waitFor();
  await page.screenshot({ path: "/tmp/acta97-task-notifications.png" });
  await page.getByRole("button", { name: /Mentioned you.*ACT-97/ }).click();
  await page.waitForFunction(
    () => window.reviewURL === "/workspaces/workspace-review?task=task-uuid",
  );
  await page.waitForFunction(() => !window.review.reading);
  assert.equal(
    await page.evaluate(() => window.review.inbox.counts["task-uuid"]),
    0,
  );
  await bell.click();
  failGet = true;
  await page.evaluate(() => window.review.refresh());
  assert.equal(await page.evaluate(() => window.review.inbox.total), 1);
  failGet = false;
  await page.screenshot({ path: "/tmp/acta92-notifications-desktop.png" });
  await page.setViewportSize({ width: 390, height: 740 });
  await bell.click();
  await bell.click();
  const popup = page.getByRole("region", { name: "Notifications" });
  await popup.waitFor();
  const bounds = await popup.boundingBox();
  assert.ok(bounds.x >= 0 && bounds.x + bounds.width <= 390 && bounds.y >= 0);
  await page.screenshot({ path: "/tmp/acta92-notifications-mobile.png" });
  assert.deepEqual(errors, []);
  console.log(
    "PASS: persisted unread UI, permission opt-in, suppression, lane links, acknowledgement races, failure recovery, push subscription opt-in, responsive popup",
  );
} finally {
  await browser.close();
  rmSync(out, { recursive: true, force: true });
}
