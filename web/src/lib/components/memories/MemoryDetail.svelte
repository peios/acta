<script lang="ts">
  import { onMount } from "svelte";
  import { api, errorMessage } from "$lib/api";
  import type { Memory } from "$lib/memories";
  import MarkdownView from "$lib/components/tasks/MarkdownView.svelte";
  import RelativeTime from "$lib/components/RelativeTime.svelte";
  let {
    memory,
    onchange,
    ondelete,
    onediting = () => {},
  }: {
    memory: Memory;
    onchange: (memory: Memory) => void;
    ondelete: () => void;
    onediting?: (value: boolean) => void;
  } = $props();
  let editing = $state(false),
    deleting = $state(false),
    busy = $state(false),
    error = $state("");
  let key = $state(""),
    summary = $state(""),
    content = $state("");
  $effect(() => onediting(editing || busy));
  onMount(() => () => onediting(false));
  let revision = 0;
  function edit() {
    key = memory.key;
    summary = memory.summary;
    content = memory.content || "";
    revision = memory.revision;
    editing = true;
    deleting = false;
    error = "";
  }
  async function save() {
    if (busy) return;
    busy = true;
    error = "";
    try {
      const saved = await api<Memory>("memories", {
        id: memory.id,
        scope: memory.scope,
        workspace: memory.scope === "workspace" ? memory.scope_id : null,
        agent_id: memory.scope === "agent" ? memory.scope_id : "",
        key,
        summary,
        content,
        revision,
      });
      editing = false;
      onchange(saved);
    } catch (e) {
      error = errorMessage(e);
    } finally {
      busy = false;
    }
  }
  async function remove() {
    if (busy) return;
    busy = true;
    error = "";
    try {
      await api(`memories/${memory.id}/delete`, { revision: memory.revision });
      ondelete();
    } catch (e) {
      error = errorMessage(e);
    } finally {
      busy = false;
    }
  }
</script>

<div class="memory-detail">
  <div class="heading">
    <span class="scope">{memory.scope}</span>
    {#if memory.can_write && !editing}<div class="actions">
        <button class="quiet" onclick={edit} disabled={busy}>Edit</button
        ><button
          class="quiet"
          onclick={() => {
            deleting = true;
            error = "";
          }}
          disabled={busy}>Delete</button
        >
      </div>{/if}
  </div>
  {#if error}<p class="notice error" role="alert">{error}</p>{/if}
  {#if editing}
    <form
      onsubmit={(event) => {
        event.preventDefault();
        void save();
      }}
    >
      <h2>Edit memory</h2>
      <label
        >Key<input
          bind:value={key}
          required
          maxlength="100"
          disabled={busy}
        /></label
      >
      <label
        >Summary<input
          bind:value={summary}
          required
          maxlength="500"
          disabled={busy}
        /></label
      >
      <label
        >Content <span>Markdown supported</span><textarea
          bind:value={content}
          required
          rows="14"
          maxlength="64000"
          disabled={busy}></textarea></label
      >
      <div class="actions">
        <button class="primary" disabled={busy}
          >{busy ? "Saving…" : "Save memory"}</button
        ><button
          type="button"
          class="secondary"
          disabled={busy}
          onclick={() => {
            editing = false;
            error = "";
          }}>Cancel</button
        >
      </div>
    </form>
  {:else}
    <h2>{memory.key}</h2>
    <p class="summary">{memory.summary}</p>
    <MarkdownView value={memory.content || ""} />
    <p class="updated">
      Revision {memory.revision} · Updated <RelativeTime
        value={memory.updated_at}
      />
    </p>
    {#if deleting}<div class="delete-confirm">
        <p>Delete this memory permanently?</p>
        <div class="actions">
          <button class="danger" disabled={busy} onclick={remove}
            >Delete memory</button
          ><button
            class="secondary"
            disabled={busy}
            onclick={() => (deleting = false)}>Cancel</button
          >
        </div>
      </div>{/if}
  {/if}
</div>

<style>
  .memory-detail {
    min-width: 0;
    overflow-wrap: anywhere;
  }
  .heading,
  .actions {
    display: flex;
    align-items: center;
    gap: 10px;
  }
  .heading {
    justify-content: space-between;
    min-height: 32px;
  }
  .scope {
    font-size: 11px;
    color: var(--muted);
    text-transform: uppercase;
    letter-spacing: 0.06em;
  }
  h2 {
    font-size: 19px;
    margin: 16px 0 12px;
  }
  .summary {
    color: var(--muted);
    line-height: 1.6;
    margin-bottom: 24px;
  }
  .updated {
    font-size: 12px;
    color: var(--muted);
    margin-top: 28px;
  }
  .quiet {
    border: 0;
    background: transparent;
    color: var(--muted);
    padding: 6px 9px;
    font-size: 12px;
  }
  .quiet:hover {
    background: var(--hover-surface);
    color: var(--text);
  }
  form {
    display: flex;
    flex-direction: column;
    gap: 16px;
  }
  label {
    display: flex;
    flex-direction: column;
    gap: 7px;
    font-size: 12px;
    color: var(--muted);
  }
  label span {
    font-size: 11px;
  }
  input,
  textarea {
    width: 100%;
    box-sizing: border-box;
  }
  textarea {
    resize: vertical;
    font-family: inherit;
  }
  .actions button {
    width: auto;
  }
  .delete-confirm {
    border-top: 1px solid var(--panel-border);
    margin-top: 20px;
    padding-top: 12px;
  }
</style>
