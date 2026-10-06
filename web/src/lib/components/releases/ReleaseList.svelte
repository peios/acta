<script lang="ts">
  import { onMount, tick } from "svelte";
  import { goto } from "$app/navigation";
  import { api, errorMessage } from "$lib/api";
  import { canWorkspace, workspacePath, type Workspace } from "$lib/workspaces";
  import { loadReleases, type Release, type ReleaseState } from "$lib/releases";
  import ReleaseProgress from "./ReleaseProgress.svelte";
  import ReleaseStateBadge from "./ReleaseStateBadge.svelte";
  import ReleaseStatePicker from "./ReleaseStatePicker.svelte";
  import "$lib/components/management/management.css";
  let { workspace }: { workspace: Workspace } = $props();
  const id = $props.id();
  const canManage = $derived(canWorkspace(workspace, "tasks.releases.manage"));
  let releases = $state<Release[]>([]),
    loading = $state(true),
    error = $state("");
  let dialog: HTMLDialogElement, nameInput: HTMLInputElement;
  let name = $state(""),
    codename = $state(""),
    initial = $state<ReleaseState>("planned"),
    creating = $state(false),
    createError = $state("");
  let controller: AbortController | undefined;
  async function load() {
    controller?.abort();
    const current = (controller = new AbortController());
    try {
      releases = await loadReleases(workspace.id, current.signal);
      error = "";
    } catch (e) {
      if (!current.signal.aborted) error = errorMessage(e);
    } finally {
      if (!current.signal.aborted) loading = false;
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
  async function openCreate() {
    name = "";
    codename = "";
    initial = "planned";
    createError = "";
    dialog.showModal();
    await tick();
    nameInput.focus();
  }
  async function create() {
    if (creating) return;
    creating = true;
    createError = "";
    try {
      const r = await api<Release>(`workspaces/${workspace.id}/releases`, {
        name,
        codename,
        state: initial,
      });
      dialog.close();
      await goto(`${workspacePath(workspace)}/releases/${r.id}`);
    } catch (e) {
      createError = errorMessage(e);
    } finally {
      creating = false;
    }
  }
</script>

<div class="management-page">
  <div class="management-heading">
    <div>
      <h2>Releases</h2>
      <p class="management-note">
        Sequence work into named cuts. Each release counts the tasks that target
        it directly, at any depth.
      </p>
    </div>
    {#if canManage}<button class="primary" onclick={() => void openCreate()}
        >New release</button
      >{/if}
  </div>
  {#if error}<p class="notice error" role="alert">
      {error}
      <button class="secondary" onclick={() => void load()}>Retry</button>
    </p>{/if}
  {#if loading}<p class="hint" role="status">Loading releases…</p>
  {:else if !releases.length && !error}<p class="management-note">
      No releases yet.{canManage
        ? " Create one, then target tasks at it from their Release field."
        : ""}
    </p>
  {:else}<ul class="management-list">
      {#each releases as r (r.id)}<li class:shipped={r.state === "released"}>
          <a href={`${workspacePath(workspace)}/releases/${r.id}`}>
            <span class="identity"
              ><strong
                >{r.name}{#if r.codename}<span class="codename"
                    >{r.codename}</span
                  >{/if}</strong
              ><ReleaseProgress total={r.total} finished={r.finished} /></span
            ><ReleaseStateBadge state={r.state} />
          </a>
        </li>{/each}
    </ul>{/if}
</div>

<dialog
  bind:this={dialog}
  class="management-dialog"
  aria-labelledby={`${id}-title`}
>
  <h2 id={`${id}-title`}>New release</h2>
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
        maxlength="60"
        placeholder="2026.9"
        required
        disabled={creating}
      />
    </div>
    <div class="field">
      <label for={`${id}-codename`}>Codename (optional)</label><input
        id={`${id}-codename`}
        bind:value={codename}
        maxlength="60"
        disabled={creating}
      />
    </div>
    <div class="field">
      <span class="state-label">State</span>
      <ReleaseStatePicker
        value={initial}
        disabled={creating}
        onchange={(v) => (initial = v)}
      />
    </div>
    {#if createError}<p class="notice error" role="alert">
        {createError}
      </p>{/if}
    <div class="management-actions">
      <button
        type="button"
        class="secondary"
        disabled={creating}
        onclick={() => dialog.close()}>Cancel</button
      ><button class="primary" disabled={creating || !name.trim()}
        >Create release</button
      >
    </div>
  </form>
</dialog>

<style>
  .codename {
    margin-left: 8px;
    font-weight: 400;
    color: var(--muted);
  }
  .shipped .identity {
    opacity: 0.7;
  }
  .state-label {
    font-size: 14px;
    font-weight: 600;
  }
  form .field + .field {
    margin-top: 16px;
  }
</style>
