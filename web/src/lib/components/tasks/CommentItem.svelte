<script lang="ts">
  import type { ActivityEntry } from "$lib/activity";
  import { api, APIError, errorMessage } from "$lib/api";
  import { taskChanged } from "$lib/tasks";
  import RelativeTime from "../RelativeTime.svelte";
  import MarkdownView from "./MarkdownView.svelte";
  import MarkdownEditor from "./MarkdownEditor.svelte";
  let {
    task,
    entry,
    canComment,
    highlighted = false,
    onreply,
    onchange,
    onreference,
    replyName = "earlier comment",
  }: {
    replyName?: string;
    task: string;
    entry: ActivityEntry;
    canComment: boolean;
    highlighted?: boolean;
    onreply: (e: ActivityEntry) => void;
    onchange: () => void;
    onreference?: (id: string) => void;
  } = $props();
  let editing = $state(false),
    body = $state(""),
    version = $state(0),
    deleteVersion = $state(0),
    busy = $state(false),
    error = $state(""),
    deleting = $state(false),
    latest = $state<ActivityEntry | null>(null);
  const comment = $derived(entry.comment!);
  const name = $derived(entry.actor.display_name || entry.actor.username);
  function edit() {
    body = comment.body;
    version = comment.version;
    editing = true;
    error = "";
    latest = null;
  }
  async function update(remove = false) {
    if (busy || (!remove && latest)) return;
    busy = true;
    error = "";
    try {
      await api(`tasks/${task}/comments/${entry.id}`, {
        body,
        version: remove ? deleteVersion : version,
        delete: remove,
      });
      editing = false;
      deleting = false;
      taskChanged();
      onchange();
    } catch (e) {
      error = errorMessage(e);
      if (e instanceof APIError && e.status === 409) {
        try {
          latest = await api<ActivityEntry>(
            `tasks/${task}/comments/${entry.id}`,
          );
          if (remove) {
            latest = null;
            onchange();
          }
        } catch {}
      }
    } finally {
      busy = false;
    }
  }
</script>

<article
  class="comment"
  class:highlighted
  data-read-id={entry.id}
  data-read-through={entry.last_event}
  data-unread={entry.unread}
  tabindex="-1"
  aria-label={`Comment by ${name}`}
>
  <span class="avatar" class:agent={!!entry.actor.owner_id} aria-hidden="true"
    >{name.slice(0, 1).toUpperCase()}</span
  >
  <div class="body">
    <div class="meta">
      <strong title={`@${entry.actor.username}`}>{name}</strong
      >{#if entry.actor.owner_id}<span class="agent-label">Agent</span
        >{/if}<RelativeTime
        value={entry.started_at}
      />{#if comment.edited && !comment.deleted}<span
          title={`Edited ${new Date(entry.updated_at).toLocaleString()}`}
          >Edited</span
        >{/if}
    </div>
    {#if comment.reply_to && onreference}<button
        class="reply-target"
        onclick={() => onreference?.(comment.reply_to!)}
        >↳ Replying to {replyName}</button
      >{/if}
    {#if editing}
      <form
        onsubmit={(e) => {
          e.preventDefault();
          void update();
        }}
      >
        <MarkdownEditor
          onsubmit={() => {
            if (!latest) void update();
          }}
          value={body}
          onchange={(v) => (body = v)}
          disabled={busy}
          label="Edit comment"
        />
        {#if latest}<div class="conflict">
            <p>
              This comment changed. Your text is kept above; compare it with the
              latest version below.
            </p>
            {#if latest.comment?.deleted}<p>
                Comment deleted
              </p>{:else}<MarkdownView
                value={latest.comment?.body || ""}
              /><button
                type="button"
                onclick={() => {
                  version = latest!.comment!.version;
                  latest = null;
                  error = "";
                }}>Keep my text and use this version</button
              >{/if}<button
              type="button"
              onclick={() => {
                editing = false;
                latest = null;
                onchange();
              }}>Discard my edit</button
            >
          </div>{/if}
        <div class="actions">
          <button
            type="button"
            disabled={busy}
            onclick={() => (editing = false)}>Cancel</button
          ><button class="save" disabled={busy || !body.trim() || !!latest}
            >{busy ? "Saving…" : "Save"}</button
          >
        </div>
      </form>
    {:else if comment.deleted}<p class="deleted">Comment deleted</p>
    {:else}<div class="text"><MarkdownView value={comment.body} /></div>{/if}
    {#if error}<p class="notice error" role="alert">{error}</p>{/if}
    {#if !editing}<div class="actions">
        {#if canComment}<button onclick={() => onreply(entry)}>Reply</button
          >{/if}
        {#if comment.can_edit}<button onclick={edit}>Edit</button>{/if}
        {#if comment.can_delete}<button
            onclick={() => {
              deleteVersion = comment.version;
              deleting = !deleting;
            }}>Delete</button
          >{/if}
        {#if deleting}<span>Delete this comment?</span><button
            disabled={busy}
            class="danger"
            onclick={() => void update(true)}>Delete comment</button
          ><button onclick={() => (deleting = false)}>Cancel</button>{/if}
      </div>{/if}
  </div>
</article>

<style>
  .comment {
    display: flex;
    gap: 11px;
    padding: 14px 10px;
    border-radius: 10px;
    scroll-margin: 12px;
    outline: none;
  }
  .comment:focus-visible {
    box-shadow: inset 0 0 0 1px var(--accent);
  }
  .highlighted {
    background: color-mix(in srgb, var(--accent) 5%, transparent);
  }
  .avatar {
    display: grid;
    place-items: center;
    flex: 0 0 27px;
    height: 27px;
    border-radius: 50%;
    background: var(--scope-active);
    color: var(--accent);
    font-size: 11px;
    font-weight: 600;
  }
  .avatar.agent {
    border-radius: 8px;
  }
  .body {
    flex: 1;
    min-width: 0;
  }
  .meta {
    display: flex;
    align-items: baseline;
    gap: 7px;
    flex-wrap: wrap;
    font-size: 10px;
    color: var(--muted);
    margin: 3px 0 9px;
  }
  strong {
    font-size: 12px;
    font-weight: 550;
    color: var(--text);
  }
  .agent-label {
    border: 1px solid var(--panel-border);
    border-radius: 4px;
    padding: 1px 4px;
    font-size: 9px;
  }
  .text {
    font-size: 13px;
    line-height: 1.7;
    overflow-wrap: anywhere;
  }
  .text :global(.task-rich-text > :first-child) {
    margin-top: 0;
  }
  .text :global(.task-rich-text > :last-child) {
    margin-bottom: 0;
  }
  .actions {
    display: flex;
    gap: 9px;
    align-items: center;
    flex-wrap: wrap;
    margin-top: 7px;
    font-size: 11px;
    color: var(--muted);
  }
  button {
    padding: 3px 0;
    background: none;
    border: 0;
    color: var(--muted);
    font-size: 11px;
  }
  button:hover {
    color: var(--text);
  }
  .save {
    color: var(--accent);
  }
  .danger {
    color: var(--danger, #dc7777);
  }
  .deleted {
    font-size: 12px;
    color: var(--muted);
    font-style: italic;
  }
  .reply-target {
    margin: 0 0 6px;
  }
  .conflict {
    border: 1px solid var(--panel-border);
    padding: 10px;
    font-size: 12px;
    border-radius: 8px;
  }
  .conflict button {
    display: block;
  }
</style>
