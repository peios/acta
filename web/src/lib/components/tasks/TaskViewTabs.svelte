<script lang="ts">
  import "$lib/horizontal-scroll.css";
  import {
    readTableLayout,
    saveTableLayout,
    removeTableLayout,
    defaultTableLayout,
    type TableLayout,
  } from "$lib/task-table-layout.js";
  import { useAccount } from "$lib/account-context";
  import { tick, untrack } from "svelte";
  import { dragScroll } from "$lib/drag-scroll";
  import { api, APIError, errorMessage } from "$lib/api";
  import {
    lastSelectedView,
    rememberSelectedView,
    copyViewSettings,
    sameViewSettings,
    defaultViewSettings,
    type ViewSettings,
  } from "$lib/task-views.js";
  import type { TaskConfig } from "$lib/tasks";
  import TaskFilterMenu from "./TaskFilterMenu.svelte";
  import TaskDisplayMenu from "./TaskDisplayMenu.svelte";
  type View = {
    board: string;
    id: string;
    name: string;
    filters: ViewSettings["filters"];
    display: ViewSettings["display"];
    version: number;
  };
  let {
    board = "tasks",
    archived = $bindable(false),
    workspace,
    config,
    panelID,
    onchange,
    onlabelchange,
    onviewchange,
  }: {
    board?: string;
    archived?: boolean;
    workspace: string;
    config: TaskConfig;
    panelID: string;
    onchange: (settings: ViewSettings) => void;
    onlabelchange: (id: string) => void;
    onviewchange: (id: string) => void;
  } = $props();
  const id = $props.id();
  const account = useAccount();
  const accountID = $derived(account.account.id);
  // A refreshed workspace object must not reset the selected tab or its drafts.
  const workspaceID = $derived(workspace);
  const selectionID = $derived(
    board === "tasks" ? workspace : `${workspace}:${board}`,
  );
  let views = $state<View[]>([]),
    drafts = $state<Record<string, ViewSettings>>({}),
    activeID = $state("");
  let loading = $state(true),
    loadError = $state(""),
    savingID = $state("");
  let errors = $state<Record<string, string>>({});
  let tabs = $state<HTMLDivElement>();
  let dialog: HTMLDialogElement, nameInput: HTMLInputElement;
  let name = $state(""),
    creating = $state(false),
    createError = $state("");
  let createSourceID = $state("");
  let createLayout: TableLayout = defaultTableLayout();
  let createSettings = $state<ViewSettings>(defaultViewSettings());
  let editID = $state(""),
    editName = $state(""),
    editError = $state("");
  let changing = $state(false),
    confirmingDelete = $state(false);
  let editInput = $state<HTMLInputElement>();
  let deleteDialog: HTMLDialogElement;
  let retry = $state(0);
  let archiveSettings = $state<ViewSettings>(defaultViewSettings());
  const settings = $derived(archived ? archiveSettings : drafts[activeID]);
  const active = $derived(views.find((v) => v.id === activeID));
  const dirty = (v: View) =>
    !!drafts[v.id] && !sameViewSettings(v, drafts[v.id]);
  $effect(() => {
    const w = workspaceID;
    const owner = accountID;
    void retry;
    let cancelled = false;
    loading = true;
    loadError = "";
    views = [];
    drafts = {};
    activeID = "";
    errors = {};
    void api<{ views: View[] }>(`workspaces/${w}/task-views?board=${board}`)
      .then((result) => {
        if (cancelled) return;
        views = result.views.filter((v) => v.board === board);
        drafts = Object.fromEntries(
          views.map((v) => [v.id, copyViewSettings(v)]),
        );
        void choose(lastSelectedView(owner, selectionID, views));
      })
      .catch((e) => {
        if (!cancelled) loadError = errorMessage(e);
      })
      .finally(() => {
        if (!cancelled) loading = false;
      });
    return () => {
      cancelled = true;
    };
  });
  $effect(() => {
    if (!active || !drafts[activeID]) return;
    onchange(copyViewSettings(settings));
    onlabelchange(archived ? `${id}-archived` : `${id}-${activeID}`);
  });
  $effect(() => {
    const preset = archived
      ? board === "tasks"
        ? "__archived"
        : "__archived_backlog"
      : activeID;
    untrack(() => onviewchange(preset));
  });
  export function sortBy(field: string) {
    if (!active) return;
    const current = settings.display;
    settings.display = {
      ...current,
      sort: field,
      direction:
        current.sort === field && current.direction === "asc" ? "desc" : "asc",
    };
  }
  async function choose(next: string, focus = false) {
    activeID = next;
    rememberSelectedView(accountID, selectionID, next);
    await tick();
    if (activeID !== next) return;
    const button = tabs?.querySelector<HTMLButtonElement>(
      `[id="${id}-${next}"]`,
    );
    button?.scrollIntoView({ block: "nearest", inline: "nearest" });
    if (focus) button?.focus({ preventScroll: true });
  }
  function keydown(event: KeyboardEvent, index: number) {
    if (event.key === "F2") {
      event.preventDefault();
      void startRename(views[index]);
      return;
    }
    let next = index;
    if (event.key === "ArrowRight") next = (index + 1) % views.length;
    else if (event.key === "ArrowLeft")
      next = (index + views.length - 1) % views.length;
    else if (event.key === "Home") next = 0;
    else if (event.key === "End") next = views.length - 1;
    else return;
    event.preventDefault();
    void choose(views[next].id, true);
  }
  async function save(view: View) {
    if (savingID || changing || editID || !dirty(view)) return;
    const w = workspace,
      submitted = copyViewSettings(drafts[view.id]);
    savingID = view.id;
    errors[view.id] = "";
    try {
      const saved = await api<View>(`workspaces/${w}/task-views/${view.id}`, {
        version: view.version,
        ...submitted,
        board,
      });
      if (workspace !== w) return;
      views = views.map((v) => (v.id === saved.id ? saved : v));
      if (sameViewSettings(drafts[view.id], submitted))
        drafts[view.id] = copyViewSettings(saved);
    } catch (e) {
      if (workspace !== w) return;
      if (e instanceof APIError && e.status === 409) {
        errors[view.id] =
          "This tab was changed in another browser. Your view changes are still here. Undo restores the latest saved view; Save applies your version.";
        try {
          const result = await api<{ views: View[] }>(
            `workspaces/${w}/task-views?board=${board}`,
          );
          if (workspace !== w) return;
          const latest = result.views.find((v) => v.id === view.id);
          if (latest) views = views.map((v) => (v.id === view.id ? latest : v));
        } catch {
          errors[view.id] =
            "Couldn’t load the latest saved tab. Your view changes are still here. Try saving again when connected.";
        }
      } else errors[view.id] = errorMessage(e);
    } finally {
      savingID = "";
    }
  }
  async function startRename(view: View) {
    if (savingID || changing) return;
    await choose(view.id);
    editID = view.id;
    editName = view.name;
    editError = "";
    await tick();
    editInput?.focus();
    editInput?.select();
    editInput?.parentElement?.scrollIntoView({
      block: "nearest",
      inline: "nearest",
    });
  }
  async function cancelRename() {
    if (changing) return;
    const previous = editID;
    editID = "";
    editError = "";
    if (previous) await choose(previous, true);
  }
  async function editFailure(error: unknown, view: View) {
    editError = errorMessage(error);
    if (error instanceof APIError && error.status === 409) {
      try {
        const result = await api<{ views: View[] }>(
          `workspaces/${workspace}/task-views?board=${board}`,
        );
        const latest = result.views.find((v) => v.id === view.id);
        if (latest) views = views.map((v) => (v.id === view.id ? latest : v));
        editError =
          "This tab changed in another browser. Your edits are still here; try again to apply them.";
      } catch {
        editError =
          "Couldn’t load the latest tab. Your edits are still here. Try again when connected.";
      }
    }
  }
  async function rename(focus = false) {
    if (!editID || changing || confirmingDelete) return;
    const view = views.find((v) => v.id === editID);
    if (!view) return;
    if (editName.trim() === view.name) {
      editID = "";
      editError = "";
      if (focus) await choose(view.id, true);
      return;
    }
    changing = true;
    editError = "";
    try {
      const renamed = await api<View>(
        `workspaces/${workspace}/task-views/${view.id}/rename`,
        { name: editName, version: view.version },
      );
      views = views.map((v) => (v.id === renamed.id ? renamed : v));
      editID = "";
      if (focus) await choose(view.id, true);
    } catch (error) {
      await editFailure(error, view);
    } finally {
      changing = false;
    }
  }
  function editorBlur(event: FocusEvent) {
    if (
      event.currentTarget instanceof HTMLElement &&
      event.relatedTarget instanceof Node &&
      event.currentTarget.contains(event.relatedTarget)
    )
      return;
    void rename();
  }
  function requestDelete() {
    const view = views.find((v) => v.id === editID);
    if (!view || changing || views.length <= 1) return;
    if (dirty(view) || editName.trim() !== view.name) {
      confirmingDelete = true;
      deleteDialog.showModal();
    } else void remove();
  }
  async function remove() {
    const view = views.find((v) => v.id === editID);
    if (!view || changing || views.length <= 1) return;
    changing = true;
    editError = "";
    try {
      await api(`workspaces/${workspace}/task-views/${view.id}/delete`, {
        version: view.version,
      });
      const index = views.findIndex((v) => v.id === view.id);
      views = views.filter((v) => v.id !== view.id);
      removeTableLayout(workspace, view.id);
      delete drafts[view.id];
      delete errors[view.id];
      editID = "";
      deleteDialog.close();
      confirmingDelete = false;
      if (activeID === view.id)
        await choose(views[Math.min(index, views.length - 1)].id, true);
    } catch (error) {
      await editFailure(error, view);
    } finally {
      changing = false;
    }
  }
  function cancelDelete() {
    deleteDialog.close();
    confirmingDelete = false;
    editInput?.focus();
  }
  function undo() {
    if (!active || savingID === active.id) return;
    drafts[active.id] = copyViewSettings(active);
    errors[active.id] = "";
  }
  async function openCreate() {
    createLayout = readTableLayout(workspace, activeID);
    createSourceID = active && dirty(active) ? active.id : "";
    createSettings = createSourceID
      ? copyViewSettings(drafts[createSourceID])
      : defaultViewSettings();
    name = "";
    createError = "";
    dialog.showModal();
    await tick();
    nameInput.focus();
  }
  async function create() {
    if (creating) return;
    const w = workspace;
    creating = true;
    createError = "";
    try {
      const view = await api<View>(`workspaces/${w}/task-views`, {
        name,
        ...copyViewSettings(createSettings),
        board,
      });
      if (workspace !== w) return;
      const source = views.find((v) => v.id === createSourceID);
      if (source && sameViewSettings(drafts[source.id], createSettings)) {
        drafts[source.id] = copyViewSettings(source);
        errors[source.id] = "";
      }
      saveTableLayout(w, view.id, createLayout);
      views = [...views, view];
      drafts[view.id] = copyViewSettings(view);
      dialog.close();
      await choose(view.id, true);
    } catch (e) {
      createError = errorMessage(e);
    } finally {
      creating = false;
    }
  }
</script>

{#snippet saveIcon()}
  <svg viewBox="0 0 20 20" aria-hidden="true"
    ><path
      d="M4 3h10l3 3v10a1 1 0 0 1-1 1H4a1 1 0 0 1-1-1V4a1 1 0 0 1 1-1Z"
    /><path d="M6 3v5h7V3M6 17v-6h8v6" /></svg
  >
{/snippet}
<div class="view-bar">
  {#if archived}<button
      id={`${id}-archived`}
      class="archive-back"
      onclick={() => (archived = false)}
      ><span aria-hidden="true">←</span> Back to tasks</button
    >{:else}<div class="tabs-area">
      <div
        class="tabs horizontal-scroll"
        role="tablist"
        aria-label="Task views"
        bind:this={tabs}
        use:dragScroll
      >
        {#each views as view, index (view.id)}
          <div
            class="tab-item"
            role="presentation"
            class:active={activeID === view.id}
          >
            {#if editID === view.id}
              <div class="tab-editor" onfocusout={editorBlur}>
                <input
                  id={`${id}-${view.id}`}
                  class="tab-name"
                  aria-label="Tab name"
                  bind:this={editInput}
                  bind:value={editName}
                  maxlength="60"
                  disabled={changing}
                  aria-invalid={!!editError}
                  onkeydown={(e) => {
                    if (e.key === "Enter") {
                      e.preventDefault();
                      void rename(true);
                    }
                    if (e.key === "Escape") {
                      e.preventDefault();
                      void cancelRename();
                    }
                  }}
                />
                {#if views.length > 1}
                  <button
                    class="icon-button delete-tab"
                    aria-label={`Delete ${view.name}`}
                    title="Delete tab"
                    disabled={changing}
                    onpointerdown={(e) => e.preventDefault()}
                    onclick={requestDelete}
                  >
                    <svg viewBox="0 0 20 20" aria-hidden="true"
                      ><path
                        d="M3 5h14M7 5V3h6v2M5 5l1 12h8l1-12M8 8v6M12 8v6"
                      /></svg
                    >
                  </button>
                {/if}
              </div>
            {:else}
              <button
                role="tab"
                id={`${id}-${view.id}`}
                aria-controls={panelID}
                aria-selected={activeID === view.id}
                tabindex={activeID === view.id ? 0 : -1}
                title="Double-click to rename (F2)"
                ondblclick={() => void startRename(view)}
                onclick={() => void choose(view.id)}
                onkeydown={(e) => keydown(e, index)}>{view.name}</button
              >
            {/if}
            {#if dirty(view)}<button
                class="icon-button save"
                aria-label={`Save ${view.name}`}
                title={`Save ${view.name}`}
                disabled={!!savingID || changing || !!editID}
                onclick={() => void save(view)}
              >
                {#if savingID === view.id}<span
                    class="saving-dot"
                    aria-hidden="true"
                  ></span>{:else}{@render saveIcon()}{/if}
              </button>{/if}
          </div>
        {/each}
      </div>
      <button
        class="icon-button new-tab"
        aria-label="New tab"
        title="New tab"
        disabled={loading || !!loadError || !!savingID || changing || !!editID}
        onclick={() => void openCreate()}
      >
        <svg viewBox="0 0 20 20" aria-hidden="true"
          ><path d="M10 4v12M4 10h12" /></svg
        >
      </button>
    </div>
  {/if}
  <div class="view-options">
    {#if !archived && active && dirty(active)}<button
        class="icon-button"
        aria-label="Reset to saved view"
        title="Reset to saved view"
        disabled={savingID === activeID}
        onclick={undo}
      >
        <svg viewBox="0 0 20 20" aria-hidden="true"
          ><path d="M7 4 3 8l4 4M3 8h8a5 5 0 0 1 0 10" /></svg
        >
      </button>{/if}
    {#if settings}
      {#if !archived}<button
          class="icon-button"
          aria-label="Archived tasks"
          title="Archived tasks"
          onclick={() => {
            archiveSettings = defaultViewSettings();
            archiveSettings.display = copyViewSettings(settings).display;
            archived = true;
          }}
          ><svg viewBox="0 0 20 20" aria-hidden="true"
            ><path d="M3 7h14v10H3zM2 3h16v4H2zM7 10h6" /></svg
          ></button
        >{/if}
      <TaskFilterMenu
        {workspace}
        {config}
        bind:priorities={settings.filters.priorities}
        bind:types={settings.filters.types}
        bind:sizes={settings.filters.sizes}
        bind:statuses={settings.filters.statuses}
        bind:assignees={settings.filters.assignees}
        bind:unassigned={settings.filters.unassigned}
      />
    {/if}
    {#if settings}
      <TaskDisplayMenu bind:display={settings.display} />
    {/if}
  </div>
</div>
{#if loading}<p class="hint" role="status">Loading views…</p>{/if}
{#if loadError}<p class="notice error" role="alert">
    {loadError}<button class="secondary" onclick={() => retry++}>Retry</button>
  </p>{/if}
{#if editError && !confirmingDelete}<p class="notice error" role="alert">
    {editError}
  </p>{/if}
{#if active && errors[active.id]}<p class="notice error" role="alert">
    {errors[active.id]}
  </p>{/if}
<dialog
  bind:this={dialog}
  class="management-dialog"
  aria-label="New tab"
  oncancel={(e) => {
    if (creating) e.preventDefault();
  }}
>
  <h2>New tab</h2>
  <p class="hint">
    {createSourceID
      ? "Save your current filters and display into a new tab. The original tab will return to its saved view."
      : "A personal view for this workspace. Choose filters and display options after creating it."}
  </p>
  <form
    onsubmit={(e) => {
      e.preventDefault();
      void create();
    }}
  >
    <div class="field">
      <label for={`${id}-name`}>Name</label><input
        id={`${id}-name`}
        bind:this={nameInput}
        bind:value={name}
        required
        maxlength="60"
        disabled={creating}
      />
    </div>
    {#if createError}<p class="notice error" role="alert">{createError}</p>{/if}
    <div class="management-actions">
      <button
        type="button"
        class="secondary"
        disabled={creating}
        onclick={() => dialog.close()}>Cancel</button
      ><button class="primary" disabled={creating || !name.trim()}
        >{creating ? "Creating…" : "Create tab"}</button
      >
    </div>
  </form>
</dialog>

<dialog
  bind:this={deleteDialog}
  class="management-dialog"
  aria-label="Delete tab"
  oncancel={(e) => {
    e.preventDefault();
    if (!changing) cancelDelete();
  }}
>
  <h2>Delete tab?</h2>
  <p class="hint">
    This tab has unsaved changes. Deleting it will discard them.
  </p>
  {#if editError}<p class="notice error" role="alert">{editError}</p>{/if}
  <div class="management-actions">
    <button class="secondary" disabled={changing} onclick={cancelDelete}
      >Cancel</button
    >
    <button class="primary" disabled={changing} onclick={() => void remove()}
      >{changing ? "Deleting…" : "Delete tab"}</button
    >
  </div>
</dialog>

<style>
  .archive-back {
    margin-right: auto;
    flex-shrink: 0;
    border: 0;
    background: transparent;
    color: var(--muted);
    padding: 10px 0;
    display: flex;
    align-items: center;
    gap: 8px;
  }
  .archive-back:hover {
    color: var(--text);
  }
  .tab-editor {
    display: flex;
    align-items: center;
    gap: 4px;
    padding-bottom: 13px;
  }
  .tab-name {
    width: 160px;
    min-width: 80px;
    height: 31px;
    padding: 3px 7px;
    font-size: 13px;
    border-radius: 5px;
  }
  .delete-tab:hover:not(:disabled) {
    color: var(--error-text, #e88f8f);
  }

  .view-bar {
    display: flex;
    align-items: center;
    gap: 12px;
    border-bottom: 1px solid var(--panel-border);
    margin-bottom: 20px;
    min-width: 0;
  }
  .tabs-area {
    display: flex;
    align-items: center;
    min-width: 0;
    flex: 1;
  }
  .tabs {
    display: flex;
    gap: 20px;
    overflow-x: auto;
    min-width: 0;
    user-select: none;
  }
  .tabs:global([data-scrollable="true"]),
  .tabs:global([data-scrollable="true"]) [role="tab"] {
    cursor: grab;
  }
  .tabs:global([data-dragging="true"]),
  .tabs:global([data-dragging="true"]) :global(*) {
    cursor: grabbing;
  }
  .tab-name {
    user-select: text;
  }
  .tab-item {
    display: flex;
    align-items: center;
    gap: 4px;
    position: relative;
    flex-shrink: 0;
  }
  .tab-item.active::after {
    content: "";
    position: absolute;
    bottom: 0;
    left: 0;
    right: 0;
    height: 2px;
    border-radius: 2px;
    background: var(--accent);
  }
  [role="tab"] {
    min-height: 44px;
    padding: 0 2px 13px;
    border: 0;
    background: transparent;
    color: var(--muted);
    font-size: 13px;
    white-space: nowrap;
    max-width: 220px;
    overflow: hidden;
    text-overflow: ellipsis;
  }
  [role="tab"][aria-selected="true"],
  [role="tab"]:hover {
    color: var(--text);
  }
  .icon-button {
    display: grid;
    place-items: center;
    width: 28px;
    height: 32px;
    flex-shrink: 0;
    padding: 0;
    border: 0;
    border-radius: 6px;
    background: transparent;
    color: var(--muted);
  }
  .icon-button:hover:not(:disabled) {
    color: var(--text);
    background: var(--hover-surface);
  }
  .save {
    margin-bottom: 13px;
  }
  .new-tab {
    margin-left: 8px;
    margin-bottom: 13px;
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
  .view-options {
    display: flex;
    align-items: center;
    gap: 4px;
    padding-bottom: 8px;
    flex-shrink: 0;
  }
  .saving-dot {
    width: 8px;
    height: 8px;
    border-radius: 50%;
    background: var(--accent);
    animation: pulse 900ms infinite alternate;
  }
  @keyframes pulse {
    to {
      opacity: 0.3;
    }
  }
  @media (prefers-reduced-motion: reduce) {
    .saving-dot {
      animation: none;
    }
  }
  @container tasks-list (max-width:620px) {
    .view-bar {
      gap: 6px;
    }
  }
</style>
