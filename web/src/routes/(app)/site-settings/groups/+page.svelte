<script lang="ts">
  import { onMount } from "svelte";
  import { goto } from "$app/navigation";
  import { api, errorMessage } from "$lib/api";
  import { useAccount } from "$lib/account-context";
  import { checkPermission } from "$lib/permissions";
  import type { Group } from "$lib/groups";
  import GroupForm from "$lib/components/management/GroupForm.svelte";
  import "$lib/components/management/management.css";
  const current = useAccount();
  let createBusy = $state(false);
  let groups = $state<Group[]>([]),
    query = $state(""),
    offset = $state(0),
    more = $state(false),
    loading = $state(false),
    error = $state(""),
    dialog = $state<HTMLDialogElement>(),
    generation = $state(0);
  async function load(reset = false) {
    if (loading) return;
    if (reset) offset = 0;
    loading = true;
    error = "";
    try {
      const result = await api<{ groups: Group[]; more: boolean }>(
        `groups?${new URLSearchParams({ q: query, offset: String(offset) })}`,
      );
      groups = result.groups;
      more = result.more;
    } catch (e) {
      error = errorMessage(e);
    } finally {
      loading = false;
    }
  }
  onMount(() => {
    if (checkPermission(current.account, "site.permissions.manage"))
      void load();
  });
</script>

<div class="management-page">
  {#if !checkPermission(current.account, "site.permissions.manage")}<p
      class="notice error"
    >
      You don’t have permission to view this page.
    </p>{:else}
    <div class="management-heading">
      <p>Share permissions across people who work together.</p>
      <button
        class="primary"
        onclick={() => {
          generation++;
          dialog?.showModal();
        }}>Create group</button
      >
    </div>
    <form
      class="management-search"
      onsubmit={(e) => {
        e.preventDefault();
        void load(true);
      }}
    >
      <div class="field">
        <label for="group-search">Search groups</label><input
          id="group-search"
          bind:value={query}
          placeholder="Group name"
          disabled={loading}
        />
      </div>
      <button class="secondary" disabled={loading}>Search</button>
    </form>
    {#if error}<p class="notice error" role="alert">{error}</p>{/if}
    {#if loading}<p class="hint" role="status">
        Loading groups…
      </p>{:else if !error && groups.length === 0}<p class="hint">
        No groups match this search.
      </p>{/if}
    <ul class="management-list">
      {#each groups as group (group.id)}<li>
          <a href={`/site-settings/groups/${group.id}`}
            ><span class="identity"
              ><strong
                >{group.name}{#if group.is_default && group.name.toLowerCase() !== "default"}<span
                    class="badge">Default</span
                  >{/if}</strong
              >{#if group.description}<span>{group.description}</span
                >{/if}</span
            ><span class="hint"
              >{group.member_count}
              {group.member_count === 1 ? "member" : "members"}</span
            ><span aria-hidden="true">›</span></a
          >
        </li>{/each}
    </ul>
    {#if offset > 0 || more}<div class="management-actions">
        <button
          class="secondary"
          disabled={loading || !offset}
          onclick={() => {
            offset -= 50;
            void load();
          }}>Previous</button
        ><span class="hint">Page {offset / 50 + 1}</span><button
          class="secondary"
          disabled={loading || !more}
          onclick={() => {
            offset += 50;
            void load();
          }}>Next</button
        >
      </div>{/if}
  {/if}
</div>
<dialog
  class="management-dialog"
  bind:this={dialog}
  aria-labelledby="create-group-title"
  oncancel={(e) => {
    if (createBusy) e.preventDefault();
  }}
>
  <h2 id="create-group-title">Create group</h2>
  <p class="hint">Create the group, then choose its permissions and members.</p>
  {#key generation}<GroupForm
      bind:busy={createBusy}
      onCancel={() => dialog?.close()}
      onSaved={(g) => {
        dialog?.close();
        void goto(`/site-settings/groups/${g.id}`);
      }}
    />{/key}
</dialog>
