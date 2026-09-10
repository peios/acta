import { uniqueFrames } from "./thread-frames.js";
/** @typedef {import('./threads.svelte').ThreadFrame} Frame */
/** @typedef {{id:string,header:string,text:string,multiple:boolean,options:{label:string,description:string}[]}} Question */
/** @typedef {{selected:string[],text:string}} QuestionDraft */
/** @typedef {{lane_name?:string,approval_id:string,question_id?:string,blocking?:boolean,questions?:Question[],title:string,reason:string,details:Record<string,any>,turn_id?:string,tool_id?:string}} ApprovalData */
/** @typedef {{id:string,frame:Frame,data:ApprovalData,status:string,error:string,busy:boolean,available:boolean,decision?:string,draft?:Record<string,QuestionDraft>,answers?:Record<string,string[]>}} ApprovalItem */
/** @typedef {{results:Record<string,any>,busy:Record<string,boolean>,errors:Record<string,string>,checked:Record<string,boolean>,modePending:string,modeError:string,drafts?:Record<string,Record<string,QuestionDraft>>}} PermissionState */
/** @param {Frame[]} frames @param {PermissionState} state @param {{run_id:string,state:string,connection_id?:string}|undefined} thread @returns {ApprovalItem[]} */
export function approvalItems(frames, state, thread) {
  const items = new Map();
  for (const frame of uniqueFrames(frames)) {
    const d = frame.data ?? {};
    if (
      ["approval/request", "question/request"].includes(frame.kind) &&
      typeof (d?.approval_id ?? d?.question_id) === "string" &&
      !items.has(d.approval_id ?? d.question_id)
    )
      items.set(d.approval_id ?? d.question_id, {
        frame,
        data: d,
        resolved: !!frame.approval_resolved,
      });
    if (
      ["approval/resolved", "question/resolved"].includes(frame.kind) &&
      items.has(d?.approval_id ?? d?.question_id)
    )
      items.get(d?.approval_id ?? d?.question_id).resolved = true;
    if (frame.kind === "turn/completed")
      for (const item of items.values())
        if (
          item.frame.run_id === frame.run_id &&
          item.frame.lane_id === frame.lane_id &&
          item.data.turn_id === d?.turn_id &&
          item.data.blocking !== false
        )
          item.resolved = true;
  }
  return [...items].map(([id, item]) => {
    const result = state.results[id];
    const live =
      thread?.state === "running" &&
      !!thread.connection_id &&
      item.frame.run_id === thread.run_id;
    const status =
      result?.outcome === "accepted"
        ? result.decision === "answer"
          ? "answered"
          : result.decision === "approve"
            ? "approved"
            : "denied"
        : result?.outcome === "uncertain"
          ? "unconfirmed"
          : item.resolved
            ? "resolved"
            : !live
              ? "unavailable"
              : result?.outcome === "rejected"
                ? "unavailable"
                : "pending";
    return {
      id,
      ...item,
      status,
      error: state.errors[id] || result?.error || "",
      busy: !!state.busy[id],
      available: live && status === "pending" && !!state.checked[id],
      decision: result?.decision,
      draft: state.drafts?.[id] ?? {},
      answers: result?.answers ?? {},
    };
  });
}
export const permissionModes = [
  {
    id: "ask",
    label: "Ask for approval",
    description: "You decide when an action needs approval.",
  },
  {
    id: "automatic",
    label: "Approve for me",
    description: "The provider reviews and decides for you.",
  },
  {
    id: "bypass",
    label: "Unrestricted",
    description: "Broader access without ordinary approval checks.",
  },
];
export class ThreadPermissions {
  /** @param {{request:(body:any,id?:string)=>Promise<any>,changed:(state:PermissionState)=>void,runId:string,uuid:()=>string,storage?:Storage,key?:string}} options */
  constructor(options) {
    this.options = options;
    this.closed = false;
    this.polling = false;
    this.ids = new Set();
    this.resolved = new Set();
    this.absent = new Set();
    /** @type {PermissionState} */ this.state = {
      results: {},
      busy: {},
      errors: {},
      checked: {},
      modePending: "",
      modeError: "",
    };
    /** @type {any} */ this.modeCommand = null;
    this.answers = {};
    try {
      this.state.drafts = JSON.parse(
        options.storage?.getItem((options.key || "") + ":drafts") || "{}",
      );
    } catch {
      this.state.drafts = {};
    }
    try {
      this.modeCommand = JSON.parse(
        options.storage?.getItem(options.key || "") || "null",
      );
      if (this.modeCommand?.run_id !== options.runId) this.modeCommand = null;
    } catch {}
    try {
      this.answers = JSON.parse(
        options.storage?.getItem((options.key || "") + ":answers") || "{}",
      );
    } catch {}
    for (const [id, q] of Object.entries(this.answers)) {
      this.state.results[id] = { outcome: "pending", decision: q.decision };
      this.state.busy[id] = true;
    }
    if (this.modeCommand)
      this.state.modePending = this.modeCommand.permission_mode;
    this.publish();
  }
  publish() {
    if (!this.closed)
      this.options.changed({
        ...this.state,
        results: { ...this.state.results },
        busy: { ...this.state.busy },
        errors: { ...this.state.errors },
        checked: { ...this.state.checked },
        drafts: { ...this.state.drafts },
      });
  }
  /** @param {Frame[]} frames */ observe(frames) {
    for (const f of frames)
      if (
        ["approval/request", "question/request"].includes(f.kind) &&
        typeof (f.data?.approval_id ?? f.data?.question_id) === "string"
      ) {
        const id = f.data?.approval_id ?? f.data?.question_id;
        this.ids.add(id);
        if (f.approval_resolved && !this.resolved.has(id)) {
          this.absent.delete(id);
          this.resolved.add(id);
        }
      }
  }
  async poll() {
    if (this.closed || this.polling) return;
    this.polling = true;
    try {
      for (const id of this.ids) {
        if (this.closed) return;
        if (this.resolved.has(id) && this.absent.has(id) && !this.answers[id])
          continue;
        if (["accepted", "rejected"].includes(this.state.results[id]?.outcome))
          continue;
        const settled = this.resolved.has(id);
        try {
          const response = await this.options.request(undefined, id);
          if (this.closed) return;
          this.absent.delete(id);
          if (response.result) {
            this.state.results[id] = response.result;
            this.state.busy[id] = false;
            delete this.answers[id];
            this.saveAnswers();
          } else if (["approval", "answer"].includes(response.action)) {
            this.state.results[id] = {
              outcome: "pending",
              decision:
                response.action === "answer" ? "answer" : response.decision,
              answers: response.answers,
            };
            this.state.busy[id] = true;
          }
          this.state.errors[id] = "";
          this.state.checked[id] = true;
        } catch (e) {
          if (this.closed) return;
          if (/** @type {any} */ (e).status === 404) {
            if (settled) this.absent.add(id);
            this.state.checked[id] = true;
            if (this.answers[id]) {
              try {
                await this.options.request(this.answers[id]);
              } catch {}
            }
          } else
            this.state.errors[id] = String(e instanceof Error ? e.message : e);
        }
      }
      if (this.modeCommand) {
        try {
          const reply = await this.options.request(
            undefined,
            this.modeCommand.id,
          );
          if (this.closed) return;
          if (reply.result) {
            this.state.modeError =
              reply.result.error ||
              (reply.result.outcome === "uncertain"
                ? "Mode change is unconfirmed."
                : "");
            this.modeCommand = null;
            this.state.modePending = "";
            this.saveMode();
          }
        } catch (e) {
          if (!this.closed) {
            this.state.modeError = String(e instanceof Error ? e.message : e);
            if (/** @type {any} */ (e).status === 404) {
              try {
                await this.options.request(this.modeCommand);
              } catch {}
            }
          }
        }
      }
      this.publish();
    } finally {
      this.polling = false;
    }
  }
  /** @param {ApprovalItem} item @param {'approve'|'deny'|'answer'} decision @param {Record<string,string[]>} [values] */
  async answer(item, decision, values) {
    if (
      this.closed ||
      !item.available ||
      this.state.busy[item.id] ||
      this.state.results[item.id]
    )
      return;
    this.state.busy[item.id] = true;
    this.state.results[item.id] = {
      outcome: "pending",
      decision,
      answers: values,
    };
    this.state.errors[item.id] = "";
    this.answers[item.id] = {
      id: item.id,
      action: decision === "answer" ? "answer" : "approval",
      run_id: item.frame.run_id,
      ...(decision === "answer"
        ? { question_id: item.id, answers: values }
        : { approval_id: item.id, decision }),
    };
    this.saveAnswers();
    this.publish();
    try {
      await this.options.request(this.answers[item.id]);
    } catch (e) {
      if (!this.closed)
        this.state.errors[item.id] = String(e instanceof Error ? e.message : e);
    } finally {
      if (!this.closed) {
        this.publish();
        void this.poll();
      }
    }
  }
  /** @param {ApprovalItem} item @param {string} id @param {QuestionDraft} value */
  draftQuestion(item, id, value) {
    if (this.closed || item.status !== "pending" || this.state.busy[item.id])
      return;
    const drafts = this.state.drafts ?? {};
    this.state.drafts = {
      ...drafts,
      [item.id]: { ...drafts[item.id], [id]: value },
    };
    try {
      this.options.storage?.setItem(
        (this.options.key || "") + ":drafts",
        JSON.stringify(this.state.drafts),
      );
    } catch {}
    this.publish();
  }
  saveAnswers() {
    try {
      this.options.storage?.setItem(
        (this.options.key || "") + ":answers",
        JSON.stringify(this.answers),
      );
    } catch {}
  }
  saveMode() {
    try {
      if (this.modeCommand)
        this.options.storage?.setItem(
          this.options.key || "",
          JSON.stringify(this.modeCommand),
        );
      else this.options.storage?.removeItem(this.options.key || "");
    } catch {}
  }
  /** @param {string} mode */ async mode(mode) {
    if (this.closed || this.modeCommand) return;
    this.modeCommand = {
      id: this.options.uuid(),
      action: "permissions",
      run_id: this.options.runId,
      permission_mode: mode,
    };
    this.saveMode();
    this.state.modePending = mode;
    this.state.modeError = "";
    this.publish();
    try {
      await this.options.request(this.modeCommand);
    } catch (e) {
      if (!this.closed)
        this.state.modeError = String(e instanceof Error ? e.message : e);
    } finally {
      if (!this.closed) {
        this.publish();
        void this.poll();
      }
    }
  }
  close() {
    this.closed = true;
  }
}
