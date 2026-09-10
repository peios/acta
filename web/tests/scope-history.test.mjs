import test from "node:test";
import assert from "node:assert/strict";
import { ScopeHistory } from "../src/lib/scope-history.js";
const fixture = () => {
  const paths = new Map();
  const storage = {
    getItem: (k) => paths.get(k) ?? null,
    setItem: (k, v) => paths.set(k, v),
  };
  return { storage, history: new ScopeHistory(() => storage) };
};
test("scope switching restores the exact last location independently", () => {
  const { storage, history } = fixture();
  history.remember("a", "/my-agents/thread-id");
  history.remember("a", "/workspaces/acta?task=task-id#details");
  assert.equal(history.destination("a", "agents"), "/my-agents/thread-id");
  assert.equal(
    history.destination("a", "workspace"),
    "/workspaces/acta?task=task-id#details",
  );
  history.remember("a", "/user-settings/profile");
  const reload = new ScopeHistory(() => storage);
  assert.equal(
    reload.destination("a", "workspace"),
    "/workspaces/acta?task=task-id#details",
  );
  assert.equal(reload.destination("b", "agents"), "/my-agents");
  history.remember("a", "/my-agents");
  assert.equal(history.destination("a", "agents"), "/my-agents");
});
test("malformed, external and cross-scope storage destinations are ignored", () => {
  const { storage, history } = fixture();
  for (const path of [
    "https://example.org",
    "//example.org",
    "/\\example.org",
    "/my-agents/../../login",
    "/my-agents-other",
    "/workspaces/acta",
    "/my-agents/\nfoo",
  ]) {
    storage.setItem("acta.scope-location:a:agents", path);
    assert.equal(history.destination("a", "agents"), "/my-agents");
  }
});
test("blocked browser storage still remembers destinations during the session", () => {
  const history = new ScopeHistory(() => {
    throw Error("blocked");
  });
  history.remember("a", "/workspaces/acta/settings/details");
  history.remember("a", "/my-agents/thread-id");
  assert.equal(
    history.destination("a", "workspace"),
    "/workspaces/acta/settings/details",
  );
});
