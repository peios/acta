import { test } from "node:test";
import assert from "node:assert/strict";
import { ThreadSender } from "../src/lib/thread-sending.js";
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
  },
) => {
  let id = 0;
  const sender = new ThreadSender({
    request,
    storage,
    key: "test",
    uuid: () => String(++id),
    changed: () => {},
  });
  return { sender, storage };
};
test("unknown delivery retries the same identity and survives reload", async () => {
  const calls = [];
  const request = async (body) => {
    if (body) {
      calls.push(body);
      throw Error("lost response");
    }
    return { result: null };
  };
  const { sender, storage } = fixture(request);
  sender.draft("hello");
  await sender.send("run", 4);
  assert.equal(sender.state.pending.state, "uncertain");
  sender.close();
  const restored = fixture(request, storage).sender;
  await restored.submit();
  assert.deepEqual(calls[0], calls[1]);
  assert.equal(restored.state.draft, "");
});
test("matching text does not acknowledge another message, matching ID does", async () => {
  const { sender } = fixture(async () => ({ result: null }));
  sender.draft("same");
  await sender.send("run", 1);
  sender.observe([
    {
      kind: "message/user",
      data: {
        state: "in_progress",
        submission_id: "other",
        content: [{ type: "text", text: "same" }],
      },
    },
  ]);
  assert.ok(sender.state.pending);
  sender.observe([
    {
      kind: "message/user",
      data: { state: "in_progress", submission_id: "1" },
    },
  ]);
  assert.equal(sender.state.pending, null);
  assert.equal(sender.state.draft, "");
});
test("only confirmed rejection permits a fresh submission", async () => {
  const calls = [];
  const { sender } = fixture(async (body) => {
    if (body) calls.push(body);
    return { result: { outcome: "rejected", error: "busy" } };
  });
  sender.draft("hello");
  await sender.send("run", 0);
  await sender.send("run", 1);
  assert.equal(calls.length, 2);
  assert.notEqual(calls[0].id, calls[1].id);
});
test("a late HTTP error cannot recreate a provider-confirmed bubble", async () => {
  let reject;
  const { sender } = fixture((body) =>
    body
      ? new Promise((_, r) => (reject = r))
      : Promise.resolve({ result: null }),
  );
  sender.draft("hi");
  const sending = sender.send("run", 0);
  sender.observe([
    { kind: "message/user", data: { state: "completed", submission_id: "1" } },
  ]);
  reject(Error("connection lost"));
  await sending;
  assert.equal(sender.state.pending, null);
});
test("storage failure prevents a network submission and preserves the draft", async () => {
  let calls = 0;
  const { sender } = fixture(
    async () => {
      calls++;
    },
    {
      getItem() {
        return null;
      },
      setItem() {
        throw Error("quota");
      },
    },
  );
  sender.draft("preserve");
  await sender.send("run", 0);
  assert.equal(calls, 0);
  assert.equal(sender.state.draft, "preserve");
  assert.equal(sender.state.pending, null);
});

test("scheduled and post-submit polling cannot overlap", async () => {
  let resolve,
    calls = 0;
  const waiting = new Promise((r) => (resolve = r));
  const { sender } = fixture(async () => {
    calls++;
    return waiting;
  });
  sender.state.pending = {
    id: "id",
    run_id: "run",
    text: "hello",
    after: 0,
    state: "uncertain",
    error: "",
  };
  const poll = sender.poll();
  await sender.poll();
  assert.equal(calls, 1);
  resolve({ result: { outcome: "accepted" } });
  await poll;
  assert.equal(sender.state.pending.state, "accepted");
  sender.close();
});

test("an off-page provider echo releases a restored outbox without resending", async () => {
  let response = { result: { outcome: "accepted" } };
  let posts = 0;
  const request = async (body) => {
    if (body) posts++;
    return response;
  };
  const first = fixture(request);
  first.sender.draft("older message");
  await first.sender.send("run", 0);
  first.sender.close();
  const restored = fixture(request, first.storage).sender;
  restored.observe([
    { kind: "message/assistant", data: { text: "latest page" } },
  ]);
  await restored.poll();
  assert.equal(restored.state.pending.state, "accepted");
  restored.draft("new unsent draft");
  response = { message_confirmed: true, result: null };
  await restored.poll();
  assert.equal(restored.state.pending, null);
  assert.equal(restored.state.draft, "new unsent draft");
  assert.equal(JSON.parse(first.storage.value).pending, null);
  assert.equal(posts, 1);
});

test("late off-page confirmation cannot publish after leaving the route", async () => {
  let resolve;
  const { sender } = fixture(() => new Promise((r) => (resolve = r)));
  sender.state.pending = {
    id: "id",
    run_id: "run",
    text: "hello",
    after: 0,
    state: "uncertain",
    error: "",
  };
  const pending = sender.poll();
  sender.close();
  resolve({ message_confirmed: true, result: null });
  await pending;
  assert.ok(sender.state.pending);
});
test("image-only sends hydrate durable references and preserve command identity on retry", async () => {
  const calls = [];
  const ref = {
    id: "image",
    name: "pixel.png",
    size: 3,
    media_type: "image/png",
  };
  const { sender, storage } = fixture(async (body) => {
    if (body) calls.push(body);
    return { result: null };
  });
  sender.options.loadImage = async () => ({
    name: ref.name,
    media_type: ref.media_type,
    base64: "YWJj",
  });
  sender.images([ref]);
  await sender.send("run", 1);
  assert.equal(calls[0].images[0].base64, "YWJj");
  assert.ok(!storage.value.includes("YWJj"));
  sender.state.pending.state = "uncertain";
  await sender.submit();
  assert.deepEqual(calls[0], calls[1]);
  sender.confirm();
  assert.equal(sender.state.images.length, 0);
});
test("missing local image is rejected before any network submission", async () => {
  let calls = 0;
  const { sender } = fixture(async () => {
    calls++;
    return {};
  });
  sender.options.loadImage = async () => {
    throw Error("Image missing");
  };
  sender.images([
    { id: "image", name: "pixel.png", size: 3, media_type: "image/png" },
  ]);
  await sender.send("run", 0);
  assert.equal(calls, 0);
  assert.equal(sender.state.pending.state, "rejected");
  assert.equal(sender.state.images.length, 1);
});
test("confirmed send does not discard a different image draft", async () => {
  const { sender } = fixture(async () => ({ result: null }));
  sender.options.loadImage = async () => ({});
  const image = (id) => ({ id, name: id, size: 3, media_type: "image/png" });
  sender.images([image("first")]);
  await sender.send("run", 0);
  sender.images([image("second")]);
  sender.confirm();
  assert.deepEqual(
    sender.state.images.map((i) => i.id),
    ["second"],
  );
});
test("adding a second reactive image never puts UI proxies into the outbox", () => {
  const { sender } = fixture(async () => ({}));
  const first = new Proxy(
    { id: "a", name: "a", size: 1, media_type: "image/png" },
    {},
  );
  sender.images([first]);
  assert.doesNotThrow(() =>
    sender.images([
      first,
      new Proxy({ id: "b", name: "b", size: 1, media_type: "image/png" }, {}),
    ]),
  );
  assert.equal(sender.state.images.length, 2);
});

for (const nextDraft of [null, "new draft", "hello", ""]) {
  test(`send clears immediately; rejection respects subsequent input ${JSON.stringify(nextDraft)}`, async () => {
    let reject;
    const { sender, storage } = fixture((body) =>
      body
        ? new Promise((_, r) => {
            reject = r;
          })
        : Promise.resolve({ result: null }),
    );
    sender.draft("hello");
    const sending = sender.send("run", 0);
    assert.equal(sender.state.draft, "");
    assert.equal(sender.state.pending.text, "hello");
    assert.equal(sender.state.pending.state, "sending");
    assert.equal(JSON.parse(storage.value).draft, "");
    if (nextDraft !== null) {
      sender.draft("typing");
      sender.draft(nextDraft);
    }
    reject(Object.assign(Error("rejected"), { status: 409 }));
    await sending;
    assert.equal(sender.state.draft, nextDraft ?? "hello");
    sender.confirm();
    assert.equal(sender.state.draft, nextDraft ?? "");
  });
}
test("rejection after reload does not restore over a draft typed and erased", async () => {
  let response = { result: null };
  const request = async () => response;
  const { sender, storage } = fixture(request);
  sender.draft("hello");
  await sender.send("run", 0);
  sender.draft("new");
  sender.draft("");
  sender.close();
  const restored = fixture(request, storage).sender;
  response = { result: { outcome: "rejected", error: "busy" } };
  await restored.poll();
  assert.equal(restored.state.draft, "");
});
