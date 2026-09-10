<script lang="ts">
  import { taskProperties, propertyLabel } from "$lib/task-properties";
  import { personName, type Task, type TaskConfig } from "$lib/tasks";
  import type { ViewDisplay } from "$lib/task-views.js";
  let {
    task,
    config,
    display,
    onopen,
    canDrag,
    moving,
    ondrag,
    ondragend,
    hover,
  }: {
    task: Task;
    config: TaskConfig;
    display: ViewDisplay;
    onopen: (task: Task) => void;
    canDrag: boolean;
    moving: boolean;
    ondrag: (event: DragEvent, task: Task) => void;
    ondragend: () => void;
    hover: { id: string };
  } = $props();
  let dragging = $state(false);
</script>

<button
  class="board-card"
  class:compact={display.density === "compact"}
  class:dragging
  class:highlighted={hover.id === task.id}
  onpointerenter={() => {
    hover.id = task.id;
  }}
  onpointerleave={() => {
    if (hover.id === task.id) hover.id = "";
  }}
  draggable={canDrag && !moving}
  disabled={moving}
  aria-label={`Open ${task.reference}: ${task.title}`}
  onclick={() => onopen(task)}
  ondragstart={(event) => {
    dragging = true;
    ondrag(event, task);
  }}
  ondragend={() => {
    dragging = false;
    ondragend();
  }}
>
  <span class="card-top"
    ><span class="reference">{task.reference}</span>
    {#if task.children}<span
        class="subtasks"
        title={`${task.children} ${task.children === 1 ? "subtask" : "subtasks"}`}
        aria-label={`${task.children} ${task.children === 1 ? "subtask" : "subtasks"}`}
      >
        <svg viewBox="0 0 20 20" aria-hidden="true"
          ><path d="M5 3v10a2 2 0 0 0 2 2h3M5 7h5" /><rect
            x="11"
            y="4"
            width="6"
            height="5"
            rx="1"
          /><rect x="11" y="12" width="6" height="5" rx="1" /></svg
        >{task.children}
      </span>{/if}
  </span>
  <span class="card-title" title={task.title}>{task.title}</span>
  {#if display.columns.length}
    <span class="card-properties">
      {#each taskProperties.filter( (p) => display.columns.includes(p.value) ) as p}<span
          class="metadata"
          title={p.label}>{propertyLabel(p.value, task[p.value])}</span
        >{/each}
      {#if display.columns.includes("status")}<span
          class="status"
          title={config.statuses.find((s) => s.id === task.status_id)?.name}
        >
          <span
            class="status-dot"
            class:done={task.status_id === config.completed_status}
          ></span>{config.statuses.find((s) => s.id === task.status_id)?.name}
        </span>{/if}
      {#if display.columns.includes("assignees")}<span
          class="avatars"
          aria-label="Assignees"
        >
          {#each task.assignees.slice(0, 3) as person}<span
              class="avatar"
              title={`${personName(person)}${!person.available ? " · Access removed" : ""}`}
              >{personName(person).slice(0, 1).toUpperCase()}</span
            >{/each}
          {#if task.assignees.length > 3}<span
              class="extra"
              title={task.assignees.slice(3).map(personName).join(", ")}
              >+{task.assignees.length - 3}</span
            >{/if}
        </span>{/if}
    </span>
  {/if}
  {#if moving}<span class="moving" role="status">Moving…</span>{/if}
</button>

<style>
  .metadata {
    font-size: 11px;
    color: var(--muted);
    border: 1px solid var(--panel-border);
    padding: 2px 6px;
    border-radius: 5px;
  }
  .board-card {
    display: flex;
    flex-direction: column;
    gap: 12px;
    width: 100%;
    min-width: 0;
    padding: 16px;
    border: 1px solid var(--panel-border);
    border-radius: 12px;
    background: var(--surface);
    color: var(--text);
    text-align: left;
    box-shadow: 0 2px 5px #00000008;
    transition:
      border-color 140ms ease,
      box-shadow 140ms ease;
  }
  .board-card:hover,
  .board-card.highlighted {
    border-color: color-mix(in srgb, var(--accent) 40%, var(--panel-border));
    box-shadow: 0 3px 12px #00000010;
  }
  .board-card[draggable="true"] {
    cursor: grab;
  }
  .board-card:active[draggable="true"] {
    cursor: grabbing;
  }
  .board-card:focus-visible {
    outline: 2px solid var(--accent);
    outline-offset: 2px;
  }
  .dragging {
    opacity: 0.4;
  }
  .compact {
    padding: 10px 12px;
    gap: 7px;
    border-radius: 9px;
  }
  .card-top {
    display: flex;
    align-items: center;
    justify-content: space-between;
    color: var(--muted);
    font-size: 11px;
  }
  .reference {
    font-variant-numeric: tabular-nums;
  }
  .subtasks {
    display: flex;
    align-items: center;
    gap: 4px;
  }
  svg {
    width: 13px;
    height: 13px;
    fill: none;
    stroke: currentColor;
    stroke-width: 1.3;
  }
  .card-title {
    display: -webkit-box;
    -webkit-box-orient: vertical;
    -webkit-line-clamp: 3;
    line-clamp: 3;
    overflow: hidden;
    overflow-wrap: anywhere;
    font-size: 13px;
    font-weight: 500;
    line-height: 1.5;
  }
  .card-properties {
    display: flex;
    flex-wrap: wrap;
    align-items: center;
    gap: 8px;
    min-height: 22px;
  }
  .status {
    display: flex;
    gap: 6px;
    align-items: center;
    min-width: 0;
    overflow: hidden;
    white-space: nowrap;
    text-overflow: ellipsis;
    font-size: 11px;
    color: var(--muted);
  }
  .status-dot {
    width: 8px;
    height: 8px;
    flex-shrink: 0;
    border: 1.5px solid var(--muted);
    border-radius: 50%;
  }
  .status-dot.done {
    background: var(--accent);
    border-color: var(--accent);
  }
  .avatars {
    display: flex;
    align-items: center;
    flex-shrink: 0;
    margin-left: auto;
  }
  .avatar {
    display: grid;
    place-items: center;
    width: 24px;
    height: 24px;
    border: 2px solid var(--surface);
    border-radius: 50%;
    background: var(--scope-active);
    font-size: 10px;
  }
  .avatar + .avatar {
    margin-left: -7px;
  }
  .extra,
  .moving {
    font-size: 10px;
    color: var(--muted);
  }
  .extra {
    margin-left: 3px;
  }
  @media (prefers-reduced-motion: reduce) {
    .board-card {
      transition: none;
    }
  }
</style>
