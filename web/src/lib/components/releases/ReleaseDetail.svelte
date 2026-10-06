<script lang="ts">
  import { onMount, tick } from "svelte";
  import { goto } from "$app/navigation";
  import { api, APIError, errorMessage } from "$lib/api";
  import { canWorkspace, workspacePath, type Workspace } from "$lib/workspaces";
  import type { Release } from "$lib/releases";
  import { defaultViewDisplay, emptyViewFilters } from "$lib/task-views.js";
  import type { Task, TaskConfig } from "$lib/tasks";
  import TaskTree from "../tasks/TaskTree.svelte";
  import MarkdownView from "../tasks/MarkdownView.svelte";
  import MarkdownEditor from "../tasks/MarkdownEditor.svelte";
  import ReleaseProgress from "./ReleaseProgress.svelte";
  import ReleaseStateBadge from "./ReleaseStateBadge.svelte";
  import ReleaseStatePicker from "./ReleaseStatePicker.svelte";
  import "$lib/components/management/management.css";
  let { workspace, releaseID }: { workspace: Workspace; releaseID: string } =
    $props();
  const id = $props.id();
  const canManage = $derived(canWorkspace(workspace, "tasks.releases.manage"));
  let release = $state<Release | null>(null),
    config = $state<TaskConfig | null>(null),
    loadError = $state(""),
    error = $state(""),
    busy = $state(false),
    refreshes = $state(0);
  let editingNotes = $state(false),
    notes = $state("");
  let dialog: HTMLDialogElement, nameInput: HTMLInputElement;
  let name = $state(""),
    codename = $state(""),
    renaming = $state(false);
  let controller: AbortController | undefined;
  async function load() {
    controller?.abort();
    const current = (controller = new AbortController());
    try {
      const [r, c] = await Promise.all([
        api<Release>(
          `workspaces/${workspace.id}/releases/${releaseID}`,
          undefined,
          { signal: current.signal },
        ),
        api<TaskConfig>(`workspaces/${workspace.id}/task-config`, undefined, {
          signal: current.signal,
        }),
      ]);
      release = r;
      config = c;
      loadError = "";
      refreshes += 1;
    } catch (e) {
      if (!current.signal.aborted) loadError = errorMessage(e);
    }
  }
  onMount(() => {
    void load();
    const refresh = () => void load();
    window.addEventListener("focus", refresh);
    window.addEventListener("acta:tasks-changed", refresh);
    return () => {
      controller?.abort();
      window.removeEventListener("focus", refresh);
      window.removeEventListener("acta:tasks-changed", refresh);
    };
  });
  /** Saves the supplied fields; a stale version reloads the latest release. */
  async function save(fields: Partial<Release>) {
    if (!release || busy) return false;
    busy = true;
    error = "";
    try {
      release = await api<Release>(
        `workspaces/${workspace.id}/releases/${release.id}`,
        { ...fields, version: release.version },
      );
      return true;
    } catch (e) {
      error = errorMessage(e);
      if (e instanceof APIError && e.status === 409) {
        error += " The latest version is shown; review it and try again.";
        await load();
      }
      return false;
    } finally {
      busy = false;
    }
  }
  async function openRename() {
    if (!release) return;
    name = release.name;
    codename = release.codename;
    error = "";
    renaming = true;
    dialog.showModal();
    await tick();
    nameInput.select();
  }
  function editNotes() {
    notes = release?.description ?? "";
    error = "";
    editingNotes = true;
  }
  function openTask(task: Task) {
    void goto(
      `${workspacePath(workspace)}?board=${task.board ?? "tasks"}&task=${encodeURIComponent(task.id)}`,
    );
  }
  // Release tasks span both boards and every depth.
  const listConfig = $derived(config ? { ...config, board: "*" } : null);
  let display = $state({
    ...defaultViewDisplay(),
    columns: ["status", "assignees"],
  });
  // Same header behaviour as the task list: a new field sorts ascending first.
  function sortBy(field: string) {
    display = {
      ...display,
      sort: field,
      direction:
        display.sort === field && display.direction === "asc" ? "desc" : "asc",
    };
  }
</script>

<div class="management-page release-page">
  <a class="management-back" href={`${workspacePath(workspace)}/releases`}
    >← All releases</a
  >
  {#if loadError}<p class="notice error" role="alert">
      {loadError}
      <button class="secondary" onclick={() => void load()}>Retry</button>
    </p>{/if}
  {#if release}
    <div class="management-heading">
      <div>
        <h2>
          {release.name}{#if release.codename}<span class="codename"
              >{release.codename}</span
            >{/if}
        </h2>
        <ReleaseProgress total={release.total} finished={release.finished} />
      </div>
      {#if canManage}<button
          class="secondary"
          disabled={busy}
          onclick={() => void openRename()}>Rename</button
        >{:else}<ReleaseStateBadge state={release.state} />{/if}
    </div>
    {#if canManage}<ReleaseStatePicker
        value={release.state}
        disabled={busy}
        onchange={(state) => void save({ state })}
      />{/if}
    {#if error && !renaming}<p class="notice error" role="alert">
        {error}
      </p>{/if}
    <section class="management-section">
      <div class="section-heading">
        <h3>Notes</h3>
        {#if canManage && !editingNotes}<button
            class="secondary"
            disabled={busy}
            onclick={editNotes}>Edit notes</button
          >{/if}
      </div>
      {#if editingNotes}<MarkdownEditor
          value={notes}
          label="Release notes"
          disabled={busy}
          onchange={(v) => (notes = v)}
        />
        <div class="management-actions">
          <button
            class="secondary"
            disabled={busy}
            onclick={() => (editingNotes = false)}>Cancel</button
          ><button
            class="primary"
            disabled={busy}
            onclick={async () => {
              if (await save({ description: notes })) editingNotes = false;
            }}>Save notes</button
          >
        </div>
      {:else if release.description.trim()}<MarkdownView
          value={release.description}
        />{:else}<p class="management-note">No notes yet.</p>{/if}
    </section>
    <section class="management-section">
      <h3>Tasks</h3>
      <p class="management-note">
        Tasks that target this release directly, at any depth and on either
        board.
      </p>
      {#if listConfig}<TaskTree
          workspace={workspace.id}
          config={listConfig}
          completion="all"
          filters={{ ...emptyViewFilters(), releases: [release.id] }}
          allDepths
          {display}
          revision={refreshes}
          onsort={sortBy}
          onopen={openTask}
        />{/if}
    </section>
  {:else if !loadError}<p class="hint" role="status">Loading release…</p>{/if}
</div>

<dialog
  bind:this={dialog}
  class="management-dialog"
  aria-labelledby={`${id}-title`}
  onclose={() => (renaming = false)}
>
  <h2 id={`${id}-title`}>Rename release</h2>
  <form
    onsubmit={async (e) => {
      e.preventDefault();
      if (await save({ name, codename })) dialog.close();
    }}
  >
    <div class="field">
      <label for={`${id}-name`}>Name</label><input
        id={`${id}-name`}
        bind:this={nameInput}
        bind:value={name}
        maxlength="60"
        required
        disabled={busy}
      />
    </div>
    <div class="field">
      <label for={`${id}-codename`}>Codename (optional)</label><input
        id={`${id}-codename`}
        bind:value={codename}
        maxlength="60"
        disabled={busy}
      />
    </div>
    {#if error && renaming}<p class="notice error" role="alert">{error}</p>{/if}
    <div class="management-actions">
      <button
        type="button"
        class="secondary"
        disabled={busy}
        onclick={() => dialog.close()}>Cancel</button
      ><button class="primary" disabled={busy || !name.trim()}>Save</button>
    </div>
  </form>
</dialog>

<style>
  .release-page {
    max-width: 960px;
  }
  .codename {
    margin-left: 10px;
    font-weight: 400;
    color: var(--muted);
  }
  .management-heading h2 {
    margin-bottom: 10px;
  }
  .section-heading {
    display: flex;
    justify-content: space-between;
    align-items: center;
    gap: 12px;
    margin-bottom: 12px;
  }
  .section-heading h3 {
    margin: 0;
  }
</style>
