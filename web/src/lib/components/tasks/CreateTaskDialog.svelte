<script lang="ts">
  import { useAccount } from "$lib/account-context";
  import { tick } from "svelte";
  import { errorMessage } from "$lib/api";
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
    parent = p;
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
  async function create() {
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
      oncreated(t);
    } catch (e) {
      error = errorMessage(e);
    } finally {
      busy = false;
    }
  }
</script>

<dialog
  bind:this={dialog}
  class="management-dialog"
  aria-label={parent ? "Create subtask" : "Create task"}
  oncancel={(e) => {
    if (busy) e.preventDefault();
  }}
>
  <h2>{parent ? "Create subtask" : "Create task"}</h2>
  <p class="hint">
    {#if parent}Starts on its parent’s board.{:else}Starts in {config.statuses.find(
        (s) => s.id === config.creation_status,
      )?.name}.{/if} You can add the details next.
  </p>
  <form
    onsubmit={(e) => {
      e.preventDefault();
      void create();
    }}
  >
    <div class="field">
      <label for="new-task-title">Title</label><input
        id="new-task-title"
        bind:this={input}
        bind:value={title}
        oninput={(event) => {
          title = event.currentTarget.value;
          saveDraft();
        }}
        maxlength="300"
        required
        disabled={busy}
      />
    </div>
    {#if error}<p class="notice error" role="alert">{error}</p>{/if}
    <div class="management-actions">
      <button
        type="button"
        class="secondary"
        disabled={busy}
        onclick={() => dialog.close()}>Cancel</button
      ><button class="primary" disabled={busy || !title.trim()}
        >{busy ? "Creating…" : "Create task"}</button
      >
    </div>
  </form>
</dialog>
