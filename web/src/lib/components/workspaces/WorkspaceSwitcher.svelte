<script lang="ts">
  import { api, errorMessage } from "$lib/api";
  import { useAccount } from "$lib/account-context";
  import { checkPermission } from "$lib/permissions";
  import { useWorkspace, workspacePath, type Workspace } from "$lib/workspaces";
  import CreateWorkspaceDialog from "./CreateWorkspaceDialog.svelte";
  import "$lib/components/management/management.css";
  const current = useWorkspace(),
    account = useAccount(),
    id = $props.id();
  let dialog = $state<HTMLDialogElement>(),
    create = $state<CreateWorkspaceDialog>(),
    list = $state<Workspace[]>([]),
    query = $state(""),
    error = $state(""),
    loading = $state(false),
    more = $state(false),
    offset = $state(0);
  let generation = 0;
  async function load(reset = true) {
    const g = ++generation;
    loading = true;
    error = "";
    const start = reset ? 0 : offset;
    try {
      const r = await api<{ workspaces: Workspace[]; more: boolean }>(
        `workspaces?q=${encodeURIComponent(query)}&offset=${start}`,
      );
      if (g !== generation) return;
      list = reset ? r.workspaces : [...list, ...r.workspaces];
      offset = start + r.workspaces.length;
      more = r.more;
    } catch (e) {
      if (g === generation) error = errorMessage(e);
    } finally {
      if (g === generation) loading = false;
    }
  }
  function open() {
    query = "";
    list = [];
    dialog?.showModal();
    void load();
  }
</script>

<button
  class="switcher"
  aria-label="Switch workspace"
  title={current.workspace?.name ?? "Workspaces"}
  onclick={open}
  ><svg
    width="19"
    height="19"
    viewBox="0 0 24 24"
    fill="none"
    stroke="currentColor"
    stroke-width="1.6"
    aria-hidden="true"
    ><rect x="3" y="4" width="18" height="16" rx="3" /><path
      d="M8 4v16m8-16v16"
    /></svg
  ><span>{current.workspace?.name ?? "Workspaces"}</span><span
    aria-hidden="true">⌄</span
  ></button
>
<dialog bind:this={dialog} class="management-dialog" aria-labelledby={id}>
  <h2 {id}>Switch workspace</h2>
  <form
    class="management-search"
    onsubmit={(e) => {
      e.preventDefault();
      void load();
    }}
  >
    <div class="field">
      <label for={`${id}-search`}>Search workspaces</label><input
        id={`${id}-search`}
        bind:value={query}
      />
    </div>
    <button class="secondary" disabled={loading}>Search</button>
  </form>
  {#if error}<p class="notice error" role="alert">{error}</p>{/if}
  <ul class="management-list">
    {#each list as w}<li>
        <a
          href={workspacePath(w)}
          onclick={() => dialog?.close()}
          aria-current={current.workspace?.id === w.id ? "page" : undefined}
          ><span class="identity"
            ><strong>{w.name}</strong><span>{w.slug}</span></span
          >{#if current.workspace?.id === w.id}<span class="hint">Current</span
            >{/if}</a
        >
      </li>{/each}
  </ul>
  {#if loading}<p class="hint" role="status">
      Loading workspaces…
    </p>{:else if !list.length}<p class="management-note">
      No accessible workspaces found.
    </p>{/if}
  {#if more}<button
      class="secondary"
      disabled={loading}
      onclick={() => load(false)}>Load more</button
    >{/if}
  <div class="management-actions">
    {#if checkPermission(account.account, "site.workspaces.create") && !account.account.owner_id}<button
        class="primary"
        onclick={() => {
          dialog?.close();
          create?.open();
        }}>Create workspace</button
      >{/if}<button class="secondary" onclick={() => dialog?.close()}
      >Close</button
    >
  </div>
</dialog>
<CreateWorkspaceDialog bind:this={create} />

<style>
  .switcher {
    display: flex;
    align-items: center;
    gap: 9px;
    min-width: 0;
    flex: 1;
    background: transparent;
    border: 0;
    border-radius: 7px;
    color: var(--text);
    padding: 8px 4px;
    text-align: left;
    font-size: 14px;
    font-weight: 600;
  }
  .switcher:hover {
    background: var(--hover-surface);
  }
  .switcher svg {
    flex-shrink: 0;
  }
  .switcher span:first-of-type {
    flex: 1;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }
  .management-dialog {
    width: min(540px, calc(100vw - 32px));
  }
</style>
