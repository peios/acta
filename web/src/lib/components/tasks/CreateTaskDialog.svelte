<script lang="ts">
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
  let dialog: HTMLDialogElement;
  let title = $state(""),
    parent = $state(""),
    error = $state(""),
    busy = $state(false);
  let input: HTMLInputElement;
  export async function open(p = "") {
    parent = p;
    title = "";
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
