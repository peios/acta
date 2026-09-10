import { imageRefs, loadImage, removeImage } from "./thread-image-input.js";
/** A browser-local outbox. Unknown delivery reuses the original command identity;
 * only an explicit rejection permits a new attempt. Provider echoes are final.
 * @typedef {{id:string, run_id:string, text:string, images?:import("./thread-image-input.js").DraftImage[], after:number, draftChanged?:boolean, draftRestored?:boolean, state:'sending'|'accepted'|'rejected'|'uncertain', error:string}} Submission
 * @typedef {{draft:string, images?:import("./thread-image-input.js").DraftImage[], pending:Submission|null, error:string}} SendingState
 */
export class ThreadSender {
  /** @param {{storage:Storage, key:string, request:(body?:unknown, id?:string)=>Promise<any>, changed:(state:SendingState)=>void, uuid:()=>string, loadImage?:(ref:import("./thread-image-input.js").DraftImage)=>Promise<any>}} options */
  constructor(options) {
    this.options = options;
    /** @type {SendingState} */
    this.state = { draft: "", images: [], pending: null, error: "" };
    this.closed = false;
    this.posting = false;
    this.polling = false;
    try {
      const saved = JSON.parse(options.storage.getItem(options.key) || "null");
      if (saved && typeof saved.draft === "string")
        this.state.draft = saved.draft;
      this.state.images = imageRefs(saved?.images);
      const p = saved?.pending;
      if (
        p &&
        typeof p.id === "string" &&
        typeof p.run_id === "string" &&
        typeof p.text === "string" &&
        Number.isFinite(p.after) &&
        ["sending", "accepted", "rejected", "uncertain"].includes(p.state)
      )
        this.state.pending = { ...p, images: imageRefs(p.images) };
    } catch {
      this.state.error = "Could not restore the message draft.";
    }
    if (this.state.pending?.state === "sending") {
      this.state.pending.state = "uncertain";
      this.state.pending.error = "Waiting for delivery confirmation.";
    }
    this.publish();
  }
  publish() {
    if (!this.closed) this.options.changed(structuredClone(this.state));
  }
  save() {
    try {
      this.options.storage.setItem(
        this.options.key,
        JSON.stringify(this.state),
      );
      this.state.error = "";
      this.publish();
      return true;
    } catch {
      this.state.error =
        "Could not save the draft in this browser. Your text is still here.";
      this.publish();
      return false;
    }
  }
  /** @param {string} text */
  draft(text) {
    if (this.state.pending) this.state.pending.draftChanged = true;
    this.state.draft = text;
    this.save();
  }
  /** @param {import("./thread-image-input.js").DraftImage[]} images */
  images(images) {
    if (this.closed) return;
    if (this.state.pending) this.state.pending.draftChanged = true;
    const previous = this.state.images || [];
    this.state.images = imageRefs(images).map((i) => ({
      id: i.id,
      name: i.name,
      size: i.size,
      media_type: i.media_type,
    }));
    if (this.save())
      for (const ref of previous) {
        if (
          !images.some((i) => i.id === ref.id) &&
          !this.state.pending?.images?.some((i) => i.id === ref.id)
        )
          void removeImage(ref).catch(() => {});
      }
  }
  /** @param {string} runId @param {number} after */
  async send(runId, after) {
    if (
      this.closed ||
      this.posting ||
      (!this.state.draft.trim() && !this.state.images?.length) ||
      (this.state.pending && this.state.pending.state !== "rejected")
    )
      return;
    if (new TextEncoder().encode(this.state.draft).length > 65536) {
      this.state.error = "Messages can be up to 64 KiB.";
      this.publish();
      return;
    }
    const previous = this.state.pending;
    const draft = this.state.draft;
    const images = this.state.images;
    this.state.pending = {
      id: this.options.uuid(),
      run_id: runId,
      text: this.state.draft,
      images: [...(this.state.images || [])],
      after,
      draftChanged: false,
      draftRestored: false,
      state: "sending",
      error: "",
    };
    this.state.draft = "";
    this.state.images = [];
    if (!this.save()) {
      this.state.draft = draft;
      this.state.images = images;
      this.state.pending = previous;
      this.publish();
      return;
    }
    for (const ref of previous?.images || []) {
      if (!this.state.pending.images?.some((i) => i.id === ref.id))
        void removeImage(ref).catch(() => {});
    }
    await this.submit();
  }
  async submit() {
    const p = this.state.pending;
    if (
      !p ||
      this.closed ||
      this.posting ||
      p.state === "accepted" ||
      p.state === "rejected"
    )
      return;
    const firstAttempt = p.state === "sending";
    this.posting = true;
    let images;
    try {
      images = p.images?.length
        ? await Promise.all(p.images.map(this.options.loadImage || loadImage))
        : [];
    } catch (error) {
      this.posting = false;
      if (!this.closed) {
        p.state = firstAttempt ? "rejected" : "uncertain";
        p.error =
          error instanceof Error
            ? error.message
            : "Could not load attached images.";
        this.restoreRejectedDraft(p);
        this.save();
      }
      return;
    }
    if (this.closed || this.state.pending?.id !== p.id) {
      this.posting = false;
      return;
    }
    try {
      await this.options.request({
        id: p.id,
        action: "send",
        run_id: p.run_id,
        text: p.text,
        ...(images.length ? { images } : {}),
      });
    } catch (error) {
      if (!this.closed && this.state.pending?.id === p.id) {
        const status =
          error && typeof error === "object" && "status" in error
            ? Number(error.status)
            : 0;
        const rejected =
          firstAttempt &&
          [400, 401, 403, 404, 409, 413, 415, 422].includes(status);
        p.state = rejected ? "rejected" : "uncertain";
        p.error =
          rejected && error instanceof Error
            ? error.message
            : "Delivery has not been confirmed. Checking again will not send a second message.";
        this.restoreRejectedDraft(p);
        this.save();
      }
    } finally {
      this.posting = false;
    }
    await this.poll();
  }
  async poll() {
    const p = this.state.pending;
    if (!p || this.closed || this.polling || p.state === "rejected") return;
    this.polling = true;
    try {
      const response = await this.options.request(undefined, p.id);
      if (this.closed || this.state.pending?.id !== p.id) return;
      // The echo may be older than the newest history page after a reload or
      // route change. The server confirms it by submission ID, never by text.
      if (response.message_confirmed === true) {
        this.confirm();
        return;
      }
      if (!response.result) return;
      const result = response.result;
      if (!["accepted", "rejected", "uncertain"].includes(result.outcome))
        return;
      if (p.state === "accepted" && result.outcome !== "accepted") return;
      p.state = result.outcome;
      p.error = result.error || "";
      if (p.state === "accepted") this.clearSubmittedDraft(p);
      this.restoreRejectedDraft(p);
      this.save();
    } catch {
      /* A failed status read is not evidence of provider rejection. */
    } finally {
      this.polling = false;
    }
  }
  /** @param {Array<{kind:string,data?:Record<string,any>}>} frames */
  observe(frames) {
    const p = this.state.pending;
    if (
      !p ||
      !frames.some(
        (f) =>
          f.kind === "message/user" &&
          f.data?.submission_id === p.id &&
          ["in_progress", "completed"].includes(f.data?.state),
      )
    )
      return;
    this.confirm();
  }
  confirm() {
    const p = this.state.pending;
    if (this.closed || !p) return;
    this.clearSubmittedDraft(p);
    for (const ref of p.images || [])
      if (!this.state.images?.some((i) => i.id === ref.id))
        void removeImage(ref).catch(() => {});
    this.state.pending = null;
    this.save();
  }
  /** @param {Submission} p */
  restoreRejectedDraft(p) {
    if (
      p.state !== "rejected" ||
      p.draftChanged ||
      p.draftRestored ||
      this.state.draft !== "" ||
      this.state.images?.length
    )
      return;
    this.state.draft = p.text;
    this.state.images = [...(p.images || [])];
    p.draftRestored = true;
  }
  /** @param {Submission} p */
  clearSubmittedDraft(p) {
    // A late confirmation may clear an untouched restored draft, never text
    // typed since submission (even if it happens to match the sent message).
    if (!p.draftRestored || p.draftChanged) return;
    this.state.draft = "";
    this.state.images = [];
    p.draftRestored = false;
  }
  close() {
    this.closed = true;
  }
}
