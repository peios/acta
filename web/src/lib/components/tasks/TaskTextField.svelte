<script lang="ts">
  import { beforeNavigate } from "$app/navigation";
  import { onMount } from "svelte";
  import { api, APIError, errorMessage } from "$lib/api";
  import { useAccount } from "$lib/account-context";
  import { taskChanged, type Task } from "$lib/tasks";
  import TaskDescription from "./TaskDescription.svelte";
  let {
    task,
    field,
    editable,
    onsaved,
    compact = false,
  }: {
    task: Task;
    field: "title" | "description";
    editable: boolean;
    onsaved: (t: Task) => void;
    compact?: boolean;
  } = $props();
  const account = useAccount();
  let draft = $state(""),
    base = $state(""),
    version = $state(0),
    ready = $state(false),
    saving = $state(false),
    error = $state(""),
    conflict = $state<Task | null>(null),
    persisted = $state(true);
  let timer: ReturnType<typeof setTimeout> | undefined;
  let editing = $state(false);
  let region: HTMLDivElement;
  let finishRequested = $state(false);
  function finishEditing() {
    if (!editing) return;
    finishRequested = true;
    clearTimeout(timer);
    void save();
  }
  let key = "";
  let writer = "";
  let disposed = false;
  const dirty = $derived(draft !== base);
  $effect(() => {
    if (finishRequested && !saving && !dirty && !error && !conflict) {
      editing = false;
      finishRequested = false;
    }
  });
  function retain() {
    try {
      if (
        disposed &&
        JSON.parse(localStorage.getItem(key) || "null")?.writer !== writer
      )
        return;
      if (draft !== base)
        localStorage.setItem(
          key,
          JSON.stringify({ value: draft, base, version, writer }),
        );
      else localStorage.removeItem(key);
      persisted = true;
    } catch {
      persisted = false;
    }
  }
  function input(v: string) {
    draft = v;
    error = "";
    retain();
    clearTimeout(timer);
    if (!conflict) timer = setTimeout(() => void save(), 700);
  }
  async function save() {
    if (!dirty || saving || !editable || conflict || disposed) return;
    saving = true;
    const submitted = draft;
    try {
      const t = await api<Task>("tasks/" + task.id, {
        field,
        version,
        value: submitted,
      });
      base = t[field];
      version = t.versions[field];
      if (draft === submitted) draft = t[field];
      error = "";
      retain();
      if (!disposed) onsaved(t);
      taskChanged();
    } catch (e) {
      error = errorMessage(e);
      if (e instanceof APIError && e.status === 409) {
        try {
          conflict = await api<Task>("tasks/" + task.id);
        } catch {}
      }
    } finally {
      saving = false;
      if (dirty && !error && !conflict && !disposed)
        timer = setTimeout(() => void save(), 700);
    }
  }
  function resolve(mine: boolean) {
    if (!conflict) return;
    base = conflict[field];
    version = conflict.versions[field];
    if (!mine) draft = base;
    conflict = null;
    error = "";
    retain();
    if (mine) void save();
  }
  beforeNavigate((navigation) => {
    retain();
    if (
      dirty &&
      !persisted &&
      !window.confirm(
        "This draft could not be saved or retained. Leave and discard it?",
      )
    )
      navigation.cancel();
  });
  onMount(() => {
    let tab = "shared";
    try {
      tab = sessionStorage.getItem("acta.draft-tab") || crypto.randomUUID();
      sessionStorage.setItem("acta.draft-tab", tab);
    } catch {}
    writer = crypto.randomUUID();
    key = `acta.draft.${account.account.id}.${task.id}.${field}.${tab}`;
    draft = base = task[field];
    version = task.versions[field];
    try {
      const raw = localStorage.getItem(key);
      if (raw) {
        const saved = JSON.parse(raw);
        if (
          typeof saved.value === "string" &&
          typeof saved.base === "string" &&
          typeof saved.version === "number"
        ) {
          draft = saved.value;
          if (saved.base !== base && draft !== base) conflict = task;
          else if (draft !== base) timer = setTimeout(() => void save(), 700);
        }
      }
    } catch {
      persisted = false;
    }
    retain();
    ready = true;
    if (field === "description" && (dirty || conflict)) editing = true;
    const unload = (e: BeforeUnloadEvent) => {
      retain();
      if (dirty && !persisted) {
        e.preventDefault();
        e.returnValue = "";
      }
    };
    window.addEventListener("beforeunload", unload);
    return () => {
      retain();
      disposed = true;
      clearTimeout(timer);
      window.removeEventListener("beforeunload", unload);
    };
  });
  $effect(() => {
    if (ready && task.versions[field] !== version && !saving) {
      if (!dirty) {
        draft = base = task[field];
        version = task.versions[field];
      } else if (task[field] !== base) {
        conflict = task;
        clearTimeout(timer);
      }
    }
  });
</script>

<svelte:window
  onclick={(event) => {
    // Finish after the destination receives its click. Collapsing on pointerdown
    // moves controls under a finger before pointerup and can swallow the tap.
    if (!editing) return;
    if (event.composedPath().includes(region)) finishRequested = false;
    else finishEditing();
  }}
/>

<div
  bind:this={region}
  class="task-text-field"
  class:compact
  class:title-field={field === "title"}
  class:description-field={field === "description"}
>
  <div class="field-heading">
    {#if field === "title"}<label for={`${task.id}-${field}`}>Title</label>
    {:else}<span class="description-label"
        ><svg
          viewBox="0 0 24 24"
          width="17"
          height="17"
          fill="none"
          stroke="currentColor"
          stroke-width="1.5"
          stroke-linecap="round"
          stroke-linejoin="round"
          aria-hidden="true"
          ><path
            d="M14 3H6a2 2 0 0 0-2 2v14a2 2 0 0 0 2 2h12a2 2 0 0 0 2-2V9Z"
          /><path d="M14 3v6h6M8 13h8M8 17h5" /></svg
        >Description</span
      >{/if}
    <div class="field-actions">
      {#if field === "title" || editing || dirty || error || conflict}<span
          role="status"
          class:quiet={!saving && !conflict && !error && !dirty}
          >{saving
            ? "Saving…"
            : conflict
              ? "Needs review"
              : error
                ? "Couldn’t save"
                : dirty
                  ? "Unsaved"
                  : "Saved"}</span
        >{/if}
      {#if field === "description" && editable && !editing}
        <button
          class="edit-description"
          aria-label="Edit description"
          disabled={saving || !ready}
          onclick={() => (editing = true)}
        >
          <svg
            viewBox="0 0 20 20"
            width="14"
            height="14"
            fill="none"
            stroke="currentColor"
            stroke-width="1.4"
            stroke-linecap="round"
            stroke-linejoin="round"
            aria-hidden="true"
            ><path d="m12 4 4 4M3 17l4-1L17 6a2.8 2.8 0 0 0-4-4L3 12Z" /></svg
          >
          Edit
        </button>
      {/if}
    </div>
  </div>
  {#if ready}{#if field === "title"}<textarea
        id={`${task.id}-${field}`}
        aria-label={compact ? `Title of ${task.reference}` : undefined}
        value={draft}
        disabled={!editable}
        oninput={(e) => input(e.currentTarget.value)}
        onblur={() => void save()}
        maxlength="300"
        rows="1"
        class="title-input"></textarea>{:else}<TaskDescription
        value={draft}
        editing={editing && editable}
        onchange={input}
      />{/if}{/if}
  {#if error && !conflict}<p class="notice error" role="alert">
      {error}
      <button
        class="secondary"
        onclick={() => void save()}
        disabled={saving || !editable}>Retry</button
      >
    </p>{/if}
  {#if conflict}<div class="notice" role="alert">
      <strong>This {field} changed elsewhere.</strong>
      <p>Your draft is safe. Review the saved version before choosing.</p>
      <details>
        <summary>Show saved version</summary>
        <pre>{conflict[field]}</pre>
      </details>
      <div class="actions">
        <button class="secondary" onclick={() => resolve(false)}
          >Use saved version</button
        ><button
          class="secondary"
          disabled={!editable}
          onclick={() => resolve(true)}>Save my version</button
        >
      </div>
    </div>{/if}
  {#if dirty && !persisted}<p class="notice error" role="alert">
      This browser couldn’t retain your draft. Keep this task open until it
      saves.
    </p>{/if}
</div>

<style>
  .field-heading {
    display: flex;
    justify-content: space-between;
    margin-bottom: 10px;
    gap: 12px;
    align-items: center;
  }
  .field-heading label {
    font-weight: 600;
    font-size: 13px;
  }
  .field-actions {
    display: flex;
    align-items: center;
    gap: 10px;
  }
  .field-actions span {
    color: var(--muted);
    font-size: 12px;
  }
  .title-input {
    display: block;
    font: inherit;
    font-size: 28px;
    line-height: 1.35;
    letter-spacing: -0.7px;
    font-weight: 600;
    width: 100%;
    field-sizing: content;
    resize: none;
    min-height: 1.35em;
    padding: 0;
    border: 0;
    border-radius: 4px;
    color: var(--text);
    background: transparent;
  }
  .title-input:focus {
    outline: 0;
    box-shadow: 0 2px 0 var(--panel-border);
  }
  .title-input:disabled {
    opacity: 1;
  }
  .title-field {
    position: relative;
    margin: 28px 0 32px;
  }
  .title-field .field-heading {
    position: absolute;
    right: 0;
    bottom: -22px;
    margin: 0;
  }
  .title-field label {
    position: absolute;
    width: 1px;
    height: 1px;
    overflow: hidden;
    clip-path: inset(50%);
    white-space: nowrap;
  }
  .quiet {
    opacity: 0;
  }
  .task-text-field:focus-within .quiet {
    opacity: 1;
  }
  .description-field {
    background: color-mix(in srgb, var(--page) 45%, var(--surface));
    padding: 14px var(--task-description-inset, 16px);
    border-radius: 14px;
  }
  .description-label {
    display: inline-flex;
    align-items: center;
    gap: 10px;
    font-size: 15px;
    font-weight: 600;
    letter-spacing: -0.15px;
  }
  .description-label svg {
    color: var(--muted);
    flex-shrink: 0;
  }
  .edit-description {
    display: inline-flex;
    align-items: center;
    gap: 6px;
    padding: 5px 7px;
    border: 0;
    border-radius: 6px;
    background: transparent;
    color: var(--muted);
    font-size: 12px;
  }
  .edit-description:hover {
    color: var(--text);
    background: var(--hover-surface);
  }
  .task-text-field {
    margin-bottom: 24px;
  }
  .task-text-field.compact {
    margin: 0;
    min-width: 0;
  }
  .compact .title-input {
    font-size: 13px;
    font-weight: 400;
    letter-spacing: 0;
    line-height: 1.5;
    padding: 6px 4px;
  }
  .compact .title-input:hover:not(:disabled) {
    background: var(--hover-surface);
  }
  .compact .field-heading {
    bottom: -12px;
  }
  .compact .field-actions span {
    font-size: 10px;
  }
  .compact .quiet {
    visibility: hidden;
  }
  .actions {
    display: flex;
    gap: 8px;
    flex-wrap: wrap;
    margin-top: 12px;
  }
  pre {
    white-space: pre-wrap;
    max-height: 200px;
    overflow: auto;
  }
</style>
