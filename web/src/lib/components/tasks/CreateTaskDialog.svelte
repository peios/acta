<script lang="ts">
  import { useAccount } from "$lib/account-context";
  import { flushSync } from "svelte";
  import { errorMessage } from "$lib/api";
  import { useWorkspace } from "$lib/workspaces";
  import {
    createTask,
    taskChanged,
    type Task,
    type TaskConfig,
  } from "$lib/tasks";
  let {
    workspace,
    config,
    oncreated,
  }: { workspace: string; config: TaskConfig; oncreated: (t: Task) => void } =
    $props();
  const account = useAccount();
  const currentWorkspace = useWorkspace();
  const id = $props.id();
  let draftKey = "";
  function saveDraft() {
    if (!draftKey) return;
    try {
      if (title) localStorage.setItem(draftKey, title);
      else localStorage.removeItem(draftKey);
    } catch {}
  }
  let dialog: HTMLDialogElement;
  let title = $state(""),
    parent = $state(""),
    error = $state(""),
    busy = $state(false);
  let input: HTMLInputElement;
  export function open(p = "") {
    if (busy) return;
    parent = p;
    draftKey = account?.account.id
      ? `acta.task-draft:${account.account.id}:${workspace}:${config.board ?? "tasks"}:${p}`
      : "";
    title = "";
    try {
      if (draftKey) title = localStorage.getItem(draftKey) ?? "";
    } catch {}
    error = "";
    // Flush bindings before opening, then focus within the original tap handler.
    // Deferring focus can lose the iOS software-keyboard activation.
    flushSync();
    dialog.showModal();
    input.focus({ preventScroll: true });
  }
  async function create(openAfter = false) {
    if (busy) return;
    busy = true;
    error = "";
    try {
      const t = await createTask(workspace, title, {
        parent_id: parent,
        board: parent ? undefined : (config.board ?? "tasks"),
      });
      title = "";
      saveDraft();
      dialog.close();
      taskChanged();
      if (openAfter) oncreated(t);
    } catch (e) {
      error = errorMessage(e);
    } finally {
      busy = false;
    }
  }
</script>

<dialog
  bind:this={dialog}
  class="management-dialog create-dialog"
  aria-label={parent ? "Create subtask" : "Create task"}
  oncancel={(e) => {
    if (busy) e.preventDefault();
  }}
>
  <header>
    <span class="mobile-scope"
      >{currentWorkspace?.workspace?.name ?? config.prefix} · {config.board ===
      "backlog"
        ? "Backlog"
        : "Tasks"}</span
    >
    <h2>{parent ? "Create subtask" : "Create task"}</h2>
  </header>
  <p class="hint desktop-hint">
    {#if parent}Starts on its parent’s board.{:else}Starts in {config.statuses.find(
        (s) => s.id === config.creation_status,
      )?.name}.{/if} You can add the details next.
  </p>
  <form
    onsubmit={(e) => {
      e.preventDefault();
      void create((e.submitter as HTMLButtonElement | null)?.value === "open");
    }}
  >
    <div class="compose-area">
      <div class="field">
        <label for={`${id}-title`}>Title</label><input
          id={`${id}-title`}
          bind:this={input}
          bind:value={title}
          oninput={(event) => {
            title = event.currentTarget.value;
            saveDraft();
          }}
          maxlength="300"
          placeholder={parent ? "Subtask title…" : "Task title…"}
          enterkeyhint="done"
          required
          disabled={busy}
        />
      </div>
      {#if error}<p class="notice error" role="alert">{error}</p>{/if}
      <div class="management-actions">
        <button
          type="button"
          class="secondary cancel-button"
          disabled={busy}
          aria-label="Cancel"
          onclick={() => dialog.close()}
          ><span class="cancel-label">Cancel</span><svg
            class="cancel-icon"
            viewBox="0 0 24 24"
            width="20"
            height="20"
            fill="none"
            stroke="currentColor"
            stroke-width="1.8"
            aria-hidden="true"><path d="m6 6 12 12M18 6 6 18" /></svg
          ></button
        >
        <div class="create-actions">
          <button
            class="primary"
            type="submit"
            value="create"
            disabled={busy || !title.trim()}
            >{busy ? "Creating…" : "Create task"}</button
          >
          <button
            class="secondary"
            type="submit"
            value="open"
            disabled={busy || !title.trim()}>Create and open</button
          >
        </div>
      </div>
    </div>
  </form>
</dialog>

<style>
  .create-actions {
    display: flex;
    gap: 12px;
  }
  .mobile-scope,
  .cancel-icon {
    display: none;
  }
  @media (max-width: 759px) {
    .create-dialog {
      position: fixed;
      top: calc(
        var(--mobile-viewport-top, 0px) + var(--mobile-viewport-height, 100dvh)
      );
      transform: translateY(-100%);
      left: 0;
      right: 0;
      bottom: auto;
      width: 100%;
      max-width: none;
      height: auto;
      max-height: calc(var(--mobile-viewport-height, 100dvh) - 16px);
      margin: 0;
      padding: 0;
      border: 0;
      border-radius: 20px 20px 0 0;
      overflow-y: auto;
      box-sizing: border-box;
    }
    .create-dialog[open] {
      display: flex;
      flex-direction: column;
    }
    header {
      padding: 20px 20px 4px;
      flex-shrink: 0;
    }
    .mobile-scope {
      display: block;
      color: var(--muted);
      font-size: 12px;
      margin-bottom: 8px;
    }
    .create-dialog h2 {
      margin: 0;
      font-size: 20px;
    }
    .desktop-hint {
      display: none;
    }
    form {
      display: flex;
      flex-direction: column;
      flex: 1;
      min-height: 0;
    }
    .compose-area {
      display: grid;
      grid-template-columns: minmax(0, 1fr) auto;
      gap: 12px;
      padding: 16px max(16px, env(safe-area-inset-right, 0px))
        max(16px, env(safe-area-inset-bottom, 0px))
        max(16px, env(safe-area-inset-left, 0px));
      flex-shrink: 0;
    }
    .field {
      grid-column: 1;
      grid-row: 1;
      margin: 0;
      min-width: 0;
    }
    .field label {
      position: absolute;
      width: 1px;
      height: 1px;
      overflow: hidden;
      clip-path: inset(50%);
    }
    .field input {
      width: 100%;
      box-sizing: border-box;
      height: 48px;
      font-size: 16px;
      margin: 0;
      border-radius: 12px;
    }
    .management-actions {
      display: contents;
    }
    .cancel-button {
      grid-column: 2;
      grid-row: 1;
      width: 48px;
      height: 48px;
      padding: 0;
      display: flex;
      align-items: center;
      justify-content: center;
    }
    .cancel-label {
      display: none;
    }
    .cancel-icon {
      display: block;
    }
    .create-actions {
      grid-column: 1 / -1;
      display: grid;
      grid-template-columns: 1fr 1fr;
      gap: 10px;
    }
    .create-actions button {
      min-height: 48px;
      border-radius: 12px;
      padding-inline: 8px;
    }
    .notice {
      grid-column: 1 / -1;
      grid-row: 2;
      margin: 0;
    }
  }
</style>
