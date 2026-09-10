<script lang="ts">
  import { propertyOptions } from "$lib/task-properties";
  import { anchoredPopover } from "$lib/anchored-popover";
  import { api, errorMessage } from "$lib/api";
  import { personName, type TaskConfig, type TaskPerson } from "$lib/tasks";
  let {
    workspace,
    config,
    priorities = $bindable<string[]>([]),
    types = $bindable<string[]>([]),
    sizes = $bindable<string[]>([]),
    statuses = $bindable<string[]>([]),
    assignees = $bindable<string[]>([]),
    unassigned = $bindable(false),
  }: {
    workspace: string;
    config: TaskConfig;
    priorities?: string[];
    types?: string[];
    sizes?: string[];
    statuses?: string[];
    assignees?: string[];
    unassigned?: boolean;
  } = $props();
  const id = $props.id();
  let trigger: HTMLButtonElement, popup: HTMLDivElement;
  let open = $state(false),
    query = $state(""),
    error = $state(""),
    loading = $state(false);
  let people = $state<TaskPerson[]>([]);
  let known = $state<Record<string, TaskPerson>>({});

  const count = $derived(
    priorities.length +
      types.length +
      sizes.length +
      statuses.length +
      assignees.length +
      (unassigned ? 1 : 0),
  );
  function toggle(values: string[], value: string) {
    return values.includes(value)
      ? values.filter((id) => id !== value)
      : [...values, value];
  }
  function clear() {
    priorities = [];
    types = [];
    sizes = [];
    statuses = [];
    assignees = [];
    unassigned = false;
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
            checked={statuses.includes(status.id)}
            onchange={() => (statuses = toggle(statuses, status.id))}
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
          checked={unassigned}
          onchange={(event) => (unassigned = event.currentTarget.checked)}
        /><span>Unassigned</span></label
      >
      {#each assignees.filter((id) => !people.some((p) => p.id === id)) as personID}<label
          ><input
            type="checkbox"
            checked
            onchange={() => (assignees = toggle(assignees, personID))}
          /><span
            >{known[personID]
              ? personName(known[personID])
              : "Selected account"}</span
          ></label
        >{/each}
      {#each people as person}<label
          ><input
            type="checkbox"
            checked={assignees.includes(person.id)}
            onchange={() => (assignees = toggle(assignees, person.id))}
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
    <fieldset>
      <legend>Priority</legend>
      <div class="property-options">
        {#each propertyOptions("priority") as option}<button
            class="filter-pill"
            aria-pressed={priorities.includes(option.value)}
            onclick={() => (priorities = toggle(priorities, option.value))}
            >{option.label}</button
          >{/each}
      </div>
    </fieldset>
    <fieldset>
      <legend>Type</legend>
      <div class="property-options">
        {#each propertyOptions("type") as option}<button
            class="filter-pill"
            aria-pressed={types.includes(option.value)}
            onclick={() => (types = toggle(types, option.value))}
            >{option.label}</button
          >{/each}
      </div>
    </fieldset>
    <fieldset>
      <legend>Size</legend>
      <div class="property-options">
        {#each propertyOptions("size") as option}<button
            class="filter-pill"
            aria-pressed={sizes.includes(option.value)}
            onclick={() => (sizes = toggle(sizes, option.value))}
            >{option.label}</button
          >{/each}
      </div>
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
</style>
