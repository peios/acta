<script lang="ts">
  import { api, APIError, errorMessage, type Account } from "$lib/api";
  import { useAccount } from "$lib/account-context";
  import { checkPermission } from "$lib/permissions";
  import {
    changeMembership,
    groupAssignable,
    isLastSuperuserSource,
    type Group,
  } from "$lib/groups";
  import "./management.css";
  let {
    account,
    onChanged,
  }: { account: Account; onChanged: () => void | Promise<void> } = $props();
  const current = useAccount();
  let dialog = $state<HTMLDialogElement>(),
    groups = $state<Group[]>([]),
    query = $state(""),
    offset = $state(0),
    more = $state(false),
    loading = $state(false),
    busy = $state(false),
    error = $state(""),
    stale = $state(false);
  const id = $props.id();
  function removable(g: Group) {
    return (
      groupAssignable(current.account, g) && !isLastSuperuserSource(account, g)
    );
  }
  async function load(reset = false) {
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
  async function update(g: Group, member: boolean) {
    if (busy) return;
    busy = true;
    error = "";
    stale = false;
    try {
      await changeMembership(g, account, member);
      dialog?.close();
      await onChanged();
    } catch (e) {
      error = errorMessage(e);
      stale = e instanceof APIError && e.code === "permissions_changed";
    } finally {
      busy = false;
    }
  }
</script>

<section class="management-section">
  <h3>Groups</h3>
  <p>
    Membership adds the group’s permissions and MFA requirements to this
    account.
  </p>
  {#if error && !dialog?.open}<p class="notice error" role="alert">{error}</p>
    {#if stale}<button
        class="secondary"
        onclick={() => {
          error = "";
          stale = false;
          void onChanged();
        }}>Reload account</button
      >{/if}{/if}
  {#if !account.groups.length}<p class="hint">
      This account isn’t a member of any groups.
    </p>{:else}<ul class="management-list">
      {#each account.groups as g (g.id)}<li>
          <div class="member-row">
            <span class="identity"
              ><strong
                >{#if checkPermission(current.account, "site.permissions.manage")}<a
                    href={`/site-settings/groups/${g.id}`}>{g.name}</a
                  >{:else}{g.name}{/if}{#if g.is_default && g.name.toLowerCase() !== "default"}<span
                    class="badge">Default</span
                  >{/if}</strong
              >{#if g.direct_require_mfa}<span>Requires MFA</span>{/if}</span
            >{#if checkPermission(current.account, "site.permissions.manage") && groupAssignable(current.account, g)}<button
                class="text-button"
                disabled={busy || !removable(g)}
                title={!removable(g)
                  ? "The last active Superuser must retain access."
                  : undefined}
                onclick={() => update(g, false)}>Remove</button
              >{/if}
          </div>
        </li>{/each}
    </ul>{/if}
  {#if checkPermission(current.account, "site.permissions.manage")}<div
      class="management-actions"
    >
      <button
        class="secondary"
        onclick={() => {
          query = "";
          dialog?.showModal();
          void load(true);
        }}>Add to group</button
      >
    </div>{/if}
</section>
<dialog
  class="management-dialog"
  bind:this={dialog}
  aria-labelledby={`${id}-title`}
  oncancel={(e) => {
    if (busy) e.preventDefault();
  }}
>
  <h2 id={`${id}-title`}>Add to group</h2>
  <p class="hint">
    For @{account.username}. Existing invitation and recovery links will need
    replacing.
  </p>
  <form
    class="management-search"
    onsubmit={(e) => {
      e.preventDefault();
      void load(true);
    }}
  >
    <div class="field">
      <label for={`${id}-search`}>Search groups</label><input
        id={`${id}-search`}
        bind:value={query}
        disabled={loading || busy}
      />
    </div>
    <button class="secondary" disabled={loading || busy}>Search</button>
  </form>
  {#if error}<p class="notice error" role="alert">{error}</p>{/if}
  {#if loading}<p class="hint" role="status">Loading groups…</p>{:else}
    <ul class="management-list">
      {#each groups as g (g.id)}<li>
          <div class="member-row">
            <span class="identity"
              ><strong>{g.name}</strong><span
                >{account.groups.some((x) => x.id === g.id)
                  ? "Already a member"
                  : !g.assignable
                    ? "You cannot assign this group’s permissions."
                    : g.description}</span
              ></span
            >{#if g.assignable && !account.groups.some((x) => x.id === g.id)}<button
                class="secondary"
                disabled={busy || stale}
                onclick={() => update(g, true)}>Add</button
              >{/if}
          </div>
        </li>{/each}
    </ul>
    {#if !groups.length}<p class="hint">
        No groups match this search.
      </p>{/if}{/if}
  <div class="management-actions">
    {#if offset || more}<button
        class="secondary"
        disabled={loading || busy || !offset}
        onclick={() => {
          offset -= 50;
          void load();
        }}>Previous</button
      ><button
        class="secondary"
        disabled={loading || busy || !more}
        onclick={() => {
          offset += 50;
          void load();
        }}>Next</button
      >{/if}<button
      class="secondary"
      disabled={busy}
      onclick={() => {
        dialog?.close();
        if (stale) void onChanged();
      }}>Close</button
    >
  </div>
</dialog>
