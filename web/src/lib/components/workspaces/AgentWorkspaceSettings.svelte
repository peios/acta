<script lang="ts">
  import { beforeNavigate } from "$app/navigation";
  import { onMount } from "svelte";
  import { api, errorMessage } from "$lib/api";
  import {
    type AgentWorkspaceAccess,
    type AgentWorkspacePolicy,
  } from "$lib/workspaces";
  import type { Permission } from "$lib/permissions";
  import PermissionsDialog from "$lib/components/management/PermissionsDialog.svelte";
  import "$lib/components/management/management.css";
  let { agentID }: { agentID: string } = $props();
  const id = $props.id();
  let settings = $state<AgentWorkspaceAccess | null>(null),
    all = $state(true),
    selected = $state<string[]>([]),
    error = $state(""),
    message = $state(""),
    busy = $state(false),
    permissions = $state<PermissionsDialog>();
  const dirty = $derived(
    !!settings &&
      (all !== settings.all ||
        JSON.stringify([...selected].sort()) !==
          JSON.stringify(
            settings.workspaces
              .filter((p) => p.selected)
              .map((p) => p.workspace.id)
              .sort(),
          )),
  );
  beforeNavigate((n) => {
    if (
      dirty &&
      (busy ||
        n.willUnload ||
        !window.confirm("Discard your unsaved agent workspace access changes?"))
    )
      n.cancel();
  });
  async function load() {
    error = "";
    try {
      settings = await api<AgentWorkspaceAccess>(
        `agents/${agentID}/workspaces`,
      );
      all = settings.all;
      selected = settings.workspaces
        .filter((p) => p.selected)
        .map((p) => p.workspace.id);
    } catch (e) {
      error = errorMessage(e);
    }
  }
  onMount(load);
  function changeAll(value: boolean) {
    if (!value && all)
      selected = settings?.workspaces.map((p) => p.workspace.id) ?? [];
    all = value;
    message = "";
  }
  async function save() {
    if (!settings || busy) return;
    busy = true;
    error = "";
    message = "";
    try {
      await api(`agents/${agentID}/workspaces`, {
        version: settings.version,
        all,
        selected,
      });
      await load();
      message = "Workspace access saved.";
    } catch (e) {
      error = errorMessage(e);
    } finally {
      busy = false;
    }
  }
  async function edit(p: AgentWorkspacePolicy) {
    if (!settings || dirty) return;
    const version = settings.version;
    try {
      const r = await api<{ permissions: Permission[] }>(
        "workspaces/permissions",
      );
      permissions?.openScoped({
        label: p.workspace.name,
        grants: p.permissions,
        ceiling: p.owner_permissions,
        inherit: p.inherit,
        catalogue: r.permissions,
        save: async (grants, inherit) => {
          await api(
            `agents/${agentID}/workspaces/${p.workspace.id}/permissions`,
            { version, inherit, permissions: grants },
          );
          await load();
        },
      });
    } catch (e) {
      error = errorMessage(e);
    }
  }
</script>

<section class="management-section">
  <h3>Workspace access</h3>
  <p>
    By default, this agent can access all your current and future workspaces and
    inherits your permissions.
  </p>
  {#if error}<p class="notice error" role="alert">{error}</p>
    <button class="secondary" onclick={load}>Reload workspace access</button
    >{/if}{#if settings}<label class="all"
      ><input
        type="checkbox"
        role="switch"
        checked={all}
        disabled={busy}
        onchange={(e) => changeAll(e.currentTarget.checked)}
      />All workspaces you can access</label
    >{#if !all}<p>Select the workspaces this agent can access.</p>{/if}
    <ul class="management-list">
      {#each settings.workspaces as p}<li>
          <div class="member-row">
            {#if !all}<input
                class="selection"
                id={`${id}-${p.workspace.id}`}
                type="checkbox"
                checked={selected.includes(p.workspace.id)}
                disabled={busy}
                onchange={(e) => {
                  selected = e.currentTarget.checked
                    ? [...selected, p.workspace.id]
                    : selected.filter((v) => v !== p.workspace.id);
                  message = "";
                }}
              />{/if}
            <div class="identity">
              <label for={!all ? `${id}-${p.workspace.id}` : undefined}
                >{p.workspace.name}</label
              ><span
                >{p.inherit ? "Inherits from User" : "Custom permissions"}</span
              >
            </div>
            {#if all || selected.includes(p.workspace.id)}<button
                class="text-button"
                disabled={busy || dirty}
                title={dirty
                  ? "Save workspace access before editing permissions."
                  : undefined}
                onclick={() => edit(p)}>Manage permissions</button
              >{/if}
          </div>
        </li>{/each}
    </ul>
    {#if !settings.workspaces.length}<p>
        Workspaces will appear here when you have access to them.
      </p>{/if}
    <div class="management-actions">
      <button class="secondary" disabled={busy || !dirty} onclick={save}
        >{busy ? "Saving…" : "Save workspace access"}</button
      >{#if dirty}<span class="hint"
          >Save access before editing workspace permissions.</span
        >{/if}{#if message}<span class="hint" role="status">{message}</span
        >{/if}
    </div>{:else if !error}<p role="status">Loading workspace access…</p>{/if}
</section>
<PermissionsDialog bind:this={permissions} />

<style>
  .all {
    display: flex;
    align-items: center;
    gap: 12px;
    font-size: 14px;
  }
  .all input {
    flex-shrink: 0;
  }
  .selection {
    width: 18px;
    height: 18px;
    padding: 0;
    flex-shrink: 0;
    accent-color: var(--accent);
  }
  .identity label {
    font-size: 14px;
  }
  .management-section {
    margin-top: 0;
  }
</style>
