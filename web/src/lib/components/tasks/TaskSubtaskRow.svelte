<script lang="ts">
  import { errorMessage } from "$lib/api";
  import {
    personName,
    setTaskStatus,
    taskChanged,
    type Task,
    type TaskConfig,
  } from "$lib/tasks";
  import TaskStatusPicker from "./TaskStatusPicker.svelte";
  import TaskTextField from "./TaskTextField.svelte";

  let {
    task,
    config,
    editable,
    onopen,
    onsaved,
  }: {
    task: Task;
    config: TaskConfig;
    editable: boolean;
    onopen: (task: Task) => void;
    onsaved: (task: Task) => void;
  } = $props();
  let saving = $state(false),
    error = $state("");
  async function changeStatus(value: string) {
    if (!editable || saving) return;
    saving = true;
    error = "";
    try {
      const updated = await setTaskStatus(task, value);
      onsaved(updated);
      taskChanged();
    } catch (e) {
      error = errorMessage(e);
    } finally {
      saving = false;
    }
  }
</script>

<div class="subtask-row">
  <TaskStatusPicker
    value={task.status_id}
    {config}
    compact
    label={`Status of ${task.reference}`}
    disabled={!editable || saving}
    onchange={(id) => void changeStatus(id)}
  />
  <span class="reference">{task.reference}</span>
  <div class="title">
    <TaskTextField {task} field="title" {editable} compact {onsaved} />
  </div>
  <div class="assignees" aria-label="Assignees">
    {#each task.assignees.slice(0, 3) as person}
      <span
        class="avatar"
        title={`${personName(person)}${!person.available ? " · Access removed" : ""}`}
      >
        {personName(person).slice(0, 1).toUpperCase()}
      </span>
    {/each}
    {#if task.assignees.length > 3}<span
        class="count"
        title={task.assignees.slice(3).map(personName).join(", ")}
        >+{task.assignees.length - 3}</span
      >{/if}
  </div>
  <button
    class="open-task"
    aria-label={`Open ${task.reference}`}
    title="Open task"
    onclick={() => onopen(task)}
  >
    <svg viewBox="0 0 20 20" aria-hidden="true"><path d="m7 4 6 6-6 6" /></svg>
  </button>
</div>
{#if error}<p class="notice error" role="alert">{error}</p>{/if}

<style>
  .subtask-row {
    display: flex;
    align-items: center;
    gap: 10px;
    min-height: 62px;
    padding: 10px 0;
    border-bottom: 1px solid var(--panel-border);
  }
  .reference {
    color: var(--muted);
    font-size: 11px;
    white-space: nowrap;
  }
  .title {
    flex: 1;
    min-width: 0;
  }
  .assignees {
    display: flex;
    align-items: center;
    padding-left: 4px;
  }
  .avatar {
    display: grid;
    place-items: center;
    width: 26px;
    height: 26px;
    border-radius: 50%;
    background: var(--hover-surface);
    border: 2px solid var(--surface);
    margin-left: -4px;
    font-size: 10px;
  }
  .count {
    color: var(--muted);
    font-size: 10px;
    margin-left: 3px;
  }
  .open-task {
    display: grid;
    place-items: center;
    width: 32px;
    height: 36px;
    flex-shrink: 0;
    border: 0;
    border-radius: 8px;
    padding: 0;
    background: transparent;
    color: var(--muted);
  }
  .open-task:hover {
    background: var(--hover-surface);
    color: var(--text);
  }
  svg {
    width: 16px;
    height: 16px;
    fill: none;
    stroke: currentColor;
    stroke-width: 1.5;
    stroke-linecap: round;
    stroke-linejoin: round;
  }
  @media (max-width: 600px) {
    .subtask-row {
      gap: 6px;
    }
  }
</style>
