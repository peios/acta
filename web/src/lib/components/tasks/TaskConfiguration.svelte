<script lang="ts">
  import { beforeNavigate } from "$app/navigation";
  import { onMount } from "svelte";
  import { api, errorMessage } from "$lib/api";
  import { canWorkspace, type Workspace } from "$lib/workspaces";
  import {
    boardConfig,
    boardStatuses,
    taskChanged,
    type TaskConfig,
    type TaskStatus,
  } from "$lib/tasks";
  let { workspace }: { workspace: Workspace } = $props();
  let board = $state("tasks");
  let fullConfig = $state<TaskConfig | null>(null);
  let config = $state<TaskConfig | null>(null),
    prefix = $state(""),
    statuses = $state<TaskStatus[]>([]),
    creation = $state(""),
    completed = $state(""),
    removed = $state<TaskStatus[]>([]),
    replacements = $state<Record<string, string>>({}),
    error = $state(""),
    busy = $state(false),
    saved = $state("");
  const canEdit = $derived(canWorkspace(workspace, "workspace.edit")),
    canStatuses = $derived(canWorkspace(workspace, "tasks.statuses.manage"));
  const dirty = $derived(
    config &&
      (prefix !== config.prefix ||
        JSON.stringify(statuses) !== JSON.stringify(config.statuses) ||
        creation !== config.creation_status ||
        completed !== config.completed_status),
  );
  beforeNavigate((n) => {
    if (dirty && !window.confirm("Discard unsaved task configuration changes?"))
      n.cancel();
  });
  async function load(part: "all" | "prefix" | "statuses" = "all") {
    try {
      fullConfig = await api<TaskConfig>(
        `workspaces/${workspace.id}/task-config`,
      );
      const scoped = boardConfig(fullConfig, board);
      config = { ...scoped, statuses: boardStatuses(scoped) };
      if (part !== "statuses") prefix = config.prefix;
      if (part !== "prefix") {
        statuses = config.statuses.map((s) => ({ ...s }));
        creation = config.creation_status;
        completed = config.completed_status;
        removed = [];
        replacements = {};
      }
    } catch (e) {
      error = errorMessage(e);
    }
  }
  async function save(part: "prefix" | "statuses") {
    if (!config) return;
    busy = true;
    error = "";
    saved = "";
    try {
      await api(
        `workspaces/${workspace.id}/task-${part}`,
        part === "prefix"
          ? { prefix, version: config.version }
          : {
              board,
              statuses,
              creation_status: creation,
              completed_status: completed,
              replacements,
              version: config.version,
            },
      );
      await load(part);
      saved = "Saved";
      taskChanged();
    } catch (e) {
      error = errorMessage(e);
    } finally {
      busy = false;
    }
  }
  onMount(() => void load());
</script>

<section class="config">
  <h2>Task prefix</h2>
  <p class="hint">
    Used in task references, such as ACT-151. Previous prefixes remain reserved.
  </p>
  {#if config}<form
      onsubmit={(e) => {
        e.preventDefault();
        void save("prefix");
      }}
    >
      <div class="field">
        <label for="task-prefix">Prefix</label><input
          id="task-prefix"
          bind:value={prefix}
          disabled={!canEdit || busy}
          maxlength="10"
        />
      </div>
      {#if canEdit}<button
          class="secondary"
          disabled={busy || prefix === config.prefix}>Save prefix</button
        >{/if}
    </form>
    {#if config.previous_prefixes.length}<details>
        <summary>Previous prefixes</summary>
        <p>{config.previous_prefixes.join(", ")}</p>
      </details>{/if}{/if}
</section>
<section class="config">
  <h2>Board statuses</h2>
  <div class="board-switch" aria-label="Board workflow">
    {#each ["tasks", "backlog"] as slug}<button
        class="secondary"
        aria-pressed={board === slug}
        disabled={busy}
        onclick={() => {
          if (
            dirty &&
            !window.confirm("Discard unsaved configuration changes?")
          )
            return;
          board = slug;
          void load();
        }}>{slug === "tasks" ? "Tasks" : "Backlog"}</button
      >{/each}
  </div>
  <p class="hint">
    Choose the workflow for this board. Completing a parent doesn’t change its
    children’s statuses.
  </p>
  {#if config}<form
      onsubmit={(e) => {
        e.preventDefault();
        void save("statuses");
      }}
    >
      <div class="status-list">
        {#each statuses as status, i (status.id)}<div class="status-row">
            <input
              aria-label={`Status ${i + 1} name`}
              bind:value={status.name}
              disabled={!canStatuses || busy}
              maxlength="60"
            />{#if canStatuses}<button
                type="button"
                class="secondary"
                disabled={busy ||
                  statuses.length <= (board === "backlog" ? 1 : 2)}
                onclick={() => {
                  removed = [...removed, status];
                  statuses = statuses.filter((s) => s.id !== status.id);
                }}>Remove</button
              >{/if}
          </div>{/each}
      </div>
      {#if canStatuses}<button
          type="button"
          class="secondary"
          disabled={busy || statuses.length >= 50}
          onclick={() =>
            (statuses = [
              ...statuses,
              { id: crypto.randomUUID(), name: "", board },
            ])}>Add status</button
        >{/if}
      <div class="selections">
        <div class="field">
          <label for="creation-status">Creation Status</label><select
            id="creation-status"
            bind:value={creation}
            disabled={!canStatuses || busy}
            >{#each statuses as s}<option value={s.id}
                >{s.name || "Unnamed status"}</option
              >{/each}</select
          >
        </div>
        <div class="field">
          <label for="completed-status">Completed Status</label><select
            id="completed-status"
            bind:value={completed}
            disabled={!canStatuses || busy}
            >{#if board === "backlog"}<option value=""
                >No completed status</option
              >{/if}{#each statuses as s}<option value={s.id}
                >{s.name || "Unnamed status"}</option
              >{/each}</select
          >
        </div>
      </div>
      {#each removed as s}<div class="field">
          <label for={`replace-${s.id}`}>Move tasks from {s.name} to</label
          ><select
            id={`replace-${s.id}`}
            bind:value={replacements[s.id]}
            required
            ><option value="">Choose a replacement</option
            >{#each statuses as next}<option value={next.id}>{next.name}</option
              >{/each}</select
          >
        </div>{/each}
      {#if canStatuses}<button class="primary" disabled={busy}
          >Save statuses</button
        >{/if}
    </form>{/if}
</section>
{#if error}<p class="notice error" role="alert">{error}</p>
  <button
    class="secondary"
    onclick={() => {
      error = "";
      void load();
    }}>Reload configuration</button
  >{/if}{#if saved}<p class="hint" role="status">{saved}</p>{/if}

<style>
  .board-switch {
    display: flex;
    gap: 8px;
    margin: 12px 0;
  }
  .board-switch [aria-pressed="true"] {
    background: var(--hover-surface);
    color: var(--text);
    border-color: var(--accent);
  }
  .config {
    border-top: 1px solid var(--border);
    padding-top: 28px;
    margin-top: 36px;
  }
  .config h2 {
    font-size: 18px;
  }
  .config form {
    display: grid;
    gap: 16px;
    justify-items: start;
  }
  .status-list {
    display: grid;
    gap: 10px;
    width: 100%;
  }
  .status-row {
    display: flex;
    gap: 8px;
  }
  .status-row input {
    min-width: 0;
    flex: 1;
  }
  .selections {
    display: flex;
    flex-wrap: wrap;
    gap: 16px;
    width: 100%;
  }
  .selections .field {
    flex: 1;
    min-width: 180px;
  }
  details {
    margin-top: 16px;
  }
</style>
