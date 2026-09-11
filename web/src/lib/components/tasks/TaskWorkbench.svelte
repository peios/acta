<script lang="ts">
  import { defaultViewDisplay, type ViewDisplay } from "$lib/task-views.js";
  import {
    defaultTableLayout,
    readTableLayout,
    saveTableLayout,
    type TableLayout,
  } from "$lib/task-table-layout.js";
  import { isTaskProperty } from "$lib/task-properties";
  import { createTaskFeed } from "$lib/task-feed.js";
  import { provideTaskRefresh } from "$lib/task-refresh-context";
  import { LatestRequest } from "$lib/requests.js";
  import { mergeTask } from "$lib/task-revisions.js";
  import { onMount, untrack } from "svelte";
  import { goto } from "$app/navigation";
  import { page } from "$app/state";
  import { api, APIError, errorMessage } from "$lib/api";
  import { useWorkspace, canWorkspace, workspacePath } from "$lib/workspaces";
  import {
    boardConfig,
    boardStatuses,
    type Task,
    type TaskConfig,
  } from "$lib/tasks";
  import TaskBoard from "./TaskBoard.svelte";
  import TaskTree from "./TaskTree.svelte";
  import TaskViewTabs from "./TaskViewTabs.svelte";
  import TaskViewer from "./TaskViewer.svelte";
  import CreateTaskDialog from "./CreateTaskDialog.svelte";
  import TaskListToolbar from "./TaskListToolbar.svelte";
  import "$lib/components/management/management.css";
  const current = useWorkspace();
  let assignmentGroups = $state<import("$lib/task-groups.js").TaskGroup[]>([]);
  let groupError = $state("");
  let groupsLoading = $state(false);
  const hover = $state({ id: "" });
  $effect(() => {
    void display.group;
    void display.mode;
    hover.id = "";
  });
  let groupKey = "";
  $effect(() => {
    const workspace = current.workspace?.id;
    const group = display.group;
    void config?.revision;
    let cancelled = false;
    if (groupKey !== `${workspace}:${group}`) assignmentGroups = [];
    groupKey = `${workspace}:${group}`;
    groupError = "";
    const needsGroups =
      !!workspace &&
      (group === "assignee" || group === "agents" || isTaskProperty(group));
    groupsLoading = needsGroups;
    if (needsGroups) {
      api<{ groups: import("$lib/task-groups.js").TaskGroup[] }>(
        `workspaces/${workspace}/task-groups?group=${group}`,
      )
        .then((r) => {
          if (!cancelled) assignmentGroups = r.groups;
        })
        .catch((e) => {
          if (!cancelled) groupError = errorMessage(e);
        })
        .finally(() => {
          if (!cancelled) groupsLoading = false;
        });
    }
    return () => {
      cancelled = true;
    };
  });
  let config = $state<TaskConfig | null>(null),
    task = $state<Task | null>(null),
    error = $state(""),
    taskError = $state(""),
    search = $state(""),
    mounted = $state(false);
  let recovery = $state(0);
  provideTaskRefresh({
    get error() {
      return error;
    },
    get recovery() {
      return recovery;
    },
  });
  let archived = $state(false);
  let activeSearch = "";
  let wasArchived = false;
  $effect(() => {
    if (archived) {
      activeSearch = untrack(() => search);
      wasArchived = true;
      search = "";
    } else if (wasArchived) {
      search = activeSearch;
      activeSearch = "";
      wasArchived = false;
    }
  });
  let display = $state<ViewDisplay>(defaultViewDisplay());
  let activeViewID = $state("");
  let tableLayout = $state<TableLayout>(defaultTableLayout());
  function changeTableLayout(layout: TableLayout, commit: boolean) {
    tableLayout = layout;
    if (commit && current.workspace && activeViewID)
      saveTableLayout(current.workspace.id, activeViewID, layout);
  }
  let create = $state<CreateTaskDialog>();
  let viewTabs = $state<TaskViewTabs>();
  let workbench: HTMLDivElement;
  let available = $state(0),
    listWidth = $state(0),
    mode = $state("modal");
  const searchID = $props.id();
  const filterID = `${searchID}-completion`;
  let priorities = $state<string[]>([]),
    types = $state<string[]>([]),
    sizes = $state<string[]>([]);
  let statusFilters = $state<string[]>([]);
  let assigneeFilters = $state<string[]>([]);
  let unassignedFilter = $state(false);
  let viewLabelID = $state("");
  const taskReads = new LatestRequest();
  let feed: ReturnType<typeof createTaskFeed> | undefined;
  const board = $derived(
    page.url.searchParams.get("board") === "backlog" ? "backlog" : "tasks",
  );
  const scopedConfig = $derived(config ? boardConfig(config, board) : null);
  $effect(() => {
    void board;
    search = "";
    archived = false;
    activeViewID = "";
  });
  const selected = $derived(page.url.searchParams.get("task") || "");
  async function loadTask() {
    const id = selected;
    const read = taskReads.begin();
    if (task?.id !== id) task = null;
    if (!id) {
      task = null;
      taskError = "";
      return;
    }
    try {
      const t = await api<Task>("tasks/" + encodeURIComponent(id), undefined, {
        signal: read.signal,
      });
      if (!read.current()) return;
      if (t.workspace_id !== current.workspace?.id)
        throw new Error("This task belongs to another workspace.");
      task = mergeTask(task, t);
      taskError = "";
    } catch (e) {
      if (read.current()) {
        taskError = errorMessage(e);
        if (e instanceof APIError && [401, 403, 404].includes(e.status))
          task = null;
      }
    }
  }
  function open(t: Task) {
    void goto(
      workspacePath(current.workspace!) +
        "?board=" +
        board +
        "&task=" +
        encodeURIComponent(t.id),
      { noScroll: true, keepFocus: true },
    );
  }
  function close() {
    void goto(workspacePath(current.workspace!) + "?board=" + board, {
      noScroll: true,
      keepFocus: true,
    });
  }
  onMount(() => {
    mounted = true;
    const observer = new ResizeObserver((entries) => {
      available = entries[0].contentRect.width;
    });
    observer.observe(workbench);
    feed = createTaskFeed({
      workspace: current.workspace!.id,
      request: api,
      onConfig: (next: TaskConfig, changed: boolean) => {
        config = next;
        if (changed) void loadTask();
      },
      onError: (e: unknown) => {
        if (!e && error) recovery += 1;
        error = e ? errorMessage(e) : "";
      },
      onAccessLost: () =>
        window.dispatchEvent(new Event("acta:workspaces-changed")),
    });
    const refresh = () => {
      void feed?.refresh();
      void loadTask();
    };
    window.addEventListener("acta:tasks-changed", refresh);
    window.addEventListener("focus", refresh);
    return () => {
      observer.disconnect();
      feed?.stop();
      taskReads.dispose();
      window.removeEventListener("acta:tasks-changed", refresh);
      window.removeEventListener("focus", refresh);
    };
  });
  $effect(() => {
    void selected;
    if (mounted) untrack(() => void loadTask());
  });
</script>

<div
  bind:this={workbench}
  class="workbench"
  class:with-panel={selected && mode === "panel"}
  class:full={selected && mode === "full"}
>
  <div class="list" bind:clientWidth={listWidth}>
    <TaskListToolbar
      title={board === "backlog" ? "Backlog" : "Tasks"}
      {archived}
      bind:search
      width={listWidth}
      taskOpen={!!selected}
      canCreate={!archived &&
        canWorkspace(current.workspace, "tasks.create") &&
        !!config}
      oncreate={() => create?.open()}
    />
    {#if config && current.workspace}
      {#key `${current.workspace.id}:${board}`}<TaskViewTabs
          {board}
          bind:this={viewTabs}
          bind:archived
          workspace={current.workspace.id}
          config={{ ...scopedConfig!, statuses: boardStatuses(scopedConfig!) }}
          panelID={`${filterID}-panel`}
          onchange={(settings) => {
            const filters = settings.filters;
            display = settings.display;
            priorities = filters.priorities ?? [];
            types = filters.types ?? [];
            sizes = filters.sizes ?? [];
            statusFilters = filters.statuses;
            assigneeFilters = filters.assignees;
            unassignedFilter = filters.unassigned;
          }}
          onviewchange={(id) => {
            activeViewID = id;
            tableLayout = readTableLayout(current.workspace!.id, id);
          }}
          onlabelchange={(id) => (viewLabelID = id)}
        />{/key}
    {/if}
    <div
      role="tabpanel"
      id={`${filterID}-panel`}
      aria-label={archived ? "Archived tasks" : undefined}
      aria-labelledby={archived ? undefined : viewLabelID || undefined}
      tabindex="0"
      class="task-results"
    >
      {#if groupError && groupError !== error}<p
          class="notice error"
          role="alert"
        >
          {groupError}
        </p>{/if}
      {#if groupsLoading}<p class="hint" role="status">Loading groups…</p>{/if}
      {#if error}<p class="notice error" role="alert">
          {error}
          <button class="secondary" onclick={() => void feed?.refresh()}
            >Retry</button
          >
        </p>{/if}{#if config && current.workspace && activeViewID}{#key `${current.workspace.id}:${activeViewID}`}
          {#if display.mode === "board"}<TaskBoard
              {archived}
              workspace={current.workspace.id}
              {assignmentGroups}
              {hover}
              config={scopedConfig!}
              {display}
              {priorities}
              {types}
              {sizes}
              statuses={statusFilters}
              assignees={assigneeFilters}
              unassigned={unassignedFilter}
              query={search}
              onopen={open}
              canEdit={!archived &&
                canWorkspace(current.workspace, "tasks.edit")}
              canCreate={!archived &&
                canWorkspace(current.workspace, "tasks.create")}
            />
          {:else}<TaskTree
              {archived}
              workspace={current.workspace.id}
              {assignmentGroups}
              {hover}
              config={scopedConfig!}
              canEdit={!archived &&
                canWorkspace(current.workspace, "tasks.edit")}
              completion="all"
              layout={tableLayout}
              onlayoutchange={changeTableLayout}
              onsort={(field) => viewTabs?.sortBy(field)}
              {display}
              {priorities}
              {types}
              {sizes}
              statuses={statusFilters}
              assignees={assigneeFilters}
              unassigned={unassignedFilter}
              query={search}
              revision={config.revision}
              onopen={open}
            />{/if}{/key}{/if}
    </div>
  </div>
  <TaskViewer
    {selected}
    {task}
    {config}
    workspace={current.workspace}
    {taskError}
    {available}
    onlayout={(value) => (mode = value)}
    onchange={(t) => {
      if (t.id === selected) task = mergeTask(task, t);
    }}
    onopen={open}
    oncreate={(p) => create?.open(p)}
    onclose={close}
    onretry={() => void loadTask()}
  />
</div>
{#if config && current.workspace}<CreateTaskDialog
    bind:this={create}
    workspace={current.workspace.id}
    config={scopedConfig!}
    oncreated={open}
  />{/if}

<style>
  .workbench {
    position: relative;
    min-height: 100%;
    display: flex;
    gap: 28px;
  }
  .list {
    flex: 1;
    min-width: 0;
    container-type: inline-size;
    container-name: tasks-list;
  }
  .task-results:focus-visible {
    outline: 2px solid var(--accent);
    outline-offset: 4px;
    border-radius: 3px;
  }
  .full > .list {
    display: none;
  }
  @media (max-width: 759px) {
    .full > .list {
      display: block;
    }
  }
</style>
