/** @typedef {{body:string,request_id:string,reply_to:string}} PendingComment */
/** @typedef {{body:string,pending:PendingComment|null,open:boolean,loaded:boolean}} CommentDraft */
/** @returns {CommentDraft} */
export const emptyCommentDraft = () => ({
  body: "",
  pending: null,
  open: false,
  loaded: false,
});
/** @param {string|null} raw @returns {CommentDraft} */
export function restoreCommentDraft(raw) {
  const draft = emptyCommentDraft();
  try {
    const value = JSON.parse(raw || "null");
    if (value && typeof value.body === "string") {
      draft.body = value.body;
      if (
        value.pending &&
        typeof value.pending.body === "string" &&
        typeof value.pending.request_id === "string" &&
        typeof value.pending.reply_to === "string"
      )
        draft.pending = value.pending;
    }
  } catch {}
  draft.open = !!draft.body || !!draft.pending;
  draft.loaded = true;
  return draft;
}
