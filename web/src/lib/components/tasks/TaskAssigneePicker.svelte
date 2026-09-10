<script lang="ts">
  import { anchoredPopover } from "$lib/anchored-popover";
  import { api, errorMessage } from "$lib/api";
  import { personName, type TaskPerson } from "$lib/tasks";

  let {
    workspace,
    assignees,
    disabled,
    onchange,
  }: {
    workspace: string;
    assignees: TaskPerson[];
    disabled: boolean;
    onchange: (ids: string[]) => Promise<boolean>;
  } = $props();
  const id = $props.id();
  let trigger: HTMLButtonElement;
  let popup: HTMLDivElement;
  let searchInput: HTMLInputElement;
  let open = $state(false),
    query = $state(""),
    loading = $state(false),
    error = $state("");
  let people = $state<TaskPerson[]>([]);
  let pending = $state<string[] | null>(null);
  const selected = $derived(pending ?? assignees.map((person) => person.id));
  const rows = $derived([
    ...assignees.filter(
      (person) =>
        !people.some((result) => result.id === person.id) &&
        `${personName(person)} ${person.username}`
          .toLowerCase()
          .includes(query.trim().toLowerCase()),
    ),
    ...people,
  ]);

  $effect(() => {
    if (!open) return;
    const term = query.trim(),
      workspaceID = workspace;
    let cancelled = false;
    loading = true;
    error = "";
    const timer = setTimeout(
      async () => {
        try {
          const result = await api<{ people: TaskPerson[] }>(
            `workspaces/${workspaceID}/task-people?q=${encodeURIComponent(term)}`,
          );
          if (!cancelled) people = result.people;
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
  async function toggle(person: TaskPerson) {
    if (disabled || pending) return;
    const ids = assignees.map((p) => p.id);
    pending = ids.includes(person.id)
      ? ids.filter((v) => v !== person.id)
      : [...ids, person.id];
    error = "";
    try {
      if (!(await onchange(pending)))
        error = "Couldn’t update assignees. Please try again.";
    } finally {
      pending = null;
    }
  }
</script>

<button
  class="assign-button"
  bind:this={trigger}
  aria-label="Change assignees"
  title="Change assignees"
  popovertarget={id}
  aria-haspopup="dialog"
  aria-expanded={open}
>
  <svg
    viewBox="0 0 24 24"
    width="16"
    height="16"
    fill="none"
    stroke="currentColor"
    stroke-width="1.6"
    aria-hidden="true"><path d="M12 5v14M5 12h14" /></svg
  >
</button>
<div
  popover="auto"
  use:anchoredPopover={() => ({
    anchor: trigger,
    width: 320,
    height: 380,
    align: "start",
  })}
  {id}
  bind:this={popup}
  role="dialog"
  aria-label="Assign task"
  class="assignee-picker"
  onbeforetoggle={(event) => {
    open = event.newState === "open";
    if (open) {
      query = "";
      people = [];
    }
  }}
  ontoggle={(event) => {
    if (event.newState === "open") searchInput?.focus();
  }}
>
  <div class="search-row">
    <svg viewBox="0 0 24 24" aria-hidden="true"
      ><circle cx="10.5" cy="10.5" r="6.5" /><path d="m16 16 4.5 4.5" /></svg
    >
    <input
      type="search"
      bind:this={searchInput}
      bind:value={query}
      aria-label="Search assignees"
      placeholder="Search people or agents…"
      autocomplete="off"
    />
  </div>
  <div class="people" aria-label="People and agents" aria-busy={loading}>
    {#if loading}<p class="empty" role="status">Searching…</p>
    {:else}
      {#each rows as person (person.id)}
        <label class="person" class:selected={selected.includes(person.id)}>
          <input
            type="checkbox"
            aria-label={`Assign ${personName(person)} (@${person.username})`}
            checked={selected.includes(person.id)}
            disabled={disabled ||
              pending !== null ||
              (!person.available && !selected.includes(person.id))}
            onchange={() => void toggle(person)}
          />
          <span class="avatar" aria-hidden="true"
            >{personName(person).slice(0, 1).toUpperCase()}</span
          >
          <span class="identity"
            ><span class="name"
              >{personName(person)}{#if person.agent}<small class="agent"
                  >Agent</small
                >{/if}</span
            >
            <span class="username"
              >@{person.username}{!person.available
                ? " · Access removed"
                : ""}</span
            ></span
          >
          <span class="selection" aria-hidden="true"
            ><svg viewBox="0 0 20 20"><path d="m5 10 3 3 7-7" /></svg></span
          >
        </label>
      {:else}<p class="empty">No matching people or agents.</p>{/each}
    {/if}
  </div>
  {#if error}<p class="picker-error" role="alert">{error}</p>{/if}
  <div class="picker-footer">
    <span role="status"
      >{pending ? "Saving…" : `${assignees.length} assigned`}</span
    >
    <button popovertarget={id} popovertargetaction="hide">Done</button>
  </div>
</div>

<style>
  .assign-button {
    display: inline-grid;
    place-items: center;
    width: 30px;
    height: 30px;
    padding: 0;
    border: 1px dashed var(--panel-border);
    border-radius: 50%;
    background: transparent;
    color: var(--muted);
  }
  .assign-button:hover,
  .assign-button[aria-expanded="true"] {
    background: var(--hover-surface);
    color: var(--text);
  }
  .assignee-picker {
    position: fixed;
    inset: auto;
    margin: 0;
    padding: 6px;
    border: 1px solid var(--panel-border);
    border-radius: 14px;
    background: var(--surface);
    color: var(--text);
    box-shadow: 0 16px 48px #0003;
    overflow: hidden;
  }
  .assignee-picker:popover-open {
    display: flex;
    flex-direction: column;
  }
  .search-row {
    display: flex;
    align-items: center;
    gap: 9px;
    flex-shrink: 0;
    margin: 4px 4px 8px;
    padding: 0 10px;
    border-radius: 9px;
    background: var(--hover-surface);
  }
  svg {
    width: 18px;
    height: 18px;
    fill: none;
    stroke: currentColor;
    stroke-width: 1.5;
    stroke-linecap: round;
    stroke-linejoin: round;
    flex-shrink: 0;
  }
  .search-row svg {
    color: var(--muted);
  }
  .search-row input {
    height: 40px;
    padding: 0;
    border: 0;
    background: transparent;
    font-size: 13px;
  }
  .search-row input:focus {
    outline: none;
  }
  .search-row:focus-within {
    box-shadow: 0 0 0 1px color-mix(in srgb, var(--muted) 35%, transparent);
  }
  .people {
    overflow: auto;
    min-height: 0;
    padding: 0 2px;
  }
  .person {
    position: relative;
    display: flex;
    align-items: center;
    gap: 10px;
    min-height: 56px;
    padding: 8px;
    border-radius: 9px;
    cursor: pointer;
    font-weight: 400;
  }
  .person:hover,
  .person.selected {
    background: var(--hover-surface);
  }
  .person:focus-within {
    outline: 2px solid var(--focus);
    outline-offset: -2px;
  }
  .person input {
    position: absolute;
    width: 1px;
    height: 1px;
    padding: 0;
    border: 0;
    overflow: hidden;
    clip-path: inset(50%);
  }
  .person:has(input:disabled) {
    cursor: default;
  }
  .avatar {
    display: grid;
    place-items: center;
    width: 32px;
    height: 32px;
    flex-shrink: 0;
    border-radius: 50%;
    background: var(--scope-active);
    color: var(--accent);
    font-size: 12px;
    font-weight: 600;
  }
  .identity {
    flex: 1;
    min-width: 0;
  }
  .name {
    display: flex;
    align-items: center;
    gap: 6px;
    flex-wrap: wrap;
    font-size: 13px;
    overflow-wrap: anywhere;
  }
  .username {
    display: block;
    color: var(--muted);
    font-size: 11px;
    margin-top: 3px;
    overflow-wrap: anywhere;
  }
  .agent {
    font-size: 10px;
    color: var(--muted);
    border: 1px solid var(--panel-border);
    border-radius: 4px;
    padding: 1px 4px;
  }
  .selection {
    color: var(--accent);
    opacity: 0;
  }
  .selected .selection {
    opacity: 1;
  }
  .empty {
    padding: 18px 12px;
    margin: 0;
    font-size: 13px;
    color: var(--muted);
  }
  .picker-error {
    padding: 8px;
    font-size: 12px;
    color: var(--error-text);
  }
  .picker-footer {
    display: flex;
    align-items: center;
    justify-content: space-between;
    flex-shrink: 0;
    padding: 8px 8px 2px;
    margin-top: 6px;
    border-top: 1px solid var(--panel-border);
    font-size: 11px;
    color: var(--muted);
  }
  .picker-footer button {
    border: 0;
    border-radius: 6px;
    background: transparent;
    padding: 6px 8px;
    font-size: 12px;
    color: var(--text);
  }
  .picker-footer button:hover {
    background: var(--hover-surface);
  }
</style>
