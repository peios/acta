<script lang="ts">
  import { isTaskProperty } from "$lib/task-properties";
  import { dragScroll } from "$lib/drag-scroll";
  import "$lib/horizontal-scroll.css";
  import { onMount } from "svelte";
  import { api, errorMessage } from "$lib/api";
  import {
    boardStatuses,
    setTaskStatus,
    taskChanged,
    type Task,
    type TaskConfig,
  } from "$lib/tasks";
  import type { ViewDisplay } from "$lib/task-views.js";
  import { moveAssignments, type TaskGroup } from "$lib/task-groups.js";
  import TaskInlineCreate from "./TaskInlineCreate.svelte";
  import TaskTree from "./TaskTree.svelte";
  let {
    archived = false,
    workspace,
    config,
    display,
    priorities = [],
    types = [],
    sizes = [],
    statuses,
    assignees,
    unassigned,
    query,
    onopen,
    canEdit,
    canCreate,
    assignmentGroups,
    hover,
  }: {
    archived?: boolean;
    workspace: string;
    config: TaskConfig;
    display: ViewDisplay;
    priorities?: string[];
    types?: string[];
    sizes?: string[];
    statuses: string[];
    assignees: string[];
    unassigned: boolean;
    query: string;
    onopen: (task: Task) => void;
    canEdit: boolean;
    canCreate: boolean;
    assignmentGroups: TaskGroup[];
    hover: { id: string };
  } = $props();
  const lanes = $derived(
    display.group === "status"
      ? boardStatuses(config)
          .filter((s) => !statuses.length || statuses.includes(s.id))
          .map((s) => ({ ...s, assign_id: "", available: true }))
      : display.group === "none"
        ? [{ id: "", name: "Tasks", assign_id: "", available: true }]
        : assignmentGroups,
  );
  let source = "";
  let counts = $state<Record<string, number | null>>({});
  let dragged = $state<Task | null>(null),
    target = $state(""),
    moving = $state(""),
    error = $state("");
  let board: HTMLDivElement;
  let scrollFrame = 0,
    pointerX = 0;
  function stopDrag() {
    hover.id = "";
    dragged = null;
    target = "";
    cancelAnimationFrame(scrollFrame);
  }
  function scroll() {
    if (!dragged) return;
    const rect = board.getBoundingClientRect();
    const left = Math.max(0, rect.left),
      right = Math.min(window.innerWidth, rect.right);
    if (pointerX < left + 40) board.scrollLeft -= 8;
    else if (pointerX > right - 40) board.scrollLeft += 8;
    scrollFrame = requestAnimationFrame(scroll);
  }
  function startDrag(event: DragEvent, task: Task, lane: string) {
    if (!canEdit || moving) {
      event.preventDefault();
      return;
    }
    dragged = task;
    source = lane;
    error = "";
    pointerX = event.clientX;
    if (event.dataTransfer) {
      event.dataTransfer.effectAllowed = "move";
      event.dataTransfer.setData("text/plain", task.id);
      event.dataTransfer.setData(
        "application/x-acta-task",
        JSON.stringify({
          id: task.id,
          workspace_id: task.workspace_id,
          version: task.versions.status_id,
        }),
      );
    }
    scrollFrame = requestAnimationFrame(scroll);
  }
  function over(event: DragEvent, status: string) {
    if (
      !dragged ||
      !canEdit ||
      !status ||
      status === source ||
      !lanes.find((l) => l.id === status)?.available ||
      moving
    )
      return;
    event.preventDefault();
    if (event.dataTransfer) event.dataTransfer.dropEffect = "move";
    target = status;
  }
  async function drop(event: DragEvent, status: string) {
    event.preventDefault();
    const task = dragged;
    stopDrag();
    if (
      !task ||
      !canEdit ||
      display.group === "none" ||
      moving ||
      source === status ||
      !lanes.some((l) => l.id === status && l.available)
    )
      return;
    moving = task.id;
    error = "";
    try {
      if (display.group === "status") await setTaskStatus(task, status);
      else if (isTaskProperty(display.group))
        await api<Task>(`tasks/${task.id}`, {
          field: display.group,
          version: task.versions[display.group],
          value: status,
        });
      else
        await api<Task>(`tasks/${task.id}`, {
          field: "assignees",
          version: task.versions.assignees,
          value: moveAssignments(
            task.assignees,
            display.group,
            source,
            lanes.find((l) => l.id === status)!.assign_id,
          ),
        });
      taskChanged();
    } catch (e) {
      error = `Couldn’t move ${task.reference}. ${errorMessage(e)}`;
      taskChanged();
    } finally {
      moving = "";
    }
  }
  onMount(() => () => cancelAnimationFrame(scrollFrame));
</script>

{#if error}<p class="notice error" role="alert">{error}</p>{/if}
<!-- svelte-ignore a11y_no_noninteractive_tabindex (The horizontally scrolling board is keyboard accessible.) -->
<div
  class="board horizontal-scroll"
  class:dragging={!!dragged}
  class:compact={display.density === "compact"}
  bind:this={board}
  use:dragScroll
  role="region"
  aria-label="Task board"
  tabindex="0"
  ondragover={(event) => {
    pointerX = event.clientX;
  }}
  ondragleave={(event) => {
    if (!(
      event.relatedTarget instanceof Node && board.contains(event.relatedTarget)
    ))
      target = "";
  }}
>
  {#each lanes as lane (lane.id)}
    <section
      class="lane"
      class:drop-target={!!dragged && target === lane.id}
      aria-label={lane.name}
      ondragover={(event) => over(event, lane.id)}
      ondrop={(event) => void drop(event, lane.id)}
      ondragleave={(event) => {
        if (
          !(
            event.relatedTarget instanceof Node &&
            event.currentTarget.contains(event.relatedTarget)
          ) &&
          target === lane.id
        )
          target = "";
      }}
    >
      <header>
        {#if display.group === "status"}<span
            class="status-dot"
            class:done={lane.id === config.completed_status}
          ></span>{/if}
        <h2>{lane.name}</h2>
        <span
          class="task-count"
          aria-label={counts[lane.id] == null
            ? "Loading task count"
            : `${counts[lane.id]} ${counts[lane.id] === 1 ? "task" : "tasks"}`}
          >{counts[lane.id] ?? "–"}</span
        >
      </header>
      {#if canCreate && lane.available}<TaskInlineCreate
          {workspace}
          status={display.group === "status" ? lane.id : config.creation_status}
          assignees={lane.assign_id ? [lane.assign_id] : []}
          properties={isTaskProperty(display.group)
            ? { [display.group]: lane.id }
            : {}}
          label={lane.name}
        />{/if}
      <TaskTree
        {archived}
        {workspace}
        {config}
        completion="all"
        {display}
        {priorities}
        {types}
        {sizes}
        {statuses}
        {assignees}
        {unassigned}
        {query}
        {onopen}
        canEdit={canEdit && !moving}
        revision={config.revision}
        groupStatus={display.group === "status" ? lane.id : ""}
        groupID={display.group === "assignee" ||
        display.group === "agents" ||
        isTaskProperty(display.group)
          ? lane.id
          : ""}
        {hover}
        presentation="board"
        movingID={moving}
        oncarddrag={(event, task) => startDrag(event, task, lane.id)}
        oncarddragend={stopDrag}
        oncount={(total) => {
          counts[lane.id] = total;
        }}
      />
    </section>
  {/each}
  {#if !lanes.length}<p class="hint">No groups to show.</p>{/if}
</div>

<style>
  .board {
    display: flex;
    align-items: stretch;
    gap: 16px;
    overflow-x: auto;
    padding: 2px 2px 14px;
    min-width: 0;
  }
  .board:focus-visible {
    outline: 2px solid var(--accent);
    outline-offset: 3px;
  }
  .lane {
    flex: 0 0 min(300px, calc(100vw - 48px));
    width: min(300px, calc(100vw - 48px));
    min-width: 0;
    min-height: 220px;
    padding: 8px;
    border: 1px solid transparent;
    border-radius: 14px;
    transition:
      background 140ms ease,
      border-color 140ms ease;
  }
  .lane.drop-target {
    border-color: var(--accent);
    background: color-mix(in srgb, var(--accent) 10%, var(--surface));
  }
  header {
    display: flex;
    align-items: center;
    gap: 8px;
    padding: 8px 6px 16px;
  }
  h2 {
    margin: 0;
    font-size: 13px;
    font-weight: 550;
    color: var(--text);
  }
  .task-count {
    color: var(--muted);
    font-size: 12px;
    font-variant-numeric: tabular-nums;
  }
  .status-dot {
    width: 9px;
    height: 9px;
    border: 1.5px solid var(--muted);
    border-radius: 50%;
  }
  .status-dot.done {
    background: var(--accent);
    border-color: var(--accent);
  }
  .compact {
    gap: 12px;
  }
  .compact .lane {
    padding: 6px;
  }
  .compact header {
    padding-bottom: 12px;
  }
  @media (prefers-reduced-motion: reduce) {
    .lane {
      transition: none;
    }
  }
</style>
