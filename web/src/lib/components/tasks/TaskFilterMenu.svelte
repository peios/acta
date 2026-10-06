<script lang="ts">
  import {
    propertyOptions,
    taskProperties,
    type TaskProperty,
  } from "$lib/task-properties";
  import { anchoredPopover } from "$lib/anchored-popover";
  import { api, errorMessage } from "$lib/api";
  import { personName, type TaskConfig, type TaskPerson } from "$lib/tasks";
  import { loadReleases, noRelease, type Release } from "$lib/releases";
  import { emptyViewFilters, type ViewFilters } from "$lib/task-views.js";
  let {
    workspace,
    config,
    filters = $bindable(),
  }: {
    workspace: string;
    config: TaskConfig;
    filters: ViewFilters;
  } = $props();
  let releaseList = $state<Release[]>([]),
    releaseError = $state("");
  $effect(() => {
    if (!open) return;
    const w = workspace;
    let cancelled = false;
    loadReleases(w)
      .then((r) => {
        if (!cancelled) {
          releaseList = r;
          releaseError = "";
        }
      })
      .catch((e) => {
        if (!cancelled) releaseError = errorMessage(e);
      });
    return () => {
      cancelled = true;
    };
  });
  const id = $props.id();
  let trigger: HTMLButtonElement, popup: HTMLDivElement;
  let open = $state(false),
    query = $state(""),
    error = $state(""),
    loading = $state(false);
  let people = $state<TaskPerson[]>([]);
  let known = $state<Record<string, TaskPerson>>({});

  const count = $derived(
    filters.priorities.length +
      filters.types.length +
      filters.sizes.length +
      filters.releases.length +
      filters.statuses.length +
      filters.assignees.length +
      (filters.unassigned ? 1 : 0),
  );
  type Selection = Exclude<keyof ViewFilters, "unassigned">;
  const propertyFilters: Record<TaskProperty, Selection> = {
    priority: "priorities",
    type: "types",
    size: "sizes",
  };
  function toggle(key: Selection, value: string) {
    const values = filters[key];
    filters[key] = values.includes(value)
      ? values.filter((id) => id !== value)
      : [...values, value];
  }
  function clear() {
    filters = emptyViewFilters();
  }

  $effect(() => {
    if (!open) return;
    const term = query,
      w = workspace;
    let cancelled = false;
    loading = true;
    const timer = setTimeout(
      async () => {
        try {
          const result = await api<{ people: TaskPerson[] }>(
            `workspaces/${w}/task-people?q=${encodeURIComponent(term)}`,
          );
          if (!cancelled) {
            people = result.people;
            known = {
              ...known,
              ...Object.fromEntries(people.map((p) => [p.id, p])),
            };
            error = "";
          }
        } catch (e) {
          if (!cancelled) error = errorMessage(e);
        } finally {
          if (!cancelled) loading = false;
        }
      },
      term ? 180 : 0,
    );
    return () => {
      cancelled = true;
      clearTimeout(timer);
    };
  });
</script>

<div class="filter-control">
  <button
    class="toolbar-button"
    class:active={count > 0}
    bind:this={trigger}
    popovertarget={id}
    aria-haspopup="dialog"
    aria-expanded={open}
    aria-label={count ? `Filter ${count}` : "Filter"}
    title={count ? `Filter tasks (${count} active)` : "Filter tasks"}
  >
    <svg viewBox="0 0 20 20" aria-hidden="true"
      ><path d="M3 4h14l-5.5 6.5V16l-3-1.5v-4Z" /></svg
    >
    <span class="button-label">Filter</span>
    {#if count}<span class="badge">{count}</span>{/if}
  </button>
  <div
    bind:this={popup}
    {id}
    popover="auto"
    use:anchoredPopover={() => ({
      anchor: trigger,
      width: 300,
      height: 500,
      align: "end",
    })}
    role="dialog"
    aria-label="Filter tasks"
    class="filter-menu"
    onbeforetoggle={(event) => {
      open = event.newState === "open";
      if (open) {
        query = "";
      }
    }}
  >
    <header>
      <strong>Filter tasks</strong><button
        class="clear"
        disabled={!count}
        onclick={clear}>Clear</button
      >
    </header>
    <fieldset>
      <legend>Status</legend>
      {#each config.statuses as status}<label
          ><input
            type="checkbox"
            checked={filters.statuses.includes(status.id)}
            onchange={() => toggle("statuses", status.id)}
          /><span>{status.name}</span></label
        >{/each}
    </fieldset>
    <fieldset>
      <legend>Assignee</legend>
      <input
        class="search"
        type="search"
        bind:value={query}
        aria-label="Search assignees to filter"
        placeholder="Search people or agents…"
      />
      <label
        ><input
          type="checkbox"
          checked={filters.unassigned}
          onchange={(event) =>
            (filters.unassigned = event.currentTarget.checked)}
        /><span>Unassigned</span></label
      >
      {#each filters.assignees.filter((id) => !people.some((p) => p.id === id)) as personID}<label
          ><input
            type="checkbox"
            checked
            onchange={() => toggle("assignees", personID)}
          /><span
            >{known[personID]
              ? personName(known[personID])
              : "Selected account"}</span
          ></label
        >{/each}
      {#each people as person}<label
          ><input
            type="checkbox"
            checked={filters.assignees.includes(person.id)}
            onchange={() => toggle("assignees", person.id)}
          /><span>{personName(person)}<small>@{person.username}</small></span
          ></label
        >{/each}
      {#if loading}<p class="hint" role="status">Loading…</p>{:else if error}<p
          class="notice error"
          role="alert"
        >
          {error}
        </p>{:else if !people.length}<p class="hint">
          No matching people.
        </p>{/if}
    </fieldset>
    {#each taskProperties as property}{@const key =
        propertyFilters[property.value]}
      <fieldset>
        <legend>{property.label}</legend>
        <div class="property-options">
          {#each propertyOptions(property.value) as option}<button
              class="filter-pill"
              aria-pressed={filters[key].includes(option.value)}
              onclick={() => toggle(key, option.value)}>{option.label}</button
            >{/each}
        </div>
      </fieldset>{/each}
    <fieldset>
      <legend>Release</legend>
      <div class="property-options">
        {#each [{ id: noRelease, name: "None" }, ...releaseList] as option}<button
            class="filter-pill"
            aria-pressed={filters.releases.includes(option.id)}
            onclick={() => toggle("releases", option.id)}>{option.name}</button
          >{/each}
      </div>
      {#if releaseError}<p class="notice error" role="alert">
          {releaseError}
        </p>{/if}
    </fieldset>
    <footer>
      <span>Match any selection in each category.</span><button
        class="clear"
        onclick={() => popup.hidePopover()}>Done</button
      >
    </footer>
  </div>
</div>

<style>
  .property-options {
    display: flex;
    flex-wrap: wrap;
    gap: 5px;
    padding: 4px 8px;
  }
  .filter-pill {
    border: 1px solid var(--panel-border);
    border-radius: 6px;
    background: transparent;
    color: var(--muted);
    padding: 5px 8px;
    font-size: 12px;
  }
  .filter-pill[aria-pressed="true"] {
    color: var(--accent);
    background: var(--scope-active);
    border-color: var(--accent);
  }
  .toolbar-button {
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
  .toolbar-button:hover,
  .toolbar-button.active {
    color: var(--text);
    background: var(--hover-surface);
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
  .badge {
    font-size: 10px;
    background: var(--scope-active);
    border-radius: 5px;
    padding: 1px 5px;
  }
  .filter-menu {
    position: fixed;
    inset: auto;
    margin: 0;
    width: min(300px, calc(100vw - 24px));
    padding: 8px;
    overflow: auto;
    border: 1px solid var(--panel-border);
    border-radius: 12px;
    background: var(--surface);
    color: var(--text);
    box-shadow: 0 12px 36px #0003;
  }
  header,
  footer {
    display: flex;
    align-items: center;
    justify-content: space-between;
    gap: 10px;
    padding: 4px 8px;
  }
  header strong {
    font-size: 13px;
    font-weight: 600;
  }
  .clear {
    border: 0;
    border-radius: 6px;
    padding: 6px;
    background: transparent;
    color: var(--muted);
    font-size: 12px;
  }
  .clear:hover:not(:disabled) {
    color: var(--text);
    background: var(--hover-surface);
  }
  fieldset {
    border: 0;
    border-top: 1px solid var(--panel-border);
    padding: 8px 0;
    margin: 8px 0 0;
    min-width: 0;
  }
  legend {
    padding: 0 8px;
    color: var(--muted);
    font-size: 11px;
  }
  label {
    font-weight: 400;
    display: flex;
    align-items: center;
    gap: 10px;
    padding: 8px;
    border-radius: 7px;
    cursor: pointer;
    font-size: 13px;
  }
  label:hover {
    background: var(--hover-surface);
  }
  label input {
    width: 15px;
    height: 15px;
    flex-shrink: 0;
    accent-color: var(--accent);
  }
  label span {
    overflow-wrap: anywhere;
  }
  small {
    display: block;
    margin-top: 2px;
    color: var(--muted);
    font-size: 11px;
  }
  .search {
    width: 100%;
    margin-bottom: 6px;
    padding: 9px 10px;
    background: var(--hover-surface);
    border: 0;
    border-radius: 7px;
    font-size: 12px;
  }
  footer {
    border-top: 1px solid var(--panel-border);
  }
  footer span {
    color: var(--muted);
    font-size: 10px;
  }
  .hint {
    padding: 0 8px;
    font-size: 12px;
  }
  @container tasks-list (max-width:620px) {
    .button-label,
    .badge {
      display: none;
    }
    .toolbar-button {
      width: 34px;
      justify-content: center;
      padding: 6px;
    }
  }
  @media (pointer: coarse) {
    label {
      min-height: 44px;
    }
  }
</style>
