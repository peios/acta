<script lang="ts">
  import { untrack } from "svelte";
  import { api, errorMessage } from "$lib/api";
  import {
    useWorkspace,
    workspaceChanged,
    canWorkspace,
    type Workspace,
    type WorkspaceMember,
    type WorkspaceGroup,
  } from "$lib/workspaces";
  import type { Permission } from "$lib/permissions";
  import PermissionsDialog from "$lib/components/management/PermissionsDialog.svelte";
  import "$lib/components/management/management.css";
  const current = useWorkspace(),
    id = $props.id();
  let tab = $state<"members" | "groups">("members"),
    query = $state(""),
    members = $state<WorkspaceMember[]>([]),
    groups = $state<WorkspaceGroup[]>([]),
    offset = $state(0),
    more = $state(false),
    error = $state(""),
    busy = $state(false),
    loading = $state(false),
    permissions = $state<PermissionsDialog>();
  let add = $state<HTMLDialogElement>(),
    remove = $state<HTMLDialogElement>(),
    target = $state<WorkspaceMember | null>(null),
    candidateQuery = $state(""),
    candidates = $state<WorkspaceMember[]>([]),
    candidateMore = $state(false),
    candidateOffset = $state(0),
    candidateError = $state(""),
    candidateLoading = $state(false);
  const canMembers = $derived(
      canWorkspace(current.workspace, "workspace.members.manage"),
    ),
    canPermissions = $derived(
      canWorkspace(current.workspace, "workspace.permissions.manage"),
    );
  let generation = 0,
    candidateGeneration = 0;
  async function load(reset = true) {
    const w = current.workspace;
    if (!w) return;
    const g = ++generation;
    loading = true;
    error = "";
    const start = reset ? 0 : offset;
    try {
      if (tab === "members") {
        const r = await api<{ members: WorkspaceMember[]; more: boolean }>(
          `workspaces/${w.id}/members?q=${encodeURIComponent(query)}&offset=${start}`,
        );
        if (g !== generation) return;
        members = reset ? r.members : [...members, ...r.members];
        more = r.more;
        offset = start + r.members.length;
      } else {
        const r = await api<{ groups: WorkspaceGroup[]; more: boolean }>(
          `workspaces/${w.id}/groups?q=${encodeURIComponent(query)}&offset=${start}`,
        );
        if (g !== generation) return;
        groups = reset ? r.groups : [...groups, ...r.groups];
        more = r.more;
        offset = start + r.groups.length;
      }
    } catch (e) {
      if (g === generation) error = errorMessage(e);
    } finally {
      if (g === generation) loading = false;
    }
  }
  $effect(() => {
    const version = current.workspace?.version;
    const allowed = canMembers || canPermissions;
    const mode = tab;
    if (version && allowed) untrack(() => void load());
    void mode;
  });
  $effect(() => {
    if (!canPermissions && tab === "groups") {
      tab = "members";
      query = "";
    }
  });
  async function refresh() {
    const w = current.workspace;
    if (!w) return;
    try {
      current.update(await api<Workspace>(`workspaces/${w.id}`));
    } finally {
      workspaceChanged();
    }
  }
  async function edit(value: WorkspaceMember | WorkspaceGroup) {
    const w = current.workspace;
    if (!w) return;
    error = "";
    try {
      const r = await api<{ permissions: Permission[] }>(
        "workspaces/permissions",
      );
      const member = "username" in value;
      permissions?.openScoped({
        label: member ? "@" + value.username : value.name,
        grants: value.direct_permissions,
        ceiling: w.permissions,
        groups: member ? value.groups : [],
        superuser: member ? value.superuser : false,
        catalogue: r.permissions,
        save: async (grants) => {
          await api(
            `workspaces/${w.id}/${member ? "members" : "groups"}/${value.id}/permissions`,
            { permissions: grants, version: w.version },
          );
          await refresh();
        },
      });
    } catch (e) {
      error = errorMessage(e);
    }
  }
  async function searchCandidates(reset = true) {
    const w = current.workspace;
    if (!w) return;
    const g = ++candidateGeneration;
    candidateLoading = true;
    candidateError = "";
    const start = reset ? 0 : candidateOffset;
    try {
      const r = await api<{ members: WorkspaceMember[]; more: boolean }>(
        `workspaces/${w.id}/members?candidates=true&q=${encodeURIComponent(candidateQuery)}&offset=${start}`,
      );
      if (g !== candidateGeneration) return;
      candidates = reset ? r.members : [...candidates, ...r.members];
      candidateMore = r.more;
      candidateOffset = start + r.members.length;
    } catch (e) {
      if (g === candidateGeneration) candidateError = errorMessage(e);
    } finally {
      if (g === candidateGeneration) candidateLoading = false;
    }
  }
  function openAdd() {
    candidateQuery = "";
    candidates = [];
    add?.showModal();
    void searchCandidates();
  }
  async function membership(value: WorkspaceMember, member: boolean) {
    const w = current.workspace;
    if (!w || busy) return;
    busy = true;
    error = "";
    candidateError = "";
    try {
      await api(`workspaces/${w.id}/members/${value.id}`, {
        member,
        version: w.version,
      });
      add?.close();
      remove?.close();
      await refresh();
    } catch (e) {
      if (member) candidateError = errorMessage(e);
      else {
        error = errorMessage(e);
        remove?.close();
      }
    } finally {
      busy = false;
    }
  }
</script>

{#if canMembers || canPermissions}<div class="management-page">
    <div class="management-heading">
      <p class="management-note">
        Workspace membership and management permissions.
      </p>
      {#if canMembers}<button class="primary" onclick={openAdd}
          >Add member</button
        >{/if}
    </div>
    {#if canPermissions}<div class="view-tabs">
        <button
          aria-pressed={tab === "members"}
          class:active={tab === "members"}
          onclick={() => {
            tab = "members";
            query = "";
          }}>Members</button
        ><button
          aria-pressed={tab === "groups"}
          class:active={tab === "groups"}
          onclick={() => {
            tab = "groups";
            query = "";
          }}>Group permissions</button
        >
      </div>{/if}
    {#if tab === "groups"}<p class="management-note">
        Site groups supply these permissions to their workspace members.
        Granting a group permissions does not add its members to this workspace.
      </p>{/if}
    <form
      class="management-search"
      onsubmit={(e) => {
        e.preventDefault();
        void load();
      }}
    >
      <div class="field">
        <label for={`${id}-search`}
          >Search {tab === "members" ? "members" : "groups"}</label
        ><input id={`${id}-search`} bind:value={query} />
      </div>
      <button class="secondary" disabled={loading}>Search</button>
    </form>
    {#if error}<p class="notice error" role="alert">{error}</p>{/if}
    <ul class="management-list">
      {#if tab === "members"}{#each members as member}<li>
            <div class="member-row">
              <div class="identity">
                <strong>{member.display_name || member.username}</strong><span
                  >@{member.username} · {member.status}{member.superuser
                    ? " · Superuser"
                    : ""}</span
                >
              </div>
              {#if canPermissions}<button
                  class="text-button"
                  onclick={() => edit(member)}>Manage permissions</button
                >{/if}{#if canMembers}<button
                  class="text-button"
                  disabled={!member.can_remove || busy}
                  title={!member.can_remove
                    ? "You must hold every workspace permission this member has."
                    : undefined}
                  onclick={() => {
                    target = member;
                    remove?.showModal();
                  }}>Remove</button
                >{/if}
            </div>
          </li>{/each}{:else}{#each groups as group}<li>
            <div class="member-row">
              <div class="identity">
                <strong>{group.name}</strong><span
                  >{group.direct_permissions.length
                    ? `${group.direct_permissions.length} workspace permissions`
                    : "No workspace permissions"}</span
                >
              </div>
              <button class="text-button" onclick={() => edit(group)}
                >Manage permissions</button
              >
            </div>
          </li>{/each}{/if}
    </ul>
    {#if loading}<p class="hint" role="status">
        Loading…
      </p>{:else if !(tab === "members" ? members.length : groups.length) && !error}<p
        class="management-note"
      >
        No {tab} found.
      </p>{/if}{#if more}<div class="management-actions">
        <button class="secondary" disabled={loading} onclick={() => load(false)}
          >Load more</button
        >
      </div>{/if}
  </div>{:else}<p class="notice error" role="alert">
    You don’t have permission to manage this workspace’s members.
  </p>{/if}
<PermissionsDialog bind:this={permissions} />
<dialog
  class="management-dialog"
  bind:this={add}
  aria-labelledby={`${id}-add`}
  oncancel={(e) => {
    if (busy) e.preventDefault();
  }}
>
  <h2 id={`${id}-add`}>Add member</h2>
  <p class="management-note">
    Choose an existing site account. Any permissions granted to their site
    groups will apply immediately.
  </p>
  <form
    class="management-search"
    onsubmit={(e) => {
      e.preventDefault();
      void searchCandidates();
    }}
  >
    <div class="field">
      <label for={`${id}-candidate`}>Search accounts</label><input
        id={`${id}-candidate`}
        bind:value={candidateQuery}
      />
    </div>
    <button class="secondary" disabled={busy || candidateLoading}>Search</button
    >
  </form>
  {#if candidateError}<p class="notice error" role="alert">
      {candidateError}
    </p>{/if}
  <ul class="management-list">
    {#each candidates as member}<li>
        <div class="member-row">
          <div class="identity">
            <strong>{member.display_name || member.username}</strong><span
              >@{member.username} · {member.status}</span
            >
          </div>
          <button
            class="secondary"
            disabled={busy}
            onclick={() => membership(member, true)}>Add</button
          >
        </div>
      </li>{/each}
  </ul>
  {#if candidateLoading}<p class="hint" role="status">
      Loading accounts…
    </p>{:else if !candidates.length}<p class="management-note">
      No accounts found.
    </p>{/if}{#if candidateMore}<button
      class="secondary"
      disabled={busy || candidateLoading}
      onclick={() => searchCandidates(false)}>Load more</button
    >{/if}
  <div class="management-actions">
    <button class="secondary" disabled={busy} onclick={() => add?.close()}
      >Cancel</button
    >
  </div>
</dialog>
<dialog
  class="management-dialog"
  bind:this={remove}
  aria-labelledby={`${id}-remove`}
  oncancel={(e) => {
    if (busy) e.preventDefault();
  }}
>
  <h2 id={`${id}-remove`}>Remove @{target?.username}?</h2>
  <p class="management-note">
    Their direct workspace grants will be removed.{#if target?.superuser}
      They will retain access through site Superuser.{:else}
      Their agents will also lose access through them.{/if}
  </p>
  <div class="management-actions">
    <button class="secondary" disabled={busy} onclick={() => remove?.close()}
      >Cancel</button
    ><button
      class="primary"
      disabled={busy}
      onclick={() => target && membership(target, false)}
      >{busy ? "Removing…" : "Remove member"}</button
    >
  </div>
</dialog>

<style>
  .view-tabs {
    display: flex;
    gap: 24px;
    border-bottom: 1px solid var(--panel-border);
    margin-bottom: 20px;
  }
  .view-tabs button {
    padding: 12px 0;
    background: none;
    border: 0;
    border-bottom: 2px solid transparent;
    color: var(--muted);
    font-size: 14px;
  }
  .view-tabs .active {
    border-bottom-color: var(--accent);
    color: var(--text);
  }
</style>
