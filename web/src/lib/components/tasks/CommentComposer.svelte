<script lang="ts">
  import { onMount } from "svelte";
  import { api, APIError, errorMessage } from "$lib/api";
  import { useAccount } from "$lib/account-context";
  import { taskChanged } from "$lib/tasks";
  import type { ActivityEntry } from "$lib/activity";
  import {
    emptyCommentDraft,
    restoreCommentDraft,
    type CommentDraft,
  } from "$lib/comment-draft.js";
  const localDraft = $state(emptyCommentDraft());
  import MarkdownEditor from "./MarkdownEditor.svelte";
  let {
    task,
    draft = localDraft,
    replyTo = "",
    replyName = "",
    onposted,
    oncancel,
  }: {
    task: string;
    draft?: CommentDraft;
    replyTo?: string;
    replyName?: string;
    onposted: (entry: ActivityEntry) => void;
    oncancel?: () => void;
  } = $props();
  const account = useAccount();
  let region: HTMLDivElement;
  let wantsFocus = $state(false);
  let busy = $state(false),
    error = $state("");
  const key = $derived(
    `acta.comment-draft:${account.account.id}:${task}:${replyTo || "root"}`,
  );
  function saveDraft() {
    try {
      localStorage.setItem(
        key,
        JSON.stringify({ body: draft.body, pending: draft.pending }),
      );
    } catch {}
  }
  function change(value: string) {
    draft.body = value;
    saveDraft();
  }
  onMount(() => {
    wantsFocus = !!replyTo;
    if (!draft.loaded) {
      let saved = null;
      try {
        saved = localStorage.getItem(key);
      } catch {}
      Object.assign(draft, restoreCommentDraft(saved));
      draft.open ||= !!replyTo;
    }
  });
  async function post() {
    if (busy || (!draft.body.trim() && !draft.pending)) return;
    busy = true;
    error = "";
    if (!draft.pending) {
      const requestID = crypto.randomUUID();
      draft.pending = {
        body: draft.body,
        request_id: requestID,
        reply_to: replyTo,
      };
      saveDraft();
    }
    try {
      const entry = await api<ActivityEntry>(
        `tasks/${task}/comments`,
        draft.pending,
      );
      draft.body = "";
      draft.pending = null;
      saveDraft();
      draft.open = !!replyTo;
      taskChanged();
      onposted(entry);
    } catch (e) {
      error = errorMessage(e);
      if (
        e instanceof APIError &&
        e.status >= 400 &&
        e.status < 500 &&
        e.status !== 408
      ) {
        draft.pending = null;
        saveDraft();
      }
    } finally {
      busy = false;
    }
  }
</script>

<svelte:window
  onclick={(event) => {
    if (draft.open && region && !event.composedPath().includes(region)) {
      draft.open = false;
      wantsFocus = false;
    }
  }}
/>

<div bind:this={region} class="composer" class:open={draft.open}>
  {#if draft.open}
    <form
      onsubmit={(e) => {
        e.preventDefault();
        void post();
      }}
    >
      {#if replyName}<p class="replying">Replying to {replyName}</p>{/if}
      <MarkdownEditor
        autofocus={wantsFocus}
        onsubmit={() => void post()}
        value={draft.body}
        onchange={change}
        disabled={busy || !!draft.pending}
        label={replyTo ? "Reply" : "Comment"}
      />
      {#if error}<p class="notice error" role="alert">
          {error} Your draft is kept. Retry checks the same post, so it won’t be duplicated.
        </p>{/if}
      <div class="actions">
        <span>⌘ / Ctrl + Enter to post</span>{#if oncancel}<button
            type="button"
            class="cancel"
            disabled={busy}
            onclick={oncancel}>Cancel</button
          >{/if}<button
          class="primary"
          disabled={busy || (!draft.body.trim() && !draft.pending)}
          >{busy
            ? "Posting…"
            : draft.pending
              ? "Retry post"
              : replyTo
                ? "Reply"
                : "Post"}</button
        >
      </div>
    </form>
  {:else}<button
      class="start"
      class:has-draft={!!draft.body.trim()}
      aria-label={draft.body.trim()
        ? replyTo
          ? "Edit reply draft"
          : "Edit comment draft"
        : undefined}
      disabled={busy}
      onclick={() => {
        wantsFocus = true;
        draft.open = true;
      }}
      ><span class="draft-text"
        >{draft.body.trim()
          ? draft.body
          : replyTo
            ? "Write a reply…"
            : "Write a comment…"}</span
      >{#if busy || draft.pending || error}<span class="draft-status"
          >{busy
            ? "Posting…"
            : draft.pending
              ? "Post not confirmed · Click to retry"
              : "Couldn’t post · Click to review"}</span
        >{/if}</button
    >{/if}
</div>

<style>
  .composer {
    margin: 14px 0 8px;
    border: 1px solid var(--panel-border);
    border-radius: 12px;
    background: color-mix(in srgb, var(--surface) 65%, transparent);
  }
  .composer.open {
    padding: 12px 14px;
  }
  .start {
    width: 100%;
    text-align: left;
    padding: 16px;
    border: 0;
    border-radius: 12px;
    background: transparent;
    color: var(--muted);
    font-size: 13px;
  }
  .start.has-draft {
    color: var(--text);
  }
  .draft-text {
    display: block;
    white-space: pre-wrap;
    overflow-wrap: anywhere;
  }
  .draft-status {
    display: block;
    margin-top: 6px;
    font-size: 11px;
    color: var(--muted);
  }
  .start:hover {
    color: var(--text);
    background: var(--hover-surface);
  }
  .actions {
    display: flex;
    align-items: center;
    justify-content: flex-end;
    gap: 10px;
    margin-top: 10px;
  }
  .actions span {
    margin-right: auto;
    font-size: 10px;
    color: var(--muted);
  }
  .actions button {
    font-size: 12px;
    padding: 7px 13px;
    border-radius: 7px;
  }
  .actions .primary {
    width: auto;
    min-height: 46px;
    flex: none;
    padding: 7px 13px;
  }
  .cancel {
    border: 0;
    background: transparent;
    color: var(--muted);
  }
  .replying {
    font-size: 11px;
    color: var(--muted);
    margin: 0 0 5px;
  }
  @media (max-width: 420px) {
    .actions span {
      display: none;
    }
  }
</style>
