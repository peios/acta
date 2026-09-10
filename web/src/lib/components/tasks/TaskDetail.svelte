<script lang="ts">
  import { taskProperties, propertyOptions } from "$lib/task-properties";
  import OptionPicker from "../OptionPicker.svelte";
  import { tick, onMount } from "svelte";
  import TaskActivity from "./TaskActivity.svelte";
  import TaskDocuments from "./TaskDocuments.svelte";
  const detailTabs = ["activity", "subtasks", "documents"];
  import type { ActivityOrder } from "$lib/activity";
  import { api, errorMessage } from "$lib/api";
  import { canWorkspace, type Workspace } from "$lib/workspaces";
  import {
    taskChanged,
    personName,
    type Task,
    type TaskConfig,
  } from "$lib/tasks";
  import TaskTextField from "./TaskTextField.svelte";
  import TaskTree from "./TaskTree.svelte";
  import TaskStatusPicker from "./TaskStatusPicker.svelte";
  import TaskAssigneePicker from "./TaskAssigneePicker.svelte";
  let {
    task,
    focusComment = "",
    config,
    workspace,
    onchange,
    onopen,
    oncreate,
  }: {
    task: Task;
    focusComment?: string;
    config: TaskConfig;
    workspace: Workspace;
    onchange: (t: Task) => void;
    onopen: (t: Task) => void;
    oncreate: (parent: string) => void;
  } = $props();
  let tab = $state("activity");
  let activity = $state<TaskActivity>();
  $effect(() => {
    if (focusComment) tab = "activity";
  });
  let unread = $state(false);
  let order = $state<ActivityOrder>("asc");
  onMount(() => {
    try {
      order =
        localStorage.getItem("acta.activity-order") === "desc" ? "desc" : "asc";
    } catch {}
  });
  async function selectTab(value: string) {
    tab = value;
    await tick();
    if (value === "activity") await activity?.jumpLatest();
  }
  async function changeOrder() {
    order = order === "asc" ? "desc" : "asc";
    try {
      localStorage.setItem("acta.activity-order", order);
    } catch {}
    await tick();
    await activity?.jumpLatest();
  }
  const detailID = $props.id();
  const indirect = $derived(
    task.descendant_assignees.filter(
      (p) => !task.assignees.some((a) => a.id === p.id),
    ),
  );
  const editable = $derived(
    !task.archived && canWorkspace(workspace, "tasks.edit"),
  );
  const created = $derived(
    new Intl.DateTimeFormat(undefined, {
      dateStyle: "medium",
      timeStyle: "short",
    }).format(new Date(task.created_at)),
  );
  let error = $state(""),
    busy = $state(false),
    parent = $state("");
  let assigneeInfo = $state<HTMLDetailsElement>();
  let assigneeLabel = $state<HTMLElement>();
  let showSubtaskAssignees = $state(false);
  let parentDialog: HTMLDialogElement;
  let parentInput: HTMLInputElement;
  export async function openReparent() {
    if (!editable || busy) return;
    parent = "";
    error = "";
    parentDialog.showModal();
    await tick();
    parentInput.focus();
  }
  async function reparent(value: string) {
    if (await patch("parent_id", value)) parentDialog.close();
  }
  async function patch(
    field: string,
    value: unknown,
    version = task.versions[field],
  ) {
    if (busy) return false;
    busy = true;
    error = "";
    try {
      const t = await api<Task>("tasks/" + task.id, { field, value, version });
      onchange(t);
      taskChanged();
      return true;
    } catch (e) {
      error = errorMessage(e);
      return false;
    } finally {
      busy = false;
    }
  }
</script>

<svelte:window
  onpointerdown={(event) => {
    if (showSubtaskAssignees && !assigneeInfo?.contains(event.target as Node))
      showSubtaskAssignees = false;
  }}
  onkeydown={(event) => {
    if (event.key === "Escape" && showSubtaskAssignees) {
      event.preventDefault();
      event.stopPropagation();
      showSubtaskAssignees = false;
      assigneeLabel?.focus();
    }
  }}
/>

<div class="task-detail">
  {#if task.ancestors.length}<nav class="ancestors" aria-label="Parent tasks">
      {#each task.ancestors as a}<button
          onclick={() => onopen({ ...task, id: a.id } as Task)}
          >{a.reference} · {a.title}</button
        ><span aria-hidden="true">/</span>{/each}
    </nav>{/if}
  <TaskTextField {task} field="title" {editable} onsaved={onchange} />
  <div class="metadata">
    <div class="property">
      <span class="property-label"
        ><svg viewBox="0 0 24 24" aria-hidden="true"
          ><circle cx="12" cy="12" r="8.5" /><path d="M12 7v5l3 2" /></svg
        >Created</span
      >
      <time datetime={task.created_at}>{created}</time>
    </div>
    <div class="property">
      <label class="property-label" for={`${detailID}-status`}
        ><svg viewBox="0 0 24 24" aria-hidden="true"
          ><circle cx="12" cy="12" r="3" /><path
            d="M12 2v3m0 14v3M2 12h3m14 0h3M5 5l2 2m10 10 2 2M5 19l2-2M17 7l2-2"
          /></svg
        >Status</label
      >
      <TaskStatusPicker
        triggerID={`${detailID}-status`}
        value={task.status_id}
        {config}
        disabled={!editable || busy}
        onchange={(id) => void patch("status_id", id)}
      />
    </div>
    <div class="property">
      <details
        class="assignee-info"
        bind:this={assigneeInfo}
        bind:open={showSubtaskAssignees}
      >
        <summary
          class="property-label"
          bind:this={assigneeLabel}
          aria-label="Assignees"
          ><svg viewBox="0 0 24 24" aria-hidden="true"
            ><circle cx="9" cy="8" r="3" /><path
              d="M3 20v-2a6 6 0 0 1 12 0v2M16 5a3 3 0 0 1 0 6m2 3a5 5 0 0 1 3 4v2"
            /></svg
          >Assignees<svg class="chevron" viewBox="0 0 24 24" aria-hidden="true"
            ><path d="m8 10 4 4 4-4" /></svg
          ></summary
        >
        <div class="assignee-dropdown">
          <h4>From subtasks</h4>
          {#each indirect as p}
            <div class="indirect-person">
              <strong>{personName(p)}{p.agent ? " · Agent" : ""}</strong>
              {#each p.sources as source}
                <button
                  onclick={() => {
                    showSubtaskAssignees = false;
                    onopen({ ...task, id: source.id } as Task);
                  }}
                >
                  <span>{source.reference}</span>{source.title}
                </button>
              {/each}
            </div>
          {:else}
            <p>No additional assignees from subtasks.</p>
          {/each}
        </div>
      </details>
      <div class="assigned">
        {#each task.assignees as p}<span
            class="assignee-chip"
            class:unavailable={!p.available}
            title={`@${p.username}${p.agent ? " · Agent" : ""}`}
            ><span class="avatar" aria-hidden="true"
              >{personName(p).slice(0, 1).toUpperCase()}</span
            >{personName(p)}{p.agent ? " · Agent" : ""}{!p.available
              ? " · Access removed"
              : ""}</span
          >{/each}{#if !task.assignees.length}<span class="hint"
            >Unassigned</span
          >{/if}{#if editable}<TaskAssigneePicker
            workspace={workspace.id}
            assignees={task.assignees}
            disabled={busy}
            onchange={(ids) => patch("assignees", ids)}
          />{/if}
      </div>
    </div>
  </div>
  <div class="metadata-properties">
    {#each taskProperties as property}<div class="metadata-property">
        <span class="metadata-label"
          ><svg viewBox="0 0 20 20" aria-hidden="true"
            ><path d={property.icon} /></svg
          >{property.label}</span
        ><OptionPicker
          label={property.label}
          value={task[property.value] || "none"}
          options={propertyOptions(property.value)}
          disabled={!editable || busy}
          onchange={(value) => void patch(property.value, value)}
        />
      </div>{/each}
  </div>
  {#if error}<p class="notice error" role="alert">
      {error} Reload the task before retrying if it changed elsewhere.
    </p>{/if}
  <TaskTextField {task} field="description" {editable} onsaved={onchange} />
  <div class="detail-tab-bar">
    <div role="tablist" aria-label="Task details">
      {#each detailTabs as name}
        <button
          role="tab"
          id={`${detailID}-${name}-tab`}
          aria-selected={tab === name}
          tabindex={tab === name ? 0 : -1}
          aria-controls={`${detailID}-${name}-panel`}
          onclick={() => void selectTab(name)}
          onkeydown={(e) => {
            if (["ArrowLeft", "ArrowRight", "Home", "End"].includes(e.key)) {
              e.preventDefault();
              const next =
                e.key === "Home"
                  ? detailTabs[0]
                  : e.key === "End"
                    ? detailTabs[detailTabs.length - 1]
                    : detailTabs[
                        (detailTabs.indexOf(tab) +
                          (e.key === "ArrowRight" ? 1 : -1) +
                          detailTabs.length) %
                          detailTabs.length
                      ];
              void selectTab(next);
              document.getElementById(`${detailID}-${next}-tab`)?.focus();
            }
          }}
        >
          {name === "activity"
            ? "Activity"
            : name === "subtasks"
              ? "Subtasks"
              : "Documents"}{#if name === "documents"}<span class="tab-count"
              >{task.document_count ?? 0}</span
            >{/if}{#if name === "activity" && unread}<span
              class="unread-dot"
              aria-label="Unread activity"
            ></span>{/if}
        </button>
      {/each}
    </div>
    {#if tab === "activity"}<button
        class="add-subtask"
        title={`Order: ${order === "asc" ? "Oldest first" : "Newest first"}. Click to reverse.`}
        aria-label={`Activity order: ${order === "asc" ? "oldest first" : "newest first"}`}
        onclick={() => void changeOrder()}
        ><svg
          width="18"
          height="18"
          viewBox="0 0 24 24"
          fill="none"
          stroke="currentColor"
          stroke-width="1.5"
          stroke-linecap="round"
          stroke-linejoin="round"
          aria-hidden="true"
          ><path
            d={order === "asc"
              ? "M8 4v16m-4-4 4 4 4-4M15 5h5m-5 5h4m-4 5h3"
              : "M8 20V4m-4 4 4-4 4 4M15 5h3m-3 5h4m-4 5h5"}
          /></svg
        ></button
      >
    {:else if tab === "subtasks" && !task.archived && canWorkspace(workspace, "tasks.create")}<button
        class="add-subtask"
        aria-label="Add subtask"
        title="Add subtask"
        onclick={() => oncreate(task.id)}
        ><svg
          viewBox="0 0 24 24"
          width="18"
          height="18"
          fill="none"
          stroke="currentColor"
          stroke-width="1.5"
          stroke-linecap="round"
          aria-hidden="true"><path d="M12 5v14M5 12h14" /></svg
        ></button
      >{/if}
  </div>
  <div
    class="detail-tab-panel"
    role="tabpanel"
    id={`${detailID}-activity-panel`}
    aria-labelledby={`${detailID}-activity-tab`}
    hidden={tab !== "activity"}
    tabindex="0"
  >
    <TaskActivity
      canComment={!task.archived && canWorkspace(workspace, "tasks.comment")}
      bind:this={activity}
      {focusComment}
      task={task.id}
      revision={config.revision}
      active={tab === "activity"}
      {order}
      onunread={(value) => (unread = value)}
    />
  </div>
  <div
    class="detail-tab-panel"
    role="tabpanel"
    hidden={tab !== "subtasks"}
    id={`${detailID}-subtasks-panel`}
    aria-labelledby={`${detailID}-subtasks-tab`}
    tabindex="0"
  >
    {#if tab === "subtasks"}<TaskTree
        presentation="subtasks"
        archived={task.archived}
        canEdit={editable}
        workspace={workspace.id}
        {config}
        parent={task.id}
        completion="all"
        revision={config.revision}
        {onopen}
      />{/if}
  </div>
  <div
    class="detail-tab-panel"
    role="tabpanel"
    hidden={tab !== "documents"}
    id={`${detailID}-documents-panel`}
    aria-labelledby={`${detailID}-documents-tab`}
    tabindex="0"
  >
    {#if tab === "documents"}{#key task.id}<TaskDocuments
          revision={config.revision}
          task={task.id}
          {editable}
        />{/key}{/if}
  </div>
</div>

<dialog
  bind:this={parentDialog}
  class="management-dialog reparent-dialog"
  aria-label="Reparent task"
  oncancel={(event) => {
    if (busy) event.preventDefault();
  }}
>
  <h2>Reparent task</h2>
  <p class="hint">
    Move {task.reference} beneath another task in this workspace.
  </p>
  <div class="current-parent">
    <span>Current parent</span><strong
      >{task.parent_id
        ? `${task.ancestors.at(-1)?.reference} · ${task.ancestors.at(-1)?.title}`
        : "Top-level task"}</strong
    >
  </div>
  <form
    onsubmit={(event) => {
      event.preventDefault();
      void reparent(parent);
    }}
  >
    <div class="field">
      <label for="new-parent">New parent</label><input
        id="new-parent"
        bind:this={parentInput}
        bind:value={parent}
        placeholder="Task reference or UUID"
        disabled={busy}
        required
      />
    </div>
    <div class="metadata-properties">
      {#each taskProperties as property}<div class="metadata-property">
          <span class="metadata-label"
            ><svg viewBox="0 0 20 20" aria-hidden="true"
              ><path d={property.icon} /></svg
            >{property.label}</span
          ><OptionPicker
            label={property.label}
            value={task[property.value] || "none"}
            options={propertyOptions(property.value)}
            disabled={!editable || busy}
            onchange={(value) => void patch(property.value, value)}
          />
        </div>{/each}
    </div>
    {#if error}<p class="notice error" role="alert">{error}</p>{/if}
    <div class="management-actions">
      {#if task.parent_id}<button
          class="secondary"
          type="button"
          disabled={busy}
          onclick={() => void reparent("")}>Make top-level</button
        >{/if}
      <button
        class="secondary"
        type="button"
        disabled={busy}
        onclick={() => parentDialog.close()}>Cancel</button
      >
      <button class="primary" disabled={busy || !parent.trim()}
        >{busy ? "Moving…" : "Reparent"}</button
      >
    </div>
  </form>
</dialog>

<style>
  .metadata-properties {
    display: flex;
    flex-wrap: wrap;
    gap: 12px;
    margin: 4px 0 16px;
  }
  .metadata-property {
    flex: 1 1 100px;
    min-width: 0;
  }
  .metadata-label {
    display: flex;
    align-items: center;
    gap: 6px;
    color: var(--muted);
    font-size: 11px;
    margin: 0 0 4px 6px;
  }
  .metadata-label svg {
    width: 14px;
    height: 14px;
    fill: none;
    stroke: currentColor;
    stroke-width: 1.5;
    stroke-linecap: round;
    stroke-linejoin: round;
  }
  .reparent-dialog {
    max-width: 480px;
  }
  .current-parent {
    display: grid;
    gap: 6px;
    margin: 20px 0;
    padding: 12px 14px;
    border-radius: 10px;
    background: var(--hover-surface);
    font-size: 13px;
  }
  .current-parent span {
    color: var(--muted);
    font-size: 12px;
  }
  .current-parent strong {
    font-weight: 500;
    overflow-wrap: anywhere;
  }
  .task-detail {
    padding: 0 30px 30px;
  }
  .ancestors {
    display: flex;
    gap: 8px;
    align-items: center;
    flex-wrap: wrap;
    margin-top: 18px;
    color: var(--muted);
    font-size: 12px;
  }
  .ancestors button {
    border: 0;
    background: none;
    color: var(--muted);
    text-align: left;
    padding: 3px;
  }
  .metadata {
    display: grid;
    gap: 18px;
    margin: 0 0 28px;
    font-size: 13px;
  }
  .property {
    display: grid;
    grid-template-columns: minmax(110px, 36%) minmax(0, 1fr);
    align-items: center;
    gap: 16px;
    min-height: 30px;
  }
  .property-label {
    display: flex;
    align-items: center;
    gap: 12px;
    color: var(--muted);
    font-size: 13px;
    font-weight: 400;
  }
  .property-label svg {
    width: 17px;
    height: 17px;
    fill: none;
    stroke: currentColor;
    stroke-width: 1.5;
    stroke-linecap: round;
    flex-shrink: 0;
  }
  .assigned {
    display: flex;
    gap: 6px;
    align-items: center;
    flex-wrap: wrap;
    font-size: 13px;
  }
  .assignee-chip {
    display: inline-flex;
    align-items: center;
    gap: 7px;
    padding: 3px 9px 3px 3px;
    background: var(--hover-surface);
    border-radius: 20px;
    overflow-wrap: anywhere;
  }
  .avatar {
    display: inline-grid;
    place-items: center;
    width: 25px;
    height: 25px;
    flex-shrink: 0;
    border-radius: 50%;
    background: var(--scope-active);
    color: var(--accent);
    font-size: 11px;
    font-weight: 600;
  }
  .unavailable {
    opacity: 0.6;
  }
  .add-subtask {
    display: grid;
    place-items: center;
    width: 36px;
    height: 36px;
    padding: 0;
    border: 0;
    border-radius: 8px;
    background: transparent;
    color: var(--muted);
  }
  .add-subtask:hover {
    background: var(--hover-surface);
    color: var(--text);
  }
  .assignee-info {
    position: relative;
    min-width: 0;
  }
  .assignee-info summary {
    cursor: pointer;
    list-style: none;
    min-height: 32px;
    gap: 8px;
  }
  .assignee-info summary::-webkit-details-marker {
    display: none;
  }
  .assignee-info summary:hover,
  .assignee-info[open] summary {
    color: var(--text);
  }
  .assignee-info .chevron {
    width: 13px;
    height: 13px;
    transition: transform 150ms ease;
  }
  .assignee-info[open] .chevron {
    transform: rotate(180deg);
  }
  .assignee-dropdown {
    position: absolute;
    top: calc(100% + 6px);
    left: 0;
    z-index: 5;
    width: min(330px, calc(100vw - 80px));
    max-height: min(320px, 50svh);
    overflow: auto;
    padding: 16px;
    border: 1px solid var(--panel-border);
    border-radius: 12px;
    background: var(--surface);
    box-shadow: 0 12px 36px #0003;
  }
  .assignee-dropdown h4 {
    margin: 0 0 14px;
    color: var(--muted);
    font-size: 12px;
    font-weight: 500;
  }
  .assignee-dropdown p {
    margin: 0;
    color: var(--muted);
    line-height: 1.5;
  }
  .indirect-person + .indirect-person {
    margin-top: 14px;
  }
  .indirect-person strong {
    font-size: 13px;
    font-weight: 500;
  }
  .indirect-person button {
    display: block;
    width: 100%;
    text-align: left;
    border: 0;
    border-radius: 6px;
    background: transparent;
    color: var(--text);
    padding: 7px 4px;
    font-size: 12px;
    line-height: 1.5;
    overflow-wrap: anywhere;
  }
  .indirect-person button span {
    color: var(--muted);
    margin-right: 7px;
  }
  .indirect-person button:hover {
    background: var(--hover-surface);
  }
  @media (prefers-reduced-motion: reduce) {
    .assignee-info .chevron {
      transition: none;
    }
  }
  .detail-tab-bar {
    display: flex;
    align-items: center;
    justify-content: space-between;
    gap: 16px;
    border-bottom: 1px solid var(--panel-border);
    margin-top: 24px;
    margin-bottom: 16px;
  }
  .detail-tab-bar [role="tablist"] {
    display: flex;
    align-self: stretch;
    gap: 22px;
  }
  .detail-tab-bar [role="tab"] {
    position: relative;
    min-height: 44px;
    padding: 0 2px;
    border: 0;
    background: transparent;
    color: var(--text);
    font-size: 13px;
    font-weight: 500;
  }
  .detail-tab-bar [role="tab"][aria-selected="true"]::after {
    content: "";
    position: absolute;
    bottom: -1px;
    left: 0;
    right: 0;
    height: 2px;
    border-radius: 2px;
    background: var(--accent);
  }
  .detail-tab-bar [role="tab"][aria-selected="false"] {
    color: var(--muted);
  }
  .tab-count {
    display: inline-flex;
    align-items: center;
    justify-content: center;
    min-width: 18px;
    height: 18px;
    padding: 0 5px;
    margin-left: 6px;
    border-radius: 5px;
    background: var(--panel-hover, #ffffff0a);
    color: var(--muted);
    font-size: 11px;
    font-variant-numeric: tabular-nums;
    box-sizing: border-box;
  }
  .unread-dot {
    display: inline-block;
    width: 6px;
    height: 6px;
    border-radius: 50%;
    background: var(--accent);
    margin-left: 7px;
    vertical-align: middle;
  }
  .detail-tab-panel[hidden] {
    display: none;
  }
  .detail-tab-bar [role="tab"]:focus-visible,
  .detail-tab-panel:focus-visible {
    outline: 2px solid var(--accent);
    outline-offset: 4px;
    border-radius: 3px;
  }
  @media (max-width: 600px) {
    .task-detail {
      padding: 0 var(--task-content-inset, 16px) 24px;
    }
    .property {
      gap: var(--task-content-inset, 16px);
    }
  }
</style>
