<script lang="ts">
  import { onMount } from "svelte";
  import TaskTree from "$lib/components/tasks/TaskTree.svelte";
  import TaskBoard from "$lib/components/tasks/TaskBoard.svelte";
  import TaskDisplayMenu from "$lib/components/tasks/TaskDisplayMenu.svelte";
  import TaskFilterMenu from "$lib/components/tasks/TaskFilterMenu.svelte";
  import TaskStatusPicker from "$lib/components/tasks/TaskStatusPicker.svelte";
  import { defaultViewDisplay } from "$lib/task-views.js";
  import { isTaskProperty, propertyOptions } from "$lib/task-properties";
  import type { TaskConfig } from "$lib/tasks";
  let display = $state({
    ...defaultViewDisplay(),
    group: "priority",
    columns: ["priority", "type", "size"],
  });
  let priorities = $state<string[]>([]),
    types = $state<string[]>([]),
    sizes = $state<string[]>([]);
  let config = $state<TaskConfig>({
    prefix: "QA",
    previous_prefixes: [],
    statuses: [{ id: "todo", name: "To do" }],
    creation_status: "todo",
    completed_status: "done",
    version: 1,
    revision: 1,
  });
  const groups = $derived(
    isTaskProperty(display.group)
      ? propertyOptions(display.group).map((o) => ({
          id: o.value,
          name: o.label,
          assign_id: "",
          available: true,
        }))
      : [],
  );
  onMount(() => {
    const refresh = () => config.revision++;
    window.addEventListener("acta:tasks-changed", refresh);
    return () => window.removeEventListener("acta:tasks-changed", refresh);
  });
</script>

<div class="fixture">
  <header>
    <TaskStatusPicker
      value="todo"
      {config}
      disabled={false}
      onchange={() => {}}
    />
    <TaskFilterMenu
      workspace="review"
      {config}
      bind:priorities
      bind:types
      bind:sizes
    /><TaskDisplayMenu bind:display />
  </header>
  {#if display.mode === "board"}<TaskBoard
      workspace="review"
      {config}
      {display}
      {priorities}
      {types}
      {sizes}
      statuses={[]}
      assignees={[]}
      unassigned={false}
      query=""
      onopen={() => {}}
      hover={{ id: "" }}
      canEdit
      canCreate
      assignmentGroups={groups}
    />
  {:else}<TaskTree
      workspace="review"
      {config}
      {display}
      {priorities}
      {types}
      {sizes}
      completion="all"
      assignmentGroups={groups}
      revision={config.revision}
      onopen={() => {}}
      onsort={(field) =>
        (display = {
          ...display,
          sort: field,
          direction:
            display.sort === field && display.direction === "asc"
              ? "desc"
              : "asc",
        })}
    />{/if}
</div>

<style>
  :global(body) {
    margin: 0;
    background: #202020;
    color: #eee;
    font-family: system-ui;
    --text: #eee;
    --muted: #aaa;
    --surface: #292929;
    --hover-surface: #333;
    --panel-border: #3b3b3b;
    --accent: #b8c8df;
    --scope-active: #343f50;
  }
  :global(button) {
    font-family: inherit;
    cursor: pointer;
  }
  .fixture {
    padding: 24px;
  }
  header {
    display: flex;
    justify-content: flex-end;
    gap: 8px;
    margin-bottom: 20px;
  }
</style>
