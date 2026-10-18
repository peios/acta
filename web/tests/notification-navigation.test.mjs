import test from "node:test";
import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import vm from "node:vm";

const source = readFileSync(
  new URL("../static/service-worker.js", import.meta.url),
  "utf8",
);
async function openNotification(url, existing = false) {
  const events = new Map();
  const opened = [];
  let pending;
  const client = {
    url: "https://acta.example/workspaces",
    focus: async () => {},
    navigate: async (value) => opened.push(value),
  };
  vm.runInNewContext(source, {
    URL,
    self: {
      location: { origin: "https://acta.example" },
      addEventListener: (name, handler) => events.set(name, handler),
      clients: {
        matchAll: async () => (existing ? [client] : []),
        openWindow: async (value) => opened.push(value),
      },
    },
  });
  events.get("notificationclick")({
    notification: { data: { url }, close() {} },
    waitUntil: (promise) => {
      pending = promise;
    },
  });
  await pending;
  return opened;
}
test("task alerts open their workspace and task in a new or existing window", async () => {
  for (const existing of [false, true]) {
    assert.deepEqual(
      await openNotification("/workspaces/review?task=task-id", existing),
      ["https://acta.example/workspaces/review?task=task-id"],
    );
  }
  assert.deepEqual(await openNotification(undefined), [
    "https://acta.example/workspaces",
  ]);
});
test("notification navigation rejects external and non-workspace destinations", async () => {
  for (const url of [
    "https://evil.example/workspaces",
    "//evil.example/workspaces",
    "/site-settings/users",
    "/workspaces/../../login",
    "/workspaces/a/settings",
  ]) {
    assert.deepEqual(await openNotification(url), []);
  }
});
