<script lang="ts">
  import TaskViewTabs from "$lib/components/tasks/TaskViewTabs.svelte";
  import TaskTree from "$lib/components/tasks/TaskTree.svelte";
  import { provideAccount } from "$lib/account-context";
  import { defaultViewSettings } from "$lib/task-views.js";
  import type { Account } from "$lib/api";
  import type { TaskConfig } from "$lib/tasks";
  provideAccount({ account: { id: "review" } as Account, update: () => {} });
  let archived = $state(false),
    settings = $state(defaultViewSettings()),
    view = $state("");
  const config: TaskConfig = {
    prefix: "QA",
    previous_prefixes: [],
    statuses: [
      { id: "todo", name: "To do" },
      { id: "done", name: "Done" },
    ],
    creation_status: "todo",
    completed_status: "done",
    version: 1,
    revision: 1,
  };
</script>

<div class="fixture">
  <h1>{archived ? "Archived tasks" : "Tasks"}</h1>
  <TaskViewTabs
    workspace="review"
    {config}
    panelID="results"
    bind:archived
    onchange={(v) => (settings = v)}
    onviewchange={(v) => (view = v)}
    onlabelchange={() => {}}
  />
  <div data-testid="state">{JSON.stringify({ archived, settings, view })}</div>
  <TaskTree
    workspace="review"
    {config}
    {archived}
    completion="all"
    display={settings.display}
    statuses={settings.filters.statuses}
    onopen={() => {}}
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
    --danger: #ffaaaa;
  }
  :global(button),
  :global(input) {
    font: inherit;
    color: inherit;
  }
  :global(button) {
    cursor: pointer;
  }
  .fixture {
    padding: 28px;
    container-type: inline-size;
    container-name: tasks-list;
  }
  [data-testid="state"] {
    display: none;
  }
</style>
