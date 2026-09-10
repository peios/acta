<script lang="ts">
  import TaskViewTabs from "$lib/components/tasks/TaskViewTabs.svelte";
  import TaskTree from "$lib/components/tasks/TaskTree.svelte";
  import TaskStatusPicker from "$lib/components/tasks/TaskStatusPicker.svelte";
  import CreateTaskDialog from "$lib/components/tasks/CreateTaskDialog.svelte";
  import { provideAccount } from "$lib/account-context";
  import { defaultViewSettings } from "$lib/task-views.js";
  import type { Account } from "$lib/api";
  import { boardConfig, boardStatuses, type TaskConfig } from "$lib/tasks";
  provideAccount({ account: { id: "review" } as Account, update: () => {} });
  let board = $state("tasks"),
    archived = $state(false),
    settings = $state(defaultViewSettings()),
    status = $state("todo");
  let create = $state<CreateTaskDialog>();
  const config: TaskConfig = {
    prefix: "QA",
    previous_prefixes: [],
    version: 1,
    revision: 1,
    creation_status: "todo",
    completed_status: "done",
    boards: [
      {
        slug: "tasks",
        name: "Tasks",
        creation_status: "todo",
        completed_status: "done",
      },
      {
        slug: "backlog",
        name: "Backlog",
        creation_status: "ideas",
        completed_status: "",
      },
    ],
    statuses: [
      { id: "todo", name: "To do", board: "tasks" },
      { id: "done", name: "Done", board: "tasks" },
      { id: "ideas", name: "Ideas", board: "backlog" },
    ],
  };
  const scoped = $derived(boardConfig(config, board));
</script>

<div class="fixture">
  <nav>
    <button onclick={() => (board = "tasks")}>Tasks board</button><button
      onclick={() => (board = "backlog")}>Backlog board</button
    >
  </nav>
  <h1>{board === "tasks" ? "Tasks" : "Backlog"}</h1>
  <button onclick={() => create?.open()}>Create task</button>
  {#key board}<TaskViewTabs
      {board}
      workspace="review"
      config={{ ...scoped, statuses: boardStatuses(scoped) }}
      panelID="results"
      bind:archived
      onchange={(v) => (settings = v)}
      onviewchange={() => {}}
      onlabelchange={() => {}}
    />{/key}
  <TaskTree
    workspace="review"
    config={scoped}
    {archived}
    completion="all"
    display={settings.display}
    statuses={settings.filters.statuses}
    onopen={() => {}}
  />
  <h2>Task status</h2>
  <TaskStatusPicker
    value={status}
    {config}
    disabled={false}
    onchange={(v) => (status = v)}
  />
  <CreateTaskDialog
    bind:this={create}
    workspace="review"
    config={scoped}
    oncreated={() => {}}
  />
</div>

<style>
  :global(body) {
    margin: 0;
    background: #202020;
    color: #e8e8e8;
    font-family: system-ui;
    --surface: #262626;
    --muted: #a6a6a6;
    --text: #e8e8e8;
    --panel-border: #383838;
    --accent: #bccadd;
    --hover-surface: #303030;
    --danger: #faa;
    --border: #383838;
    --success-text: #81b599;
    --success-surface: #24362d;
  }
  :global(button),
  :global(input) {
    font: inherit;
    color: inherit;
  }
  :global(button) {
    cursor: pointer;
  }
  nav {
    display: flex;
    gap: 12px;
  }
  nav button {
    padding: 8px 14px;
    border: 1px solid #555;
    border-radius: 8px;
    background: #292929;
  }
  .fixture {
    padding: 28px;
    container-type: inline-size;
    container-name: tasks-list;
  }
</style>
