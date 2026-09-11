<script lang="ts">
  import { useAccount } from "$lib/account-context";
  import { tick } from "svelte";
  import { errorMessage } from "$lib/api";
  import { useWorkspace } from "$lib/workspaces";
  import { propertyOptions, taskProperties } from "$lib/task-properties";
  import {
    createTask,
    taskChanged,
    boardStatuses,
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
  let status = $state("");
  let properties = $state({ priority: "none", type: "none", size: "none" });
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
  export async function open(p = "") {
    if (busy) return;
    parent = p;
    status = "";
    properties = { priority: "none", type: "none", size: "none" };
    draftKey = account?.account.id
      ? `acta.task-draft:${account.account.id}:${workspace}:${config.board ?? "tasks"}:${p}`
      : "";
    title = "";
    try {
      if (draftKey) title = localStorage.getItem(draftKey) ?? "";
    } catch {}
    error = "";
    dialog.showModal();
    await tick();
    input.focus();
  }
  async function create(openAfter = false) {
    if (busy) return;
    busy = true;
    error = "";
    try {
      const t = await createTask(workspace, title, {
        parent_id: parent,
        board: parent ? undefined : (config.board ?? "tasks"),
        ...(status ? { status_id: status } : {}),
        ...properties,
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
    <div class="mobile-details">
      <p class="hint">
        Give it a title to get started. Everything else is optional.
      </p>
      {#if !parent}
        <label class="detail-field" for={`${id}-status`}>
          <span>Status</span>
          <select
            id={`${id}-status`}
            aria-label="Status"
            bind:value={status}
            disabled={busy}
          >
            <option value=""
              >{config.statuses.find((s) => s.id === config.creation_status)
                ?.name ?? "Default"}</option
            >
            {#each boardStatuses(config).filter((s) => s.id !== config.creation_status) as option}
              <option value={option.id}>{option.name}</option>
            {/each}
          </select>
        </label>
      {/if}
      {#each taskProperties as property}
        <label class="detail-field" for={`${id}-${property.value}`}>
          <span>{property.label}</span>
          <select
            id={`${id}-${property.value}`}
            aria-label={property.label}
            bind:value={properties[property.value]}
            disabled={busy}
          >
            {#each propertyOptions(property.value) as option}
              <option value={option.value}>{option.label}</option>
            {/each}
          </select>
        </label>
      {/each}
    </div>
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
  .mobile-details,
  .mobile-scope,
  .cancel-icon {
    display: none;
  }
  @media (max-width: 759px) {
    .create-dialog {
      position: fixed;
      top: var(--mobile-viewport-top, 0px);
      left: 0;
      right: 0;
      bottom: auto;
      width: 100%;
      max-width: none;
      height: var(--mobile-viewport-height, 100dvh);
      max-height: none;
      margin: 0;
      padding: 0;
      border: 0;
      border-radius: 0;
      overflow: hidden;
      box-sizing: border-box;
    }
    .create-dialog[open] {
      display: flex;
      flex-direction: column;
    }
    header {
      padding: calc(20px + env(safe-area-inset-top, 0px)) 24px 8px;
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
      font-size: 24px;
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
    .mobile-details {
      display: block;
      flex: 1;
      min-height: 0;
      overflow-y: auto;
      overscroll-behavior: contain;
      padding: 0 24px 24px;
    }
    .hint {
      font-size: 13px;
      line-height: 1.6;
      color: var(--muted);
      margin: 4px 0 20px;
    }
    .detail-field {
      display: flex;
      align-items: center;
      justify-content: space-between;
      gap: 16px;
      min-height: 56px;
      border-bottom: 1px solid var(--panel-border);
      font-size: 14px;
    }
    .detail-field > span {
      color: var(--muted);
    }
    select {
      width: 60%;
      min-height: 44px;
      font-size: 16px;
      background: transparent;
      color: var(--text);
      border: 0;
      padding: 8px;
      text-align: right;
    }
    .compose-area {
      display: grid;
      grid-template-columns: minmax(0, 1fr) auto;
      gap: 12px;
      padding: 16px max(16px, env(safe-area-inset-right, 0px))
        max(16px, env(safe-area-inset-bottom, 0px))
        max(16px, env(safe-area-inset-left, 0px));
      border-top: 1px solid var(--panel-border);
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
