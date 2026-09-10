import assert from "node:assert/strict";
import {
  ThreadPermissions,
  approvalItems,
} from "../src/lib/thread-permissions.js";
const blank = () => ({
  results: {},
  errors: {},
  busy: {},
  checked: {},
  modePending: "",
  modeError: "",
});
const frame = {
  thread_id: "thread",
  run_id: "run",
  sequence: 1,
  output_index: 1,
  kind: "approval/request",
  data: {
    approval_id: "request",
    title: "Write",
    details: {},
    turn_id: "turn",
  },
};
const live = { run_id: "run", state: "running", connection_id: "connected" };
let state = blank();
state.checked.request = true;
assert.equal(approvalItems([frame, frame], state, live).length, 1);
assert.equal(approvalItems([frame], state, live)[0].available, true);
assert.equal(
  approvalItems([frame], state, { ...live, run_id: "new" })[0].status,
  "unavailable",
);
assert.equal(
  approvalItems(
    [frame, { ...frame, sequence: 2, kind: "approval/resolved" }],
    state,
    live,
  )[0].available,
  false,
);
state.results.request = { outcome: "accepted", decision: "deny" };
assert.equal(approvalItems([frame], state, live)[0].status, "denied");
const storage = new Map();
const saved = {
  getItem: (k) => storage.get(k),
  setItem: (k, v) => storage.set(k, v),
  removeItem: (k) => storage.delete(k),
};
let release;
let result = null;
const session = new ThreadPermissions({
  runId: "run",
  uuid: () => "mode-command",
  storage: saved,
  key: "test",
  changed: (s) => (state = s),
  request: async () => ({ action: "approval", decision: "approve", result }),
});
session.observe([frame]);
await session.poll();
assert.equal(approvalItems([frame], state, live)[0].busy, true);
result = { outcome: "accepted", decision: "approve" };
await session.poll();
assert.equal(approvalItems([frame], state, live)[0].status, "approved");
session.close();
let calls = 0;
const s2 = new ThreadPermissions({
  runId: "run",
  uuid: () => "mode-command",
  storage: saved,
  key: "second",
  changed: (s) => (state = s),
  request: async (body) => {
    if (body) {
      calls++;
      await new Promise((r) => (release = r));
      return {};
    }
    throw Object.assign(new Error("not found"), { status: 404 });
  },
});
s2.observe([frame]);
await s2.poll();
const item = approvalItems([frame], state, live)[0];
const promise = s2.answer(item, "approve");
await s2.answer(item, "deny");
assert.equal(calls, 1);
s2.close();
release();
await promise;
assert.equal(
  JSON.parse(saved.getItem("second:answers")).request.decision,
  "approve",
);
const s3 = new ThreadPermissions({
  runId: "run",
  uuid: () => "",
  storage: saved,
  key: "second",
  changed: (s) => (state = s),
  request: async () => ({}),
});
assert.equal(state.busy.request, true);
assert.equal(state.results.request.decision, "approve");
s3.close();
console.log("permission projection and decision recovery passed");

// Questions share durable delivery with approvals but carry actual answers.
const question = {
  ...frame,
  kind: "question/request",
  data: {
    question_id: "question",
    turn_id: "turn",
    blocking: true,
    questions: [
      {
        id: "colour",
        text: "Colour?",
        header: "Colour",
        multiple: false,
        options: [],
      },
    ],
  },
};
let questionResult = null;
let questionWrites = [];
const questionOptions = {
  runId: "run",
  uuid: () => "unused",
  storage: saved,
  key: "question-test",
  changed: (s) => {
    state = s;
  },
  request: async (body) => {
    if (body) {
      questionWrites.push(body);
      return {};
    }
    if (!questionWrites.length)
      throw Object.assign(new Error("missing"), { status: 404 });
    return {
      action: "answer",
      answers: { colour: ["Custom & ü"] },
      result: questionResult,
    };
  },
};
let questions = new ThreadPermissions(questionOptions);
questions.observe([question]);
await questions.poll();
let qi = approvalItems([question], state, live)[0];
assert.equal(qi.available, true);
questions.draftQuestion(qi, "colour", { selected: [], text: "Custom & ü" });
assert.equal(
  approvalItems([question], state, live)[0].draft.colour.text,
  "Custom & ü",
);
questions.close();
questions = new ThreadPermissions(questionOptions);
questions.observe([question]);
await questions.poll();
qi = approvalItems([question], state, live)[0];
assert.equal(qi.draft.colour.text, "Custom & ü");
await questions.answer(qi, "answer", { colour: ["Custom & ü"] });
await questions.answer(qi, "answer", { colour: ["Other"] });
assert.equal(questionWrites.length, 1);
assert.deepEqual(questionWrites[0], {
  id: "question",
  action: "answer",
  run_id: "run",
  question_id: "question",
  answers: { colour: ["Custom & ü"] },
});
questionResult = {
  outcome: "accepted",
  decision: "answer",
  answers: { colour: ["Custom & ü"] },
};
await questions.poll();
assert.equal(approvalItems([question], state, live)[0].status, "answered");
assert.deepEqual(approvalItems([question], state, live)[0].answers, {
  colour: ["Custom & ü"],
});
questions.close();
const ended = {
  ...frame,
  sequence: 2,
  kind: "turn/completed",
  data: { turn_id: "turn" },
};
assert.equal(
  approvalItems([question, ended], blank(), live)[0].status,
  "resolved",
);
assert.equal(
  approvalItems(
    [{ ...question, data: { ...question.data, blocking: false } }, ended],
    blank(),
    live,
  )[0].status,
  "pending",
);

// Settled requests without an Acta answer (cancelled or provider-resolved)
// must not add an endless GET on every poll as conversation history grows.
const lookups = [];
const polling = new ThreadPermissions({
  runId: "run",
  uuid: () => "",
  changed: () => {},
  request: async (_body, id) => {
    lookups.push(id);
    throw Object.assign(new Error("missing"), { status: 404 });
  },
});
polling.observe([{ ...question, approval_resolved: true }, frame]);
await polling.poll();
await polling.poll();
await polling.poll();
assert.equal(lookups.filter((id) => id === "question").length, 1);
assert.equal(lookups.filter((id) => id === "request").length, 3);
polling.close();

// A resolved frame must not suppress recovery of a browser-owned uncertain write.
let recovered = 0;
const recoverStorage = {
  getItem: (key) =>
    key.endsWith(":answers")
      ? JSON.stringify({
          question: {
            id: "question",
            action: "answer",
            run_id: "run",
            question_id: "question",
            answers: { colour: ["Blue"] },
          },
        })
      : null,
};
const uncertain = new ThreadPermissions({
  runId: "run",
  uuid: () => "",
  changed: () => {},
  storage: recoverStorage,
  key: "recover",
  request: async (body) => {
    if (body) {
      recovered++;
      return {};
    }
    throw Object.assign(new Error("missing"), { status: 404 });
  },
});
uncertain.observe([{ ...question, approval_resolved: true }]);
await uncertain.poll();
await uncertain.poll();
assert.equal(recovered, 2);
uncertain.close();

// A previously absent command may be answered in another browser before the
// resolved projection arrives. Fetch its final result instead of caching the old 404.
let remotelyAnswered = false;
let remoteState;
const remote = new ThreadPermissions({
  runId: "run",
  uuid: () => "",
  changed: (s) => (remoteState = s),
  request: async () => {
    if (!remotelyAnswered)
      throw Object.assign(new Error("missing"), { status: 404 });
    return {
      result: {
        outcome: "accepted",
        decision: "answer",
        answers: { colour: ["Remote"] },
      },
    };
  },
});
remote.observe([question]);
await remote.poll();
remotelyAnswered = true;
remote.observe([{ ...question, approval_resolved: true }]);
await remote.poll();
assert.equal(remoteState.results.question.outcome, "accepted");
assert.deepEqual(remoteState.results.question.answers, { colour: ["Remote"] });
remote.close();
