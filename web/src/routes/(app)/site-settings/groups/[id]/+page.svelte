<script lang="ts">
  import { untrack } from "svelte";
  import { page } from "$app/state";
  import { api, errorMessage, type Account } from "$lib/api";
  import { useAccount } from "$lib/account-context";
  import { checkPermission } from "$lib/permissions";
  import {
    changeMembership,
    groupAssignable,
    isLastSuperuserSource,
    type Group,
  } from "$lib/groups";
  import GroupForm from "$lib/components/management/GroupForm.svelte";
  import PermissionsDialog from "$lib/components/management/PermissionsDialog.svelte";
  import "$lib/components/management/management.css";
  const current = useAccount();
  let group = $state<Group | null>(null),
    members = $state<Account[]>([]),
    query = $state(""),
    offset = $state(0),
    more = $state(false),
    loading = $state(false),
    error = $state(""),
    busy = $state(false),
    permissions = $state<PermissionsDialog>();
  let generation = 0;
  async function load(id: string, reset = false) {
    const request = ++generation;
    if (reset) offset = 0;
    if (group?.id !== id) group = null;
    loading = true;
    error = "";
    try {
      const [g, result] = await Promise.all([
        api<Group>(`groups/${id}`),
        api<{ users: Account[]; more: boolean }>(
          `groups/${id}/members?${new URLSearchParams({ q: query, offset: String(offset) })}`,
        ),
      ]);
      if (request !== generation) return;
      group = g;
      members = result.users;
      more = result.more;
    } catch (e) {
      if (request === generation) error = errorMessage(e);
    } finally {
      if (request === generation) loading = false;
    }
  }
  $effect(() => {
    const id = page.params.id;
    if (id && checkPermission(current.account, "site.permissions.manage"))
      untrack(() => {
        void load(id);
      });
  });
  function removable(a: Account) {
    return !!group && !isLastSuperuserSource(a, group);
  }
  async function remove(a: Account) {
    if (!group || busy) return;
    busy = true;
    error = "";
    try {
      await changeMembership(group, a, false);
      await load(group.id);
    } catch (e) {
      error = errorMessage(e);
    } finally {
      busy = false;
    }
  }
</script>

<div class="management-page">
  {#if !checkPermission(current.account, "site.permissions.manage")}<p
      class="notice error"
    >
      You don’t have permission to view this page.
    </p>{:else}
    <a class="management-back" href="/site-settings/groups">← All groups</a>
    {#if error}<p class="notice error" role="alert">{error}</p>
      <button class="secondary" onclick={() => load(page.params.id!)}
        >Reload group</button
      >{/if}
    {#if group}
      <div class="management-heading">
        <h2>
          {group.name}{#if group.is_default && group.name.toLowerCase() !== "default"}<span
              class="badge">Default</span
            >{/if}
        </h2>
      </div>
      {#if group.is_default}<p class="management-note">
          New accounts join this group automatically. Existing accounts keep
          their current memberships. Individual users can be removed from
          Default.
        </p>{/if}
      {#key group.id}<GroupForm {group} onSaved={(g) => (group = g)} />{/key}
      <section class="management-section">
        <h3>Permissions</h3>
        <p>
          Every member receives these permissions. MFA requirements also apply
          to every member.
        </p>
        <button class="secondary" onclick={() => permissions?.open(group!)}
          >Manage permissions</button
        >
      </section>
      <section class="management-section">
        <h3>Members <span class="badge">{group.member_count}</span></h3>
        <p>
          {#if checkPermission(current.account, "site.users.view")}Add members
            from the Groups section on a <a href="/site-settings/users"
              >user’s account page</a
            >.{:else}User pages require View users. You can manage existing
            memberships here.{/if}
        </p>
        <form
          class="management-search"
          onsubmit={(e) => {
            e.preventDefault();
            void load(group!.id, true);
          }}
        >
          <div class="field">
            <label for="member-search">Search members</label><input
              id="member-search"
              bind:value={query}
              disabled={loading}
            />
          </div>
          <button class="secondary" disabled={loading}>Search</button>
        </form>
        {#if loading}<p class="hint" role="status">
            Loading members…
          </p>{:else if !members.length}<p class="hint">
            {query
              ? "No members match this search."
              : "This group has no members yet."}
          </p>{/if}
        <ul class="management-list">
          {#each members as a (a.id)}<li>
              <div class="member-row">
                <span class="identity"
                  ><strong
                    >{#if checkPermission(current.account, "site.users.view")}<a
                        href={`/site-settings/users/${a.id}`}
                        >{a.display_name || a.username}</a
                      >{:else}{a.display_name || a.username}{/if}</strong
                  ><span>@{a.username}</span></span
                >{#if groupAssignable(current.account, group)}<button
                    class="text-button"
                    disabled={busy || !removable(a)}
                    title={!removable(a)
                      ? "The last active Superuser must retain access."
                      : undefined}
                    onclick={() => remove(a)}>Remove</button
                  >{/if}
              </div>
            </li>{/each}
        </ul>
        {#if offset || more}<div class="management-actions">
            <button
              class="secondary"
              disabled={loading || !offset}
              onclick={() => {
                offset -= 50;
                void load(group!.id);
              }}>Previous</button
            ><span class="hint">Page {offset / 50 + 1}</span><button
              class="secondary"
              disabled={loading || !more}
              onclick={() => {
                offset += 50;
                void load(group!.id);
              }}>Next</button
            >
          </div>{/if}
      </section>
    {:else if !error}<p class="hint" role="status">Loading group…</p>{/if}{/if}
</div>
<PermissionsDialog
  bind:this={permissions}
  onGroupSaved={(g) => {
    group = g;
    void load(g.id);
  }}
/>
