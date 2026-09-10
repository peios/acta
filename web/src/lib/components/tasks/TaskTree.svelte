<script lang="ts">
  import { isTaskProperty, propertyLabel } from "$lib/task-properties";
  import { dragScroll } from "$lib/drag-scroll";
  import "$lib/horizontal-scroll.css";
  import { defaultViewDisplay, type ViewDisplay } from "$lib/task-views.js";
  import { untrack, onDestroy } from "svelte";
  import { LatestRequest } from "$lib/requests.js";
  import { readTaskWindow } from "$lib/task-pages.js";
  import { useTaskRefresh } from "$lib/task-refresh-context";
  import { api, errorMessage } from "$lib/api";
  import {
    boardStatuses,
    completedStatus,
    personName,
    type Task,
    type TaskConfig,
    type TaskPage,
  } from "$lib/tasks";
  import {
    defaultTableLayout,
    visibleColumns,
    columnPercentages,
    type TableLayout,
  } from "$lib/task-table-layout.js";
  import TaskTableHeader from "./TaskTableHeader.svelte";
  import TaskTree from "./TaskTree.svelte";
  import TaskBoardCard from "./TaskBoardCard.svelte";
  import TaskSubtaskRow from "./TaskSubtaskRow.svelte";
  let {
    archived = false,
    workspace,
    config,
    parent = "",
    completion = "unfinished",
    query = "",
    revision = 0,
    onopen,
    depth = 0,
    presentation = "tree",
    canEdit = false,
    priorities = [],
    types = [],
    sizes = [],
    statuses = [],
    assignees = [],
    unassigned = false,
    display = defaultViewDisplay(),
    embedded = false,
    groupStatus = "",
    groupID = "",
    assignmentGroups = [],
    hover = { id: "" },
    layout = defaultTableLayout(),
    onlayoutchange = () => {},
    onsort = () => {},
    movingID = "",
    oncarddrag = () => {},
    oncarddragend = () => {},
    oncount = () => {},
  }: {
    archived?: boolean;
    workspace: string;
    config: TaskConfig;
    parent?: string;
    completion?: string;
    query?: string;
    revision?: number;
    onopen: (t: Task) => void;
    depth?: number;
    presentation?: "tree" | "subtasks" | "board";
    canEdit?: boolean;
    priorities?: string[];
    types?: string[];
    sizes?: string[];
    statuses?: string[];
    assignees?: string[];
    unassigned?: boolean;
    display?: ViewDisplay;
    embedded?: boolean;
    groupStatus?: string;
    groupID?: string;
    assignmentGroups?: import("$lib/task-groups.js").TaskGroup[];
    hover?: { id: string };
    layout?: TableLayout;
    onlayoutchange?: (layout: TableLayout, commit: boolean) => void;
    onsort?: (field: string) => void;
    movingID?: string;
    oncarddrag?: (event: DragEvent, task: Task) => void;
    oncarddragend?: () => void;
    oncount?: (total: number | null) => void;
  } = $props();
  let rows = $state<Task[]>([]),
    expanded = $state<Set<string>>(new Set()),
    more = $state(false),
    cursor = $state(""),
    loading = $state(false),
    error = $state("");
  const filtered = $derived(
    priorities.length > 0 ||
      types.length > 0 ||
      sizes.length > 0 ||
      statuses.length > 0 ||
      assignees.length > 0 ||
      unassigned,
  );
  const columns = $derived(visibleColumns(layout, display.columns));
  const columnCount = $derived(columns.length);
  let containerWidth = $state(0);
  const minimumWidth = $derived(
    columns.reduce((sum, column) => sum + column.minimum, 0),
  );
  const tableWidth = $derived(Math.max(containerWidth, minimumWidth));
  const percentages = $derived(columnPercentages(layout, columns, tableWidth));
  const grouped = $derived(
    presentation === "tree" && !parent && !embedded && display.group !== "none",
  );
  const groups = $derived(
    display.group === "status"
      ? boardStatuses(config).filter(
          (s) => !statuses.length || statuses.includes(s.id),
        )
      : assignmentGroups,
  );
  let collapsedGroups = $state<Set<string>>(new Set());
  function toggleGroup(id: string) {
    const next = new Set(collapsedGroups);
    if (next.has(id)) next.delete(id);
    else next.add(id);
    collapsedGroups = next;
  }
  const reads = new LatestRequest();
  const refresh = useTaskRefresh();
  onDestroy(() => reads.dispose());
  let lastQuery = "";
  async function load(reset = true) {
    const read = reads.begin();
    if (grouped) {
      loading = false;
      return;
    }
    loading = true;
    error = "";
    try {
      const q = new URLSearchParams({
        board: config.board ?? "tasks",
        archived: String(archived),
        parent,
        state: completion,
        q: query,
        cursor: reset ? "" : cursor,
        sort: display.sort,
        direction: display.direction,
      });
      for (const id of groupStatus ? [groupStatus] : statuses)
        q.append("status", id);
      for (const [key, values] of Object.entries({
        priority: priorities,
        type: types,
        size: sizes,
      }))
        for (const value of values) q.append(key, value);
      for (const id of assignees) q.append("assignee", id);
      if (groupID) {
        q.set("group", display.group);
        q.set("group_id", groupID);
      }
      if (unassigned) q.set("unassigned", "true");
      const identity = new URLSearchParams(q);
      identity.delete("cursor");
      const key = `${workspace}:${identity}`;
      if (key !== lastQuery) {
        rows = [];
        more = false;
        cursor = "";
        oncount(null);
        lastQuery = key;
      }
      const request = (nextCursor: string) => {
        q.set("cursor", nextCursor);
        return api<TaskPage>(`workspaces/${workspace}/tasks?${q}`, undefined, {
          signal: read.signal,
        });
      };
      const r = reset
        ? await readTaskWindow(request, rows.length)
        : await request(cursor);
      if (!read.current()) return;
      oncount(r.total);
      rows = reset
        ? r.tasks
        : [
            ...new Map(
              [...rows, ...r.tasks].map((task) => [task.id, task]),
            ).values(),
          ];
      more = r.more;
      cursor = r.cursor;
    } catch (e) {
      if (read.current()) error = errorMessage(e);
    } finally {
      if (read.current()) loading = false;
    }
  }
  $effect(() => {
    void workspace;
    void config.board;
    void parent;
    void archived;
    void completion;
    void query;
    void revision;
    void refresh.recovery;
    void priorities;
    void types;
    void sizes;
    void statuses;
    void assignees;
    void unassigned;
    void display.group;
    void display.sort;
    void display.direction;
    void grouped;
    void groupStatus;
    void groupID;
    untrack(() => void load());
  });
  function toggle(id: string) {
    const n = new Set(expanded);
    if (n.has(id)) n.delete(id);
    else n.add(id);
    expanded = n;
  }
</script>

{#snippet feedback()}
  {#if error && error !== refresh.error}<p class="notice error" role="alert">
      {error}<button class="secondary" onclick={() => void load()}>Retry</button
      >
    </p>{/if}
  {#if loading && !rows.length}<p class="hint" role="status">Loading tasks…</p>
  {:else if !loading && !error && !rows.length}<p class="empty">
      {parent
        ? "No matching subtasks."
        : filtered || query
          ? "No matching tasks."
          : groupStatus
            ? "No tasks in this status."
            : "No tasks here yet."}
    </p>{/if}
  {#if more}<button
      class="secondary more"
      disabled={loading}
      onclick={() => void load(false)}>Load more</button
    >{/if}
{/snippet}

{#snippet tableRows()}
  {#each rows as task (task.id)}
    <tr
      class="task-row"
      draggable={canEdit && !archived}
      ondragstart={(event) => {
        if (!canEdit || !event.dataTransfer) return;
        event.dataTransfer.effectAllowed = "move";
        event.dataTransfer.setData(
          "application/x-acta-task",
          JSON.stringify({
            id: task.id,
            workspace_id: task.workspace_id,
            version: task.versions.status_id,
          }),
        );
      }}
      class:child-row={depth > 0}
      class:highlighted={hover.id === task.id}
      onpointerenter={() => {
        hover.id = task.id;
      }}
      onpointerleave={() => {
        if (hover.id === task.id) hover.id = "";
      }}
    >
      {#each columns as column (column.id)}
        {#if column.id === "number"}<td class="reference-cell">
            <div class="task-identity">
              <button
                class="expand"
                aria-label={`${expanded.has(task.id) ? "Collapse" : "Expand"} ${task.reference} subtasks`}
                aria-expanded={!filtered && expanded.has(task.id)}
                disabled={!task.children || filtered}
                onclick={() => toggle(task.id)}
              >
                {#if task.children && !filtered}<svg
                    class:expanded={expanded.has(task.id)}
                    viewBox="0 0 20 20"
                    aria-hidden="true"><path d="m7 5 5 5-5 5" /></svg
                  >{/if}
              </button>
              <button class="reference" onclick={() => onopen(task)}
                >{task.reference}</button
              >
            </div>
          </td>
        {:else if column.id === "title"}<td
            class="title-cell"
            style:padding-left={`${10 + Math.min(depth, 8) * 16}px`}
            ><button
              class="task-open"
              title={task.title}
              aria-label={`${task.reference} ${task.title}`}
              onclick={() => onopen(task)}>{task.title}</button
            ></td
          >
        {:else if column.id === "status"}<td>
            <span
              class="status"
              class:completed={completedStatus(config, task.status_id)}
              title={config.statuses.find((s) => s.id === task.status_id)?.name}
            >
              <span class="status-dot" aria-hidden="true"
              ></span>{config.statuses.find((s) => s.id === task.status_id)
                ?.name}
            </span>
          </td>
        {:else if isTaskProperty(column.id)}<td
            ><span
              class="metadata-value"
              class:unset={task[column.id] === "none"}
              >{propertyLabel(column.id, task[column.id])}</span
            ></td
          >
        {:else if column.id === "assignees"}
          <td>
            <div class="assignees" aria-label="Assignees">
              {#each task.assignees.slice(0, 3) as p}<span
                  class="avatar"
                  title={`${personName(p)}${!p.available ? " · Access removed" : ""}`}
                  >{personName(p).slice(0, 1).toUpperCase()}</span
                >{/each}
              {#if task.assignees.length > 3}<span class="count"
                  >+{task.assignees.length - 3}</span
                >{/if}
              {#if task.descendant_assignees.some((p) => !task.assignees.some((a) => a.id === p.id))}<span
                  class="indirect"
                  title={task.descendant_assignees
                    .filter((p) => !task.assignees.some((a) => a.id === p.id))
                    .map(
                      (p) =>
                        `${personName(p)}: ${p.sources.map((s) => s.reference).join(", ")}`,
                    )
                    .join("\n")}
                  >+{task.descendant_assignees.filter(
                    (p) => !task.assignees.some((a) => a.id === p.id),
                  ).length} via subtasks</span
                >
              {:else if !task.assignees.length}<span class="unassigned">—</span
                >{/if}
            </div>
          </td>{/if}
      {/each}
    </tr>
    {#if expanded.has(task.id) && !filtered}<TaskTree
        {archived}
        {workspace}
        {config}
        parent={task.id}
        {completion}
        {revision}
        {onopen}
        {canEdit}
        {presentation}
        {display}
        {layout}
        depth={depth + 1}
      />{/if}
  {/each}
  {#if error || loading || !rows.length || more}<tr
      ><td colspan={columnCount} class="feedback-cell">{@render feedback()}</td
      ></tr
    >{/if}
{/snippet}

{#if presentation === "subtasks"}
  <div class="task-tree">
    {#each rows as task (task.id)}
      <TaskSubtaskRow
        {task}
        {config}
        editable={canEdit}
        {onopen}
        onsaved={(updated) => {
          rows = rows.map((row) => (row.id === updated.id ? updated : row));
        }}
      />
    {/each}
    {@render feedback()}
  </div>
{:else if presentation === "board"}
  <div class="board-cards" class:compact-cards={display.density === "compact"}>
    {#each rows as task (task.id)}<TaskBoardCard
        {task}
        {config}
        {display}
        {onopen}
        canDrag={canEdit}
        {hover}
        moving={movingID === task.id}
        ondrag={oncarddrag}
        ondragend={oncarddragend}
      />{/each}
    {@render feedback()}
  </div>
{:else if depth > 0 || embedded}
  {@render tableRows()}
{:else}
  <!-- svelte-ignore a11y_no_noninteractive_tabindex (The horizontal scroll region must be keyboard accessible.) -->
  <div
    class="table-scroll horizontal-scroll"
    bind:clientWidth={containerWidth}
    use:dragScroll
    class:compact={display.density === "compact"}
    role="region"
    aria-label="Task list"
    tabindex="0"
  >
    <table
      class="task-table"
      aria-label="Tasks"
      style:min-width={`${minimumWidth}px`}
    >
      <colgroup
        >{#each columns as column (column.id)}<col
            style:width={`${percentages[column.id]}%`}
          />{/each}</colgroup
      >
      <TaskTableHeader
        {columns}
        {layout}
        {percentages}
        {tableWidth}
        {display}
        onchange={onlayoutchange}
        {onsort}
      />
      <tbody>
        {#if grouped}
          {#each groups as status (status.id)}
            <tr class="group-row"
              ><th colspan={columnCount} scope="rowgroup">
                <button
                  class="group-toggle"
                  aria-expanded={!collapsedGroups.has(status.id)}
                  onclick={() => toggleGroup(status.id)}
                >
                  <svg
                    viewBox="0 0 20 20"
                    class:expanded={!collapsedGroups.has(status.id)}
                    aria-hidden="true"><path d="m7 5 5 5-5 5" /></svg
                  >
                  <span
                    class="status-dot"
                    class:done={status.id === config.completed_status}
                    aria-hidden="true"
                  ></span>{status.name}
                </button>
              </th></tr
            >
            {#if !collapsedGroups.has(status.id)}<TaskTree
                {archived}
                {workspace}
                {config}
                {completion}
                {query}
                {revision}
                {onopen}
                {canEdit}
                {priorities}
                {types}
                {sizes}
                {statuses}
                {assignees}
                {unassigned}
                {display}
                {layout}
                embedded
                groupStatus={display.group === "status" ? status.id : ""}
                groupID={display.group === "status" ? "" : status.id}
                {hover}
              />{/if}
          {/each}
        {:else}{@render tableRows()}{/if}
      </tbody>
    </table>
  </div>
{/if}

<style>
  .metadata-value {
    font-size: 12px;
    color: var(--text);
  }
  .metadata-value.unset {
    color: var(--muted);
  }
  .board-cards {
    display: flex;
    flex-direction: column;
    gap: 10px;
  }
  .board-cards.compact-cards {
    gap: 6px;
  }
  .task-tree {
    min-width: 0;
  }
  .table-scroll {
    position: relative;
    overflow-x: auto;
    --row-padding: 8px;
  }
  .table-scroll:focus-visible {
    outline: 2px solid var(--accent);
    outline-offset: 3px;
  }
  .task-table {
    width: 100%;
    table-layout: fixed;
    border-collapse: collapse;
    font-size: 13px;
  }
  th {
    background: transparent;
    color: var(--muted);
    font-size: 11px;
    font-weight: 500;
    text-align: left;
    padding: 11px 10px;
    border-bottom: 1px solid var(--panel-border);
  }
  td {
    padding: var(--row-padding) 10px;
    border-bottom: 1px solid
      color-mix(in srgb, var(--panel-border) 55%, transparent);
    vertical-align: middle;
  }
  tr:last-child td {
    border-bottom: 0;
  }
  tbody .task-row.highlighted,
  .task-row:hover {
    background: color-mix(in srgb, var(--hover-surface) 50%, transparent);
  }
  .reference-cell {
    padding-left: 4px;
    padding-right: 4px;
  }
  .task-identity {
    display: flex;
    align-items: center;
    gap: 2px;
  }
  .reference {
    font-size: 11px;
    font-variant-numeric: tabular-nums;
    color: var(--muted);
    white-space: nowrap;
    background: none;
    border: 0;
    padding: 6px 0;
  }
  .task-open {
    display: block;
    width: 100%;
    text-align: left;
    background: none;
    border: 0;
    color: var(--text);
    padding: 6px 0;
    font-size: 13px;
    line-height: 1.5;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
    font-weight: 450;
  }
  .task-open:hover,
  .reference:hover {
    color: var(--accent);
  }
  .expand {
    display: grid;
    place-items: center;
    border: 0;
    background: none;
    color: var(--muted);
    padding: 0;
    width: 28px;
    height: 32px;
    flex-shrink: 0;
    border-radius: 6px;
  }
  .expand:hover:not(:disabled) {
    background: var(--hover-surface);
    color: var(--text);
  }
  .expand:disabled {
    cursor: default;
  }
  .expand svg {
    width: 14px;
    height: 14px;
    fill: none;
    stroke: currentColor;
    stroke-width: 1.5;
    stroke-linecap: round;
    stroke-linejoin: round;
    transition: transform 140ms ease;
  }
  .expand svg.expanded {
    transform: rotate(90deg);
  }
  .status {
    display: flex;
    align-items: center;
    gap: 7px;
    min-width: 0;
    font-size: 11px;
    color: var(--muted);
    white-space: nowrap;
    overflow: hidden;
    text-overflow: ellipsis;
  }
  .status-dot {
    width: 9px;
    height: 9px;
    flex-shrink: 0;
    border: 1.5px solid var(--muted);
    border-radius: 50%;
  }
  .completed .status-dot,
  .status-dot.done {
    background: var(--accent);
    border-color: var(--accent);
  }
  .compact {
    --row-padding: 2px;
  }
  .group-row th {
    padding: 0;
    border-top: 1px solid var(--panel-border);
    background: color-mix(in srgb, var(--hover-surface) 50%, transparent);
  }
  .group-toggle {
    display: flex;
    align-items: center;
    gap: 8px;
    width: 100%;
    padding: 12px 8px;
    border: 0;
    background: none;
    color: var(--text);
    font-size: 12px;
    font-weight: 550;
    text-align: left;
  }
  .group-toggle svg {
    width: 14px;
    height: 14px;
    fill: none;
    stroke: currentColor;
    stroke-width: 1.5;
    transition: transform 140ms ease;
  }
  .group-toggle svg.expanded {
    transform: rotate(90deg);
  }
  .assignees {
    display: flex;
    align-items: center;
    gap: 3px;
    min-width: 0;
  }
  .avatar {
    display: grid;
    place-items: center;
    border-radius: 50%;
    background: var(--scope-active);
    border: 2px solid var(--surface);
    flex-shrink: 0;
    width: 26px;
    height: 26px;
    font-size: 11px;
  }
  .avatar + .avatar {
    margin-left: -9px;
  }
  .indirect {
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }
  .indirect,
  .count,
  .unassigned {
    font-size: 10px;
    color: var(--muted);
  }
  .empty {
    margin: 0;
    padding: 12px 4px;
    color: var(--muted);
    font-size: 13px;
  }
  .more {
    margin: 8px 0;
  }
  .feedback-cell {
    padding: 0 12px;
  }
  @media (prefers-reduced-motion: reduce) {
    .expand svg,
    .group-toggle svg {
      transition: none;
    }
  }
</style>
