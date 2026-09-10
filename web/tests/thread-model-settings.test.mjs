import test from "node:test";
import assert from "node:assert/strict";
import {
  ThreadModelSettings,
  settingsForModel,
} from "../src/lib/thread-model-settings.js";
const model = {
  id: "small",
  name: "Small",
  description: "",
  efforts: [{ id: "low", description: "" }],
  default_effort: "low",
  fast_mode: false,
};
const settings = { model: "small", effort: "low", fast_mode: false };
const fixture = (
  request,
  storage = {
    value: null,
    getItem() {
      return this.value;
    },
    setItem(k, v) {
      this.value = v;
    },
    removeItem() {
      this.value = null;
    },
  },
) => ({
  storage,
  session: new ThreadModelSettings({
    request,
    storage,
    key: "settings",
    runId: "run",
    uuid: () => "request",
    changed: () => {},
  }),
});
test("model switch chooses only supported effort and speed", () => {
  assert.deepEqual(
    settingsForModel(model, { effort: "ultra", fast_mode: true }),
    settings,
  );
});
test("provider acknowledgement waits for a newer configuration frame", async () => {
  const { session } = fixture(async () => ({
    result: { outcome: "accepted" },
  }));
  await session.change(settings, 5);
  assert.equal(session.state.pending.state, "accepted");
  session.observe({ ...settings, effort: "high" }, 5);
  assert.ok(session.state.pending);
  session.observe(settings, 6);
  assert.equal(session.state.pending, null);
});
test("lost response survives reload and reuses immutable settings request", async () => {
  const sent = [];
  const request = async (body) => {
    if (body) {
      sent.push(body);
      throw Error("disconnected");
    }
    return { result: null };
  };
  const { session, storage } = fixture(request);
  await session.change(settings, 5);
  session.close();
  const recovered = fixture(request, storage).session;
  await recovered.submit();
  assert.deepEqual(sent[0], sent[1]);
  assert.equal(recovered.state.pending.state, "uncertain");
});
test("rejection restores the provider selection instead of retaining a false saved state", async () => {
  const { session } = fixture(async () => ({
    result: { outcome: "rejected", error: "unsupported" },
  }));
  await session.change(settings, 5);
  assert.equal(session.state.pending, null);
  assert.equal(session.state.error, "unsupported");
});
test("configuration arriving before a failed HTTP response remains confirmed", async () => {
  let reject;
  const { session } = fixture((body) =>
    body
      ? new Promise((_, r) => (reject = r))
      : Promise.resolve({ result: null }),
  );
  const sending = session.change(settings, 5);
  session.observe(settings, 6);
  reject(Error("late network failure"));
  await sending;
  assert.equal(session.state.pending, null);
  assert.equal(session.state.error, "");
});

test("known HTTP rejection releases controls but a lost response remains uncertain", async () => {
  const { session } = fixture(async () => {
    throw Object.assign(Error("Harness is unavailable"), { status: 409 });
  });
  await session.change(settings, 5);
  assert.equal(session.state.pending, null);
  assert.equal(session.state.error, "Harness is unavailable");
});

test("catalogue cache survives navigation and reload and is isolated per thread/run", async () => {
  const values = new Map();
  const storage = {
    getItem: (key) => values.get(key) ?? null,
    setItem: (key, value) => values.set(key, value),
    removeItem: (key) => values.delete(key),
  };
  const calls = [];
  const create = (thread, run) =>
    new ThreadModelSettings({
      storage,
      key: `pending:${thread}:${run}`,
      catalogueKey: `catalogue:${thread}`,
      runId: run,
      uuid: () => crypto.randomUUID(),
      changed: () => {},
      request: async (body) => {
        if (body) calls.push(body);
        return { result: { outcome: "accepted", models: [model] } };
      },
    });
  const first = create("one", "run");
  await first.load();
  first.close();
  const reopened = create("one", "run");
  assert.deepEqual(reopened.state.models, [model]);
  await reopened.load();
  assert.equal(calls.length, 1);
  const other = create("two", "run");
  assert.deepEqual(other.state.models, []);
  await other.load();
  assert.equal(calls.length, 2);
  const resumed = create("one", "new-run");
  assert.deepEqual(resumed.state.models, []);
  await resumed.load();
  assert.equal(calls.length, 3);
});

test("invalid or expired catalogue cache is disposable", async () => {
  for (const cache of [
    { runId: "run", savedAt: Date.now(), models: [{}] },
    { runId: "run", savedAt: Date.now() - 16 * 60 * 1000, models: [model] },
  ]) {
    const storage = {
      getItem: (key) => (key === "catalogue" ? JSON.stringify(cache) : null),
      setItem: () => {},
      removeItem: () => {},
    };
    let loaded = 0;
    const session = new ThreadModelSettings({
      storage,
      key: "pending",
      catalogueKey: "catalogue",
      runId: "run",
      uuid: () => crypto.randomUUID(),
      changed: () => {},
      request: async (body) => {
        if (body) loaded++;
        return { result: { outcome: "accepted", models: [model] } };
      },
    });
    assert.deepEqual(session.state.models, []);
    await session.load();
    assert.equal(loaded, 1);
    assert.deepEqual(session.state.models, [model]);
  }
});

test("resolved provider settings confirm without mutating the immutable request", async () => {
  const requested = { model: "alias", effort: "", fast_mode: false };
  const confirmed = { model: "native-id", effort: "", fast_mode: false };
  const { session } = fixture(async () => ({
    result: { outcome: "accepted", settings: confirmed },
  }));
  await session.change(requested, 5);
  assert.deepEqual(session.state.pending.settings, requested);
  session.observe({ ...confirmed, effort: null }, 6);
  assert.equal(session.state.pending, null);
});
test("models without effort are accepted and remove incompatible previous choices", async () => {
  const simple = {
    ...model,
    resolved_model: "native-small",
    efforts: [],
    default_effort: "",
  };
  const { session } = fixture(async () => ({
    result: { outcome: "accepted", models: [simple] },
  }));
  await session.load();
  assert.deepEqual(session.state.models, [simple]);
  assert.deepEqual(
    settingsForModel(simple, { effort: "high", fast_mode: true }),
    { model: "small", effort: "", fast_mode: false },
  );
});
