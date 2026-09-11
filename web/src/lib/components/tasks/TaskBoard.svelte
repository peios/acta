<script lang="ts">
  import type { TouchDragPoint } from "$lib/touch-drag";
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
  let moveDialog: HTMLDialogElement;
  let moveSelection = $state<{ task: Task; from: string } | null>(null);
  let moveQuery = $state("");
  function openMove(task: Task, from: string) {
    if (!canEdit || archived || moving) return;
    moveSelection = { task, from };
    moveQuery = "";
    moveDialog.showModal();
  }
  let source = "";
  let counts = $state<Record<string, number | null>>({});
  let dragged = $state<Task | null>(null),
    target = $state(""),
    moving = $state(""),
    error = $state("");
  let board: HTMLDivElement;
  let scrollFrame = 0,
    pointerX = 0;
  let touchPoint = $state<TouchDragPoint | null>(null);
  let lastScrollTime = 0;
  function validTarget(id: string) {
    return (
      display.group !== "none" &&
      id !== source &&
      lanes.some((l) => l.id === id && l.available)
    );
  }
  function touchTarget() {
    if (!touchPoint) return;
    const element = document
      .elementFromPoint(touchPoint.x, touchPoint.y)
      ?.closest<HTMLElement>("[data-board-lane]");
    const id = element?.dataset.boardLane;
    target =
      element && board.contains(element) && id !== undefined && validTarget(id)
        ? id
        : "";
  }
  function beginTouch(point: TouchDragPoint, task: Task, lane: string) {
    if (!canEdit || moving || display.group === "none") return false;
    dragged = task;
    source = lane;
    error = "";
    touchPoint = point;
    pointerX = point.x;
    lastScrollTime = 0;
    scrollFrame = requestAnimationFrame(scroll);
    return true;
  }
  function moveTouch(point: TouchDragPoint) {
    touchPoint = point;
    pointerX = point.x;
    touchTarget();
  }
  function finishTouch(cancelled: boolean) {
    if (!touchPoint) return;
    const destination = target;
    if (cancelled || !destination) stopDrag();
    else void drop(destination);
  }
  function stopDrag() {
    hover.id = "";
    dragged = null;
    target = "";
    touchPoint = null;
    lastScrollTime = 0;
    cancelAnimationFrame(scrollFrame);
  }
  function scroll(time: number) {
    if (!dragged) return;
    const rect = board.getBoundingClientRect();
    const left = Math.max(0, rect.left),
      right = Math.min(window.innerWidth, rect.right);
    const step =
      Math.min(32, lastScrollTime ? time - lastScrollTime : 16) * 0.6;
    lastScrollTime = time;
    if (pointerX < left + 48) board.scrollLeft -= step;
    else if (pointerX > right - 48) board.scrollLeft += step;
    if (touchPoint) {
      const container = board.closest("main");
      if (container) {
        const bounds = container.getBoundingClientRect();
        if (touchPoint.y < bounds.top + 48) container.scrollTop -= step;
        else if (touchPoint.y > bounds.bottom - 48) container.scrollTop += step;
      }
      touchTarget();
    }
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
  async function drop(status: string) {
    const task = dragged;
    const from = source;
    stopDrag();
    if (task) await moveTask(task, from, status);
  }
  async function moveTask(task: Task, from: string, status: string) {
    if (
      archived ||
      !canEdit ||
      display.group === "none" ||
      moving ||
      from === status ||
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
            from,
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
      data-board-lane={lane.id}
      class:drop-target={!!dragged && target === lane.id}
      aria-label={lane.name}
      ondragover={(event) => over(event, lane.id)}
      ondrop={(event) => {
        event.preventDefault();
        void drop(lane.id);
      }}
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
        oncardmove={display.group === "none"
          ? undefined
          : (task) => openMove(task, lane.id)}
        touchDragFor={display.group === "none"
          ? undefined
          : (task) => ({
              enabled: () => canEdit && !moving && display.group !== "none",
              start: (point) => beginTouch(point, task, lane.id),
              move: moveTouch,
              finish: finishTouch,
            })}
        oncount={(total) => {
          counts[lane.id] = total;
        }}
      />
    </section>
  {/each}
  {#if !lanes.length}<p class="hint">No groups to show.</p>{/if}
</div>

{#if touchPoint && dragged}
  <div
    class="drag-preview"
    aria-hidden="true"
    style:left={`${Math.max(12, Math.min(touchPoint.x - 110, (typeof window !== "undefined" ? window.innerWidth : 390) - 232))}px`}
    style:top={`${Math.max(12, touchPoint.y - 100)}px`}
  >
    <span>{dragged.reference}</span><strong>{dragged.title}</strong>
    <small
      >{target
        ? `Move to ${lanes.find((l) => l.id === target)?.name}`
        : "Drag to another column"}</small
    >
  </div>
{/if}
<div class="sr-only" role="status" aria-live="polite">
  {dragged && touchPoint
    ? target
      ? `Release to move to ${lanes.find((l) => l.id === target)?.name}`
      : `Moving ${dragged.reference}. Drag to another column.`
    : moving
      ? "Saving task move…"
      : ""}
</div>

<dialog
  bind:this={moveDialog}
  class="management-dialog move-dialog"
  aria-label="Move task"
>
  <h2>Move to…</h2>
  <p class="hint">
    {moveSelection?.task.reference} · {moveSelection?.task.title}
  </p>
  <input
    aria-label="Find destination column"
    placeholder="Find a column…"
    bind:value={moveQuery}
  />
  <div class="destinations">
    {#each lanes.filter((lane) => lane.name
        .toLocaleLowerCase()
        .includes(moveQuery.trim().toLocaleLowerCase())) as lane (lane.id)}
      <button
        class="destination"
        disabled={!lane.available || lane.id === moveSelection?.from}
        onclick={() => {
          const selection = moveSelection;
          moveDialog.close();
          moveSelection = null;
          if (selection) void moveTask(selection.task, selection.from, lane.id);
        }}
        ><span>{lane.name}</span>{#if lane.id === moveSelection?.from}<small
            >Current</small
          >{/if}</button
      >
    {:else}<p class="hint">No matching columns.</p>{/each}
  </div>
  <div class="management-actions">
    <button class="secondary" onclick={() => moveDialog.close()}>Cancel</button>
  </div>
</dialog>

<style>
  .move-dialog {
    max-width: 420px;
  }
  .move-dialog input {
    width: 100%;
  }
  .destinations {
    display: grid;
    gap: 4px;
    margin-block: 16px;
    max-height: 40dvh;
    overflow-y: auto;
  }
  .destination {
    display: flex;
    align-items: center;
    justify-content: space-between;
    gap: 12px;
    min-height: 48px;
    padding: 12px;
    text-align: left;
    border: 0;
    border-radius: 8px;
    background: transparent;
    color: var(--text);
  }
  .destination:hover:not(:disabled) {
    background: var(--hover-surface);
  }
  .destination small {
    color: var(--muted);
  }

  .drag-preview {
    position: fixed;
    z-index: 90;
    pointer-events: none;
    width: 220px;
    padding: 12px;
    display: grid;
    gap: 5px;
    border: 1px solid var(--accent);
    border-radius: 12px;
    background: var(--surface);
    color: var(--text);
    box-shadow: 0 12px 30px #0003;
  }
  .drag-preview span,
  .drag-preview small {
    color: var(--muted);
    font-size: 11px;
  }
  .drag-preview strong {
    font-size: 13px;
    overflow: hidden;
    white-space: nowrap;
    text-overflow: ellipsis;
  }
  .sr-only {
    position: absolute;
    width: 1px;
    height: 1px;
    overflow: hidden;
    clip-path: inset(50%);
  }
  @media (max-width: 720px) {
    .board {
      scroll-snap-type: x mandatory;
      scroll-padding-inline: 2px;
      overscroll-behavior-x: contain;
      mask-image: none;
    }
    .board .lane {
      flex-basis: calc(100% - 20px);
      width: calc(100% - 20px);
      scroll-snap-align: start;
      scroll-snap-stop: always;
    }
    .board.dragging {
      scroll-snap-type: none;
    }
  }

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
