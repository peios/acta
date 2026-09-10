<script lang="ts">
  import { tick } from "svelte";
  import { errorMessage } from "$lib/api";
  import { createTask, taskChanged } from "$lib/tasks";
  let {
    workspace,
    status,
    label,
    properties = {},
    assignees = [],
  }: {
    workspace: string;
    status: string;
    label: string;
    properties?: { priority?: string; type?: string; size?: string };
    assignees?: string[];
  } = $props();
  let editing = $state(false),
    title = $state(""),
    busy = $state(false),
    error = $state("");
  let input = $state<HTMLInputElement>(null!);
  let trigger = $state<HTMLButtonElement>(null!);
  async function start() {
    editing = true;
    await tick();
    input.focus();
  }
  async function close() {
    editing = false;
    title = "";
    error = "";
    await tick();
    trigger.focus();
  }
  async function submit() {
    if (busy || !title.trim()) return;
    busy = true;
    error = "";
    try {
      await createTask(workspace, title.trim(), {
        ...properties,
        status_id: status,
        assignees,
      });
      taskChanged();
      await close();
    } catch (e) {
      error = errorMessage(e);
    } finally {
      busy = false;
    }
  }
</script>

<div class="inline-create">
  {#if editing}
    <form
      onsubmit={(event) => {
        event.preventDefault();
        void submit();
      }}
    >
      <input
        bind:this={input}
        bind:value={title}
        aria-label={`New task title in ${label}`}
        placeholder="Task title…"
        maxlength="300"
        required
        disabled={busy}
        onkeydown={(event) => {
          if (event.key === "Escape" && !busy) {
            event.preventDefault();
            event.stopPropagation();
            void close();
          }
        }}
      />
      <div class="actions">
        <span>{busy ? "Creating…" : "Enter to create"}</span>
        <button type="button" disabled={busy} onclick={() => void close()}
          >Cancel</button
        >
        <button type="submit" class="add" disabled={busy || !title.trim()}
          >Add</button
        >
      </div>
      {#if error}<p role="alert" class="notice error">{error}</p>{/if}
    </form>
  {:else}
    <button
      bind:this={trigger}
      class="create"
      onclick={() => void start()}
      aria-label={`Create new task in ${label}`}
    >
      <svg
        viewBox="0 0 24 24"
        width="16"
        height="16"
        fill="none"
        stroke="currentColor"
        stroke-width="1.7"
        aria-hidden="true"><path d="M12 5v14M5 12h14" /></svg
      >
      Create new task
    </button>
  {/if}
</div>

<style>
  .inline-create {
    margin-bottom: 8px;
  }
  .create {
    display: flex;
    align-items: center;
    gap: 7px;
    width: 100%;
    padding: 9px 10px;
    border: 0;
    border-radius: 8px;
    background: transparent;
    color: var(--muted);
    text-align: left;
    font-size: 12px;
    cursor: pointer;
  }
  .create:hover {
    background: var(--hover-surface);
    color: var(--text);
  }
  form {
    background: var(--surface);
    border: 1px solid var(--panel-border);
    border-radius: 10px;
    padding: 10px;
  }
  form:focus-within {
    border-color: var(--accent);
  }
  input {
    width: 100%;
    min-width: 0;
    padding: 2px;
    border: 0;
    border-radius: 0;
    background: transparent;
    color: var(--text);
    font-size: 13px;
    box-shadow: none;
  }
  input:focus {
    outline: none;
    box-shadow: none;
  }
  .actions {
    display: flex;
    align-items: center;
    gap: 4px;
    margin-top: 10px;
    font-size: 11px;
    color: var(--muted);
  }
  .actions span {
    margin-right: auto;
  }
  .actions button {
    border: 0;
    border-radius: 5px;
    padding: 4px 7px;
    background: transparent;
    color: var(--muted);
    cursor: pointer;
    font-size: 11px;
  }
  .actions button:hover,
  .actions .add {
    background: var(--hover-surface);
    color: var(--text);
  }
  .actions button:disabled {
    opacity: 0.5;
    cursor: default;
  }
</style>
