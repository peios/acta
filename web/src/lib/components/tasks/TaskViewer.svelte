<script lang="ts">
  import DetailViewer from "$lib/components/DetailViewer.svelte";
  import { api, errorMessage } from "$lib/api";
  import { taskChanged } from "$lib/tasks";
  import TaskFollow from "./TaskFollow.svelte";
  import TaskDetail from "./TaskDetail.svelte";
  import { canWorkspace, type Workspace } from "$lib/workspaces";
  import type { Task, TaskConfig } from "$lib/tasks";
  import "$lib/components/management/management.css";
  let {
    selected,
    focusComment = "",
    task,
    config,
    workspace,
    taskError = "",
    available,
    embedded = false,
    onchange,
    onopen,
    oncreate,
    onclose,
    onretry,
    onlayout = () => {},
  }: {
    selected: string;
    focusComment?: string;
    task: Task | null;
    config: TaskConfig | null;
    workspace: Workspace | null;
    taskError?: string;
    available: number;
    embedded?: boolean;
    onchange: (task: Task) => void;
    onopen: (task: Task) => void;
    oncreate: (parent: string) => void;
    onclose: () => void;
    onretry: () => void;
    onlayout?: (mode: string) => void;
  } = $props();
  let taskDetail = $state<TaskDetail>();
  let taskMenu = $state<HTMLDetailsElement>();
  let menuOpen = $state(false);
  let archiving = $state(false),
    archiveError = $state("");
  $effect(() => {
    void selected;
    archiveError = "";
  });
  async function toggleArchive() {
    if (!task || archiving) return;
    const target = task;
    archiving = true;
    archiveError = "";
    menuOpen = false;
    try {
      const saved = await api<Task>(`tasks/${target.id}/archive`, {
        archived: !target.archived,
        version: target.versions.archived,
      });
      if (selected === target.id) onchange(saved);
      taskChanged();
    } catch (e) {
      if (selected === target.id) {
        archiveError = errorMessage(e);
        onretry();
      }
    } finally {
      archiving = false;
    }
  }
  function close() {
    menuOpen = false;
    onclose();
  }
</script>

<svelte:window
  onpointerdown={(event) => {
    if (menuOpen && !taskMenu?.contains(event.target as Node)) menuOpen = false;
  }}
  onkeydown={(event) => {
    if (event.key === "Escape" && menuOpen) {
      event.preventDefault();
      event.stopPropagation();
      menuOpen = false;
      taskMenu?.querySelector("summary")?.focus();
    }
  }}
/>
<DetailViewer
  {selected}
  {available}
  {embedded}
  {onlayout}
  onclose={close}
  title={task?.reference || "Task"}
  label="task"
  storageKey="acta.task"
>
  {#snippet actions()}
    {#if task}{#key task.id}<TaskFollow task={task.id} />{/key}{/if}
    {#if task && canWorkspace(workspace, "tasks.edit")}
      <details class="task-menu" bind:this={taskMenu} bind:open={menuOpen}>
        <summary
          class="view-control"
          aria-label="Task actions"
          title="Task actions"
        >
          <svg
            viewBox="0 0 24 24"
            width="20"
            height="20"
            fill="currentColor"
            aria-hidden="true"
            ><circle cx="12" cy="5" r="1.6" /><circle
              cx="12"
              cy="12"
              r="1.6"
            /><circle cx="12" cy="19" r="1.6" /></svg
          >
        </summary>
        <div class="task-menu-popup">
          {#if !task.archived}<button
              onclick={() => {
                menuOpen = false;
                void taskDetail?.openReparent();
              }}
            >
              <svg
                viewBox="0 0 24 24"
                width="16"
                height="16"
                fill="none"
                stroke="currentColor"
                stroke-width="1.5"
                stroke-linecap="round"
                stroke-linejoin="round"
                aria-hidden="true"
                ><path d="M6 4v10a4 4 0 0 0 4 4h10m-4-4 4 4-4 4" /><circle
                  cx="6"
                  cy="4"
                  r="2"
                /></svg
              >Reparent
            </button>{/if}
          <button disabled={archiving} onclick={() => void toggleArchive()}>
            <svg
              viewBox="0 0 24 24"
              width="16"
              height="16"
              fill="none"
              stroke="currentColor"
              stroke-width="1.5"
              aria-hidden="true"
              ><path d="M4 8h16v12H4zM3 4h18v4H3zM9 12h6" /></svg
            >
            {task.archived ? "Restore task" : "Archive task"}
          </button>
        </div>
      </details>
    {/if}
  {/snippet}

  {#if task?.archived}<div class="archive-banner" role="status">
      <div>
        <strong>Archived</strong>
        <p>Restore this task to edit it or add comments.</p>
      </div>
      {#if canWorkspace(workspace, "tasks.edit")}<button
          class="secondary"
          disabled={archiving}
          onclick={() => void toggleArchive()}
          >{archiving ? "Restoring…" : "Restore"}</button
        >{/if}
    </div>{/if}
  {#if archiveError}<p class="notice error" role="alert">{archiveError}</p>{/if}
  {#if taskError}<p class="notice error" role="alert">{taskError}</p>
    <button class="secondary" onclick={onretry}>Retry</button
    >{/if}{#if task && config && workspace}{#key task.id}<TaskDetail
        bind:this={taskDetail}
        {task}
        {focusComment}
        {config}
        {workspace}
        {onchange}
        {onopen}
        {oncreate}
      />{/key}{:else if !taskError}<p class="hint" role="status">
      Loading task…
    </p>{/if}
</DetailViewer>

<style>
  .archive-banner {
    display: flex;
    align-items: center;
    justify-content: space-between;
    gap: 16px;
    padding: 14px 16px;
    margin-bottom: 20px;
    border: 1px solid var(--panel-border);
    border-radius: 12px;
    background: var(--hover-surface);
  }
  .archive-banner p {
    margin: 4px 0 0;
    color: var(--muted);
    font-size: 12px;
  }
  .view-control {
    display: inline-flex;
    align-items: center;
    justify-content: center;
    min-width: 36px;
    min-height: 36px;
    background: transparent;
    border: 0;
    color: var(--muted);
    padding: 6px 8px;
    border-radius: 8px;
  }
  .view-control:hover {
    background: var(--hover-surface);
    color: var(--text);
  }
  .task-menu {
    position: relative;
  }
  .task-menu summary {
    list-style: none;
    cursor: pointer;
  }
  .task-menu summary::-webkit-details-marker {
    display: none;
  }
  .task-menu-popup {
    position: absolute;
    right: 0;
    top: calc(100% + 6px);
    z-index: 7;
    width: 180px;
    padding: 5px;
    border: 1px solid var(--panel-border);
    border-radius: 10px;
    background: var(--surface);
    box-shadow: 0 12px 32px #0003;
  }
  .task-menu-popup button {
    display: flex;
    align-items: center;
    gap: 10px;
    width: 100%;
    padding: 10px;
    border: 0;
    border-radius: 6px;
    background: transparent;
    color: var(--text);
    text-align: left;
    font-size: 13px;
  }
  .task-menu-popup button:hover {
    background: var(--hover-surface);
  }
</style>
