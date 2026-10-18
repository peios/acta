<script lang="ts">
  import TaskViewer from "./TaskViewer.svelte";
  import CreateTaskDialog from "./CreateTaskDialog.svelte";
  import { api, APIError, errorMessage } from "$lib/api";
  import type { Task, TaskConfig } from "$lib/tasks";
  import type { Workspace } from "$lib/workspaces";
  import { mergeTask } from "$lib/task-revisions.js";
  import { createTaskFeed } from "$lib/task-feed.js";
  import { provideTaskRefresh } from "$lib/task-refresh-context";
  import { LatestRequest } from "$lib/requests.js";
  let {
    selected,
    focusComment = "",
    onlayout = () => {},
    available,
    onopen,
    onclose,
  }: {
    selected: string;
    focusComment?: string;
    onlayout?: (mode: string) => void;
    available: number;
    onopen: (id: string) => void;
    onclose: () => void;
  } = $props();
  let task = $state<Task | null>(null);
  let workspace = $state<Workspace | null>(null);
  let config = $state<TaskConfig | null>(null);
  let taskError = $state(""),
    feedError = $state("");
  const error = $derived(taskError || feedError);
  let recovery = $state(0);
  let refresh = () => {};
  let create = $state<CreateTaskDialog>();
  provideTaskRefresh({
    get error() {
      return error;
    },
    get recovery() {
      return recovery;
    },
  });
  $effect(() => {
    const id = selected;
    task = null;
    workspace = null;
    config = null;
    taskError = "";
    feedError = "";
    const reads = new LatestRequest();
    let feed: ReturnType<typeof createTaskFeed> | undefined;
    let alive = true;
    async function load() {
      if (!id) return;
      const read = reads.begin();
      try {
        const next = await api<Task>(
          `tasks/${encodeURIComponent(id)}`,
          undefined,
          { signal: read.signal },
        );
        if (!read.current()) return;
        const scope = await api<Workspace>(
          `workspaces/${next.workspace_id}`,
          undefined,
          { signal: read.signal },
        );
        if (!read.current()) return;
        task = mergeTask(task, next);
        workspace = scope;
        if (taskError) recovery++;
        taskError = "";
        if (!feed)
          feed = createTaskFeed({
            workspace: scope.id,
            request: api,
            onConfig: (nextConfig, changed) => {
              if (!alive) return;
              const refreshTask = config !== null && changed;
              config = nextConfig;
              if (refreshTask) void load();
            },
            onError: (e) => {
              if (!alive) return;
              if (!e && feedError) recovery++;
              feedError = e ? errorMessage(e) : "";
            },
            onAccessLost: () => {
              if (alive) {
                task = null;
                workspace = null;
                config = null;
                feed = undefined;
              }
            },
          });
      } catch (e) {
        if (!read.current()) return;
        taskError = errorMessage(e);
        if (e instanceof APIError && [401, 403, 404].includes(e.status)) {
          task = null;
          workspace = null;
          config = null;
          feed?.stop();
          feed = undefined;
        }
      }
    }
    refresh = () => {
      void load();
      void feed?.refresh();
    };
    const changed = () => refresh();
    window.addEventListener("acta:tasks-changed", changed);
    window.addEventListener("focus", changed);
    void load();
    return () => {
      alive = false;
      reads.dispose();
      feed?.stop();
      window.removeEventListener("acta:tasks-changed", changed);
      window.removeEventListener("focus", changed);
    };
  });
</script>

<TaskViewer
  {selected}
  {focusComment}
  {onlayout}
  {task}
  {config}
  {workspace}
  {available}
  embedded
  taskError={error}
  {onclose}
  onretry={() => refresh()}
  onopen={(t) => onopen(t.id)}
  onchange={(t) => {
    if (t.id === selected) task = mergeTask(task, t);
  }}
  oncreate={(parent) => create?.open(parent)}
/>
{#if config && workspace}
  <CreateTaskDialog
    bind:this={create}
    workspace={workspace.id}
    {config}
    oncreated={(t) => onopen(t.id)}
  />
{/if}
