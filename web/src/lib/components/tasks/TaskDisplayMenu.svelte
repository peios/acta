<script lang="ts">
  import { taskProperties } from "$lib/task-properties";
  import { anchoredPopover } from "$lib/anchored-popover";
  import { taskColumns } from "$lib/task-table-layout.js";
  import OptionPicker from "../OptionPicker.svelte";
  import { type ViewDisplay } from "$lib/task-views.js";
  let { display = $bindable<ViewDisplay>() }: { display: ViewDisplay } =
    $props();
  const id = $props.id();
  const properties = taskColumns
    .filter((column) => !column.required)
    .map((column) => ({ value: column.id, label: column.label }))
    .sort((a, b) => a.label.localeCompare(b.label));
  const visibleProperties = $derived(
    properties.filter((p) => display.columns.includes(p.value)),
  );
  const availableProperties = $derived(
    properties.filter((p) => !display.columns.includes(p.value)),
  );
  let trigger: HTMLButtonElement;
  let open = $state(false);

  function column(key: string) {
    display = {
      ...display,
      columns: display.columns.includes(key)
        ? display.columns.filter((c) => c !== key)
        : [...display.columns, key],
    };
  }
</script>

<button
  class="display-button"
  class:open
  bind:this={trigger}
  popovertarget={id}
  aria-label="Display"
  title="Display options"
  aria-haspopup="dialog"
  aria-expanded={open}
>
  <svg viewBox="0 0 20 20" aria-hidden="true"
    ><path d="M3 5h14M3 10h14M3 15h14M7 3v4M13 8v4M8 13v4" /></svg
  ><span class="button-label">Display</span>
</button>
<div
  {id}
  popover="auto"
  use:anchoredPopover={() => ({
    anchor: trigger,
    width: 340,
    height: 500,
    align: "end",
  })}
  role="dialog"
  aria-label="Display tasks"
  class="display-menu"
  onbeforetoggle={(event) => {
    open = event.newState === "open";
  }}
>
  <header>Display</header>
  <div class="segmented view-mode" role="group" aria-label="View type">
    <button
      aria-pressed={display.mode === "table"}
      onclick={() => (display.mode = "table")}
    >
      <svg viewBox="0 0 20 20" aria-hidden="true"
        ><path d="M6 5h11M6 10h11M6 15h11M3 5h.01M3 10h.01M3 15h.01" /></svg
      >Table
    </button>
    <button
      aria-pressed={display.mode === "board"}
      onclick={() => (display.mode = "board")}
    >
      <svg viewBox="0 0 20 20" aria-hidden="true"
        ><rect x="3" y="4" width="5" height="12" rx="1" /><rect
          x="12"
          y="4"
          width="5"
          height="8"
          rx="1"
        /></svg
      >Board
    </button>
  </div>
  <section aria-label="Layout">
    <div class="option">
      <span>Group by</span>
      <div class="choice">
        <OptionPicker
          label="Group by"
          value={display.group}
          options={[
            { value: "none", label: "No grouping" },
            ...taskProperties,
            { value: "status", label: "Status" },
            { value: "assignee", label: "Assignee" },
            { value: "agents", label: "Agents" },
          ]}
          onchange={(value) => (display.group = value)}
        />
      </div>
    </div>
    <div class="option">
      <span>Sort by</span>
      <div class="choice sort-choice">
        <OptionPicker
          label="Sort by"
          value={display.sort}
          options={[
            { value: "number", label: "Task number" },
            { value: "title", label: "Title" },
            ...taskProperties,
            { value: "status", label: "Status" },
            { value: "created", label: "Created" },
            { value: "updated", label: "Last updated" },
          ]}
          onchange={(value) => (display.sort = value)}
        />
        <button
          class="order-button"
          aria-label={`Sort ${display.direction === "asc" ? "ascending" : "descending"}. Switch to ${display.direction === "asc" ? "descending" : "ascending"}`}
          title={display.direction === "asc"
            ? "Ascending — click for descending"
            : "Descending — click for ascending"}
          onclick={() =>
            (display.direction = display.direction === "asc" ? "desc" : "asc")}
        >
          <svg viewBox="0 0 20 20" aria-hidden="true"
            ><path d="M6 3v14" />{#if display.direction === "asc"}<path
                d="m3 6 3-3 3 3M12 5h2M12 10h4M12 15h6"
              />{:else}<path
                d="m3 14 3 3 3-3M12 5h6M12 10h4M12 15h2"
              />{/if}</svg
          >
        </button>
      </div>
    </div>
  </section>
  <fieldset>
    <legend>Properties</legend>
    <p class="hint">Task number and title are always shown.</p>
    <div class="properties" aria-label="Visible properties">
      {#each visibleProperties as property (property.value)}
        <span class="property-pill"
          >{property.label}<button
            aria-label={`Remove ${property.label}`}
            title={`Remove ${property.label}`}
            onclick={() => column(property.value)}
            ><svg viewBox="0 0 20 20" aria-hidden="true"
              ><path d="m6 6 8 8M14 6l-8 8" /></svg
            ></button
          ></span
        >
      {/each}
      <OptionPicker
        label="Add property"
        options={availableProperties}
        iconOnly
        onchange={column}
      />
    </div>
  </fieldset>
  <fieldset>
    <legend>Density</legend>
    <div class="segmented density" role="group" aria-label="Density">
      <button
        aria-pressed={display.density === "comfortable"}
        onclick={() => (display.density = "comfortable")}>Comfortable</button
      >
      <button
        aria-pressed={display.density === "compact"}
        onclick={() => (display.density = "compact")}>Compact</button
      >
    </div>
  </fieldset>
</div>

<style>
  .view-mode {
    margin: 0 4px 12px;
  }
  .view-mode button {
    display: flex;
    align-items: center;
    justify-content: center;
    gap: 8px;
    min-height: 36px;
  }
  .view-mode svg {
    width: 17px;
    height: 17px;
  }

  .display-button {
    display: inline-flex;
    align-items: center;
    gap: 7px;
    min-height: 34px;
    padding: 6px 9px;
    border: 0;
    border-radius: 7px;
    background: transparent;
    color: var(--muted);
    font-size: 12px;
  }
  .display-button:hover,
  .display-button.open {
    background: var(--hover-surface);
    color: var(--text);
  }
  svg {
    width: 15px;
    height: 15px;
    fill: none;
    stroke: currentColor;
    stroke-width: 1.4;
    stroke-linecap: round;
    stroke-linejoin: round;
  }
  .display-menu {
    position: fixed;
    inset: auto;
    margin: 0;
    width: min(340px, calc(100vw - 24px));
    padding: 8px;
    overflow: auto;
    border: 1px solid var(--panel-border);
    border-radius: 12px;
    background: var(--surface);
    color: var(--text);
    box-shadow: 0 12px 36px #0003;
  }
  header {
    padding: 6px 8px 12px;
    font-size: 13px;
    font-weight: 600;
  }
  section {
    padding: 0 4px;
  }
  .option {
    display: grid;
    grid-template-columns: 62px minmax(0, 1fr);
    align-items: center;
    gap: 12px;
    margin: 0;
    padding: 5px 4px;
    font-size: 12px;
  }
  .choice {
    min-width: 0;
  }
  .sort-choice {
    display: flex;
    align-items: center;
    gap: 5px;
  }
  .order-button {
    display: grid;
    place-items: center;
    width: 30px;
    height: 34px;
    flex-shrink: 0;
    border: 0;
    border-radius: 6px;
    padding: 6px;
    background: transparent;
    color: var(--muted);
  }
  .order-button:hover {
    background: var(--hover-surface);
    color: var(--text);
  }
  .properties {
    display: flex;
    flex-wrap: wrap;
    align-items: center;
    gap: 6px;
  }
  .property-pill {
    display: inline-flex;
    align-items: center;
    gap: 5px;
    padding: 3px 3px 3px 9px;
    border: 1px solid var(--panel-border);
    border-radius: 6px;
    background: var(--hover-surface);
    font-size: 11px;
  }
  .property-pill button {
    display: grid;
    place-items: center;
    width: 22px;
    height: 22px;
    padding: 3px;
    border: 0;
    border-radius: 4px;
    background: transparent;
    color: var(--muted);
  }
  .property-pill button:hover {
    background: var(--surface);
    color: var(--text);
  }
  fieldset {
    margin: 12px 0 0;
    padding: 8px;
    border: 0;
    border-top: 1px solid var(--panel-border);
    min-width: 0;
  }
  legend {
    font-size: 11px;
    color: var(--muted);
    padding: 0 4px;
  }
  .hint {
    font-size: 11px;
    color: var(--muted);
    margin: 2px 0 8px;
  }
  .segmented {
    display: flex;
    padding: 3px;
    gap: 3px;
    border-radius: 7px;
    background: var(--hover-surface);
  }
  .segmented button {
    flex: 1;
    border: 0;
    border-radius: 5px;
    background: transparent;
    color: var(--muted);
    padding: 7px 6px;
    font-size: 11px;
  }
  .segmented button[aria-pressed="true"] {
    color: var(--text);
    background: var(--surface);
    box-shadow: 0 1px 3px #0002;
  }
  @container tasks-list (max-width:620px) {
    .button-label {
      display: none;
    }
    .display-button {
      width: 34px;
      justify-content: center;
      padding: 6px;
    }
  }
</style>
