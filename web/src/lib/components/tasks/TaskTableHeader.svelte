<script lang="ts">
  import { onMount, tick } from "svelte";
  import {
    reorderColumn,
    resizeColumns,
    type TableLayout,
    type ColumnID,
    taskColumns,
  } from "$lib/task-table-layout.js";
  import type { ViewDisplay } from "$lib/task-views.js";
  let {
    columns,
    layout,
    percentages,
    tableWidth,
    display,
    onchange,
    onsort,
  }: {
    columns: typeof taskColumns;
    layout: TableLayout;
    percentages: Record<string, number>;
    tableWidth: number;
    display: ViewDisplay;
    onchange: (layout: TableLayout, commit: boolean) => void;
    onsort: (field: string) => void;
  } = $props();
  let row: HTMLTableRowElement;
  let dragging = $state<ColumnID | null>(null);
  let resizing = $state<ColumnID | null>(null);
  let dropSlot = $state(-1);
  let ghostX = $state(0),
    ghostY = $state(0);
  let announcement = $state("");
  type Gesture = {
    kind: "resize" | "reorder";
    id: ColumnID;
    index: number;
    pointer: number;
    target: HTMLElement;
    x: number;
    y: number;
    currentX: number;
    currentY: number;
    original: TableLayout;
    percentages: Record<string, number>;
    width: number;
    columns: typeof taskColumns;
    moved: boolean;
    scroller: HTMLElement | null;
  };
  let gesture: Gesture | null = null;
  let frame = 0;
  let suppressSort = false;
  const sortable = (id: ColumnID) => id !== "assignees";
  function sort(event: MouseEvent, id: ColumnID) {
    if (suppressSort && event.detail !== 0) {
      suppressSort = false;
      return;
    }
    if (sortable(id)) onsort(id);
  }
  function begin(event: PointerEvent, kind: Gesture["kind"], index: number) {
    if (event.button !== 0 || !event.isPrimary || gesture) return;
    event.preventDefault();
    suppressSort = false;
    const target = event.currentTarget as HTMLElement;
    target.focus();
    gesture = {
      kind,
      id: columns[index].id,
      index,
      pointer: event.pointerId,
      target,
      x: event.clientX,
      y: event.clientY,
      currentX: event.clientX,
      currentY: event.clientY,
      original: { order: [...layout.order], widths: { ...layout.widths } },
      percentages: { ...percentages },
      width: tableWidth,
      columns: [...columns],
      moved: false,
      scroller: row.closest(".table-scroll"),
    };
    target.setPointerCapture(event.pointerId);
  }
  function locateDrop() {
    if (!gesture) return;
    const cells = [...row.cells];
    const index = cells.findIndex((cell) => {
      const rect = cell.getBoundingClientRect();
      return gesture!.currentX < rect.left + rect.width / 2;
    });
    dropSlot = index < 0 ? cells.length : index;
  }
  function autoScroll() {
    if (!gesture || !dragging) return;
    const scroller = gesture.scroller;
    if (scroller) {
      const rect = scroller.getBoundingClientRect();
      const left = Math.max(0, rect.left),
        right = Math.min(window.innerWidth, rect.right);
      const delta =
        gesture.currentX < left + 32
          ? -8
          : gesture.currentX > right - 32
            ? 8
            : 0;
      if (delta) {
        scroller.scrollLeft += delta;
        locateDrop();
      }
    }
    frame = requestAnimationFrame(autoScroll);
  }
  function move(event: PointerEvent) {
    const g = gesture;
    if (!g || event.pointerId !== g.pointer) return;
    g.currentX = event.clientX;
    g.currentY = event.clientY;
    const dx = event.clientX - g.x;
    if (!g.moved && Math.hypot(dx, event.clientY - g.y) < 4) return;
    g.moved = true;
    suppressSort = true;
    if (g.kind === "resize") {
      resizing = g.id;
      onchange(
        resizeColumns(
          g.original,
          g.columns,
          g.percentages,
          g.index,
          (dx / g.width) * 100,
          g.width,
        ),
        false,
      );
    } else {
      if (!dragging) {
        dragging = g.id;
        frame = requestAnimationFrame(autoScroll);
      }
      ghostX = event.clientX + 12;
      ghostY = event.clientY + 12;
      locateDrop();
    }
  }
  function finish(commit: boolean) {
    const g = gesture;
    if (!g) return;
    gesture = null;
    cancelAnimationFrame(frame);
    if (g.target.hasPointerCapture(g.pointer))
      g.target.releasePointerCapture(g.pointer);
    if (g.moved) {
      if (!commit) onchange(g.original, false);
      else if (g.kind === "resize") {
        const next = resizeColumns(
          g.original,
          g.columns,
          g.percentages,
          g.index,
          ((g.currentX - g.x) / g.width) * 100,
          g.width,
        );
        onchange(next, true);
        announcement = `${g.columns[g.index].label} column resized`;
      } else {
        const destination = dropSlot > g.index ? dropSlot - 1 : dropSlot;
        onchange(
          reorderColumn(
            g.original,
            g.columns.map((c) => c.id),
            g.id,
            destination,
          ),
          true,
        );
        announcement = `${g.columns[g.index].label} moved to column ${destination + 1}`;
      }
    }
    dragging = null;
    resizing = null;
    dropSlot = -1;
    void tick().then(() => {
      if (g.target.isConnected) g.target.focus({ preventScroll: true });
    });
  }
  function reorderKey(event: KeyboardEvent, index: number) {
    if (!event.altKey || !["ArrowLeft", "ArrowRight"].includes(event.key))
      return;
    event.preventDefault();
    const target = event.currentTarget as HTMLElement;
    const label = columns[index].label;
    const destination = Math.max(
      0,
      Math.min(
        columns.length - 1,
        index + (event.key === "ArrowLeft" ? -1 : 1),
      ),
    );
    onchange(
      reorderColumn(
        layout,
        columns.map((c) => c.id),
        columns[index].id,
        destination,
      ),
      true,
    );
    announcement = `${label} moved to column ${destination + 1}`;
    void tick().then(() => target.focus({ preventScroll: true }));
  }
  function resizeKey(event: KeyboardEvent, index: number) {
    if (!["ArrowLeft", "ArrowRight"].includes(event.key)) return;
    event.preventDefault();
    const delta =
      (event.key === "ArrowLeft" ? -1 : 1) * (event.shiftKey ? 5 : 1);
    onchange(
      resizeColumns(layout, columns, percentages, index, delta, tableWidth),
      true,
    );
  }
  onMount(() => {
    const cancel = (event: KeyboardEvent) => {
      if (event.key === "Escape" && gesture) {
        event.preventDefault();
        finish(false);
      }
    };
    window.addEventListener("keydown", cancel);
    return () => {
      cancelAnimationFrame(frame);
      window.removeEventListener("keydown", cancel);
    };
  });
</script>

<thead>
  <tr bind:this={row}>
    {#each columns as column, index (column.id)}
      <th
        scope="col"
        class:dragging={dragging === column.id}
        class:drop-before={!!dragging && dropSlot === index}
        class:drop-after={!!dragging &&
          index === columns.length - 1 &&
          dropSlot === columns.length}
        aria-sort={display.sort === column.id
          ? display.direction === "asc"
            ? "ascending"
            : "descending"
          : undefined}
      >
        <button
          class="column-name"
          aria-label={sortable(column.id)
            ? `Sort by ${column.label}`
            : `Move ${column.label} column`}
          title={sortable(column.id)
            ? "Click to sort · Click again to reverse · Drag to reorder · Alt + ← / →"
            : "Drag to reorder · Alt + ← / →"}
          onclick={(event) => sort(event, column.id)}
          onpointerdown={(event) => begin(event, "reorder", index)}
          onpointermove={move}
          onpointerup={() => finish(true)}
          onpointercancel={() => finish(false)}
          onlostpointercapture={() => finish(false)}
          onkeydown={(event) => reorderKey(event, index)}
        >
          {column.label}{#if display.sort === column.id}<span aria-hidden="true"
              >{display.direction === "asc" ? "↑" : "↓"}</span
            >{/if}
        </button>
        {#if index < columns.length - 1}
          <!-- svelte-ignore a11y_no_noninteractive_tabindex, a11y_no_noninteractive_element_interactions (Focusable separators are interactive column resize controls.) -->
          <div
            class="resize"
            class:active={resizing === column.id}
            role="separator"
            tabindex="0"
            aria-orientation="vertical"
            aria-label={`Resize ${column.label} column`}
            aria-valuemin={Math.round((column.minimum / tableWidth) * 100)}
            aria-valuemax={Math.round(
              percentages[column.id] +
                percentages[columns[index + 1].id] -
                (columns[index + 1].minimum / tableWidth) * 100,
            )}
            aria-valuenow={Math.round(percentages[column.id])}
            aria-valuetext={`${Math.round(percentages[column.id])} percent`}
            title="Drag to resize · ← / →"
            onpointerdown={(event) => begin(event, "resize", index)}
            onpointermove={move}
            onpointerup={() => finish(true)}
            onpointercancel={() => finish(false)}
            onlostpointercapture={() => finish(false)}
            onkeydown={(event) => resizeKey(event, index)}
          ></div>
        {/if}
        {#if dragging === column.id}<div
            class="column-ghost"
            aria-hidden="true"
            style:left={`${ghostX}px`}
            style:top={`${ghostY}px`}
          >
            {column.label}
          </div>{/if}
        {#if index === 0}<span class="sr-only" aria-live="polite"
            >{announcement}</span
          >{/if}
      </th>
    {/each}
  </tr>
</thead>

<style>
  th {
    position: relative;
    padding: 0;
    border-bottom: 1px solid var(--panel-border);
    text-align: left;
    color: var(--muted);
    font-size: 11px;
    font-weight: 500;
  }
  .column-name {
    display: flex;
    align-items: center;
    gap: 4px;
    width: 100%;
    padding: 11px 10px;
    border: 0;
    background: transparent;
    color: inherit;
    font: inherit;
    cursor: grab;
    text-align: left;
    touch-action: none;
    user-select: none;
    overflow: hidden;
    white-space: nowrap;
  }
  .column-name:hover {
    color: var(--text);
    background: var(--hover-surface);
  }
  .column-name:active {
    cursor: grabbing;
  }
  .dragging .column-name {
    opacity: 0.4;
  }
  .resize {
    position: absolute;
    z-index: 1;
    right: -5px;
    top: 4px;
    bottom: 4px;
    width: 10px;
    cursor: col-resize;
    touch-action: none;
  }
  .resize::after {
    content: "";
    position: absolute;
    left: 4px;
    top: 3px;
    bottom: 3px;
    width: 2px;
    border-radius: 2px;
    background: var(--panel-border);
    opacity: 0;
    transition: opacity 120ms ease;
  }
  th:hover .resize::after {
    opacity: 1;
  }
  .resize:hover::after,
  .resize.active::after,
  .resize:focus-visible::after {
    opacity: 1;
    background: var(--accent);
  }
  .resize:focus-visible,
  .column-name:focus-visible {
    outline: 2px solid var(--accent);
    outline-offset: -2px;
  }
  .drop-before::before,
  .drop-after::after {
    content: "";
    position: absolute;
    top: 0;
    bottom: 0;
    width: 2px;
    background: var(--accent);
    z-index: 2;
  }
  .drop-before::before {
    left: 0;
  }
  .drop-after::after {
    right: 0;
  }
  .column-ghost {
    position: fixed;
    z-index: 100;
    pointer-events: none;
    padding: 8px 14px;
    border-radius: 6px;
    background: var(--surface);
    border: 1px solid var(--panel-border);
    box-shadow: 0 4px 16px #0003;
    font-size: 12px;
    color: var(--text);
  }
  .sr-only {
    position: absolute;
    width: 1px;
    height: 1px;
    padding: 0;
    overflow: hidden;
    clip-path: inset(50%);
    white-space: nowrap;
  }
  @media (prefers-reduced-motion: reduce) {
    .resize::after {
      transition: none;
    }
  }
</style>
