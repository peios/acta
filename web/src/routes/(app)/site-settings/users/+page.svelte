<script lang="ts">
  import { onMount, tick } from "svelte";
  import { goto } from "$app/navigation";
  import { api, APIError, errorMessage } from "$lib/api";
  import { useAccount } from "$lib/account-context";
  import { handoffInvitation, type ManagedAccount } from "$lib/management";
  import { checkPermission, canOpenUsers } from "$lib/permissions";
  import LinkDialog from "$lib/components/management/LinkDialog.svelte";
  let invitation = $state<LinkDialog>();
  const current = useAccount();
  let users = $state<ManagedAccount[]>([]),
    query = $state(""),
    status = $state(""),
    offset = $state(0),
    more = $state(false),
    loading = $state(false),
    error = $state("");
  let dialog = $state<HTMLDialogElement>(),
    username = $state(""),
    displayName = $state(""),
    busy = $state(false),
    createError = $state(""),
    fields = $state<Record<string, string>>({});
  async function load(reset = false) {
    if (loading) return;
    if (reset) offset = 0;
    loading = true;
    error = "";
    try {
      const result = await api<{ users: ManagedAccount[]; more: boolean }>(
        `users?${new URLSearchParams({ q: query, status, offset: String(offset) })}`,
      );
      users = result.users;
      more = result.more;
    } catch (e) {
      error = errorMessage(e);
    } finally {
      loading = false;
    }
  }
  onMount(() => {
    if (checkPermission(current.account, "site.users.view")) void load();
  });
  function create() {
    username = "";
    displayName = "";
    createError = "";
    fields = {};
    dialog?.showModal();
  }
  async function submit(e: SubmitEvent) {
    e.preventDefault();
    if (busy) return;
    busy = true;
    createError = "";
    fields = {};
    try {
      const result = await api<{ account: ManagedAccount; url: string }>(
        "users",
        { username, display_name: displayName },
      );
      dialog?.close();
      if (checkPermission(current.account, "site.users.view")) {
        handoffInvitation(result.account.id, result.url);
        await goto(`/site-settings/users/${result.account.id}`);
      } else void invitation?.open(result.account, result.url);
    } catch (e) {
      createError = errorMessage(e);
      fields = e instanceof APIError ? e.fields : {};
    } finally {
      busy = false;
      await tick();
      dialog
        ?.querySelector<HTMLElement>('[aria-invalid="true"], [role="alert"]')
        ?.focus();
    }
  }
</script>

<div class="users-page">
  {#if !canOpenUsers(current.account)}<p class="notice error">
      You don’t have permission to view this page.
    </p>{:else}
    {#if checkPermission(current.account, "site.users.create") && !current.account.can_create_users}<p
        class="hint"
      >
        Creating accounts is unavailable because Default grants permissions you
        cannot assign.
      </p>{/if}
    <div class="heading">
      <p class="intro">Manage the people who can use this Acta installation.</p>
      {#if current.account.can_create_users}<button
          class="primary"
          onclick={create}>Create user</button
        >{/if}
    </div>
    {#if checkPermission(current.account, "site.users.view")}<form
        class="search"
        onsubmit={(e) => {
          e.preventDefault();
          void load(true);
        }}
      >
        <div class="field">
          <label for="user-search">Search users</label><input
            id="user-search"
            bind:value={query}
            placeholder="Name or username"
            disabled={loading}
          />
        </div>
        <div class="field">
          <label for="status">Status</label><select
            id="status"
            bind:value={status}
            disabled={loading}
            ><option value="">All</option><option value="active">Active</option
            ><option value="pending">Pending</option><option value="disabled"
              >Disabled</option
            ></select
          >
        </div>
        <button class="secondary" disabled={loading}>Search</button>
      </form>
      {#if error}<p class="notice error" role="alert">{error}</p>{/if}
      {#if loading}<p role="status" class="hint">
          Loading users…
        </p>{:else if !error && users.length === 0}<p class="empty">
          No users match this search.
        </p>{/if}
      <ul aria-label="Users">
        {#each users as user (user.id)}<li>
            <a href={`/site-settings/users/${user.id}`}
              ><span class="avatar" aria-hidden="true"
                >{Array.from(
                  user.display_name || user.username,
                )[0]?.toUpperCase()}</span
              ><span class="identity"
                ><strong>{user.display_name || user.username}</strong><span
                  >@{user.username}</span
                ></span
              ><span class="status">{user.status}</span><span aria-hidden="true"
                >›</span
              ></a
            >
          </li>{/each}
      </ul>
      {#if offset > 0 || more}<div class="pagination">
          <button
            class="secondary"
            disabled={loading || offset === 0}
            onclick={() => {
              offset = Math.max(0, offset - 50);
              void load();
            }}>Previous</button
          ><span>Page {offset / 50 + 1}</span><button
            class="secondary"
            disabled={loading || !more}
            onclick={() => {
              offset += 50;
              void load();
            }}>Next</button
          >
        </div>{/if}
    {/if}{/if}
</div>
<LinkDialog bind:this={invitation} />
<dialog
  bind:this={dialog}
  aria-labelledby="create-title"
  oncancel={(e) => {
    if (busy) e.preventDefault();
  }}
>
  <h2 id="create-title">Create user</h2>
  <p class="hint">Create a pending account, then share its invitation link.</p>
  <form onsubmit={submit} aria-busy={busy}>
    {#if createError}<p class="notice error" role="alert" tabindex="-1">
        {createError}
      </p>{/if}
    <div class="field">
      <label for="new-username">Username</label><input
        id="new-username"
        bind:value={username}
        required
        maxlength="32"
        autocapitalize="none"
        spellcheck="false"
        disabled={busy}
        aria-invalid={!!fields.username}
      />
      <p class="hint">
        1–32 letters or numbers, with dots, hyphens or underscores between them.
      </p>
    </div>
    <div class="field">
      <label for="new-display"
        >Display name <span class="hint">(optional)</span></label
      ><input
        id="new-display"
        bind:value={displayName}
        disabled={busy}
        aria-invalid={!!fields.display_name}
      />
    </div>
    <div class="actions">
      <button
        type="button"
        class="secondary"
        disabled={busy}
        onclick={() => dialog?.close()}>Cancel</button
      ><button class="primary" disabled={busy}
        >{busy ? "Creating…" : "Create user"}</button
      >
    </div>
  </form>
</dialog>

<style>
  .primary {
    width: auto;
    flex-shrink: 0;
  }
  .users-page {
    max-width: 800px;
    width: 100%;
    padding: 8px 0 32px;
  }
  .heading {
    display: flex;
    align-items: center;
    justify-content: space-between;
    gap: 24px;
    margin-bottom: 28px;
  }
  .intro {
    margin: 0;
    font-size: 14px;
  }
  .heading button {
    white-space: nowrap;
  }
  .search {
    display: flex;
    align-items: end;
    gap: 12px;
    margin-bottom: 28px;
  }
  .search .field:first-child {
    flex: 1;
  }
  ul {
    padding: 0;
    list-style: none;
    margin: 0;
  }
  li {
    border-bottom: 1px solid var(--panel-border);
  }
  li a {
    display: flex;
    align-items: center;
    gap: 16px;
    padding: 18px 8px;
    text-decoration: none;
    color: var(--text);
    border-radius: 8px;
  }
  li a:hover {
    background: var(--hover-surface);
  }
  .avatar {
    display: grid;
    place-items: center;
    width: 36px;
    height: 36px;
    border-radius: 50%;
    background: var(--scope-active);
    color: var(--accent);
  }
  .identity {
    display: grid;
    gap: 5px;
    flex: 1;
    min-width: 0;
  }
  .identity strong {
    font-size: 14px;
    overflow-wrap: anywhere;
  }
  .identity > span,
  .status {
    font-size: 12px;
    color: var(--muted);
  }
  .status {
    text-transform: capitalize;
  }
  .pagination {
    display: flex;
    align-items: center;
    justify-content: flex-end;
    gap: 16px;
    margin-top: 24px;
    font-size: 13px;
  }
  .empty {
    font-size: 14px;
    color: var(--muted);
  }
  dialog {
    width: min(460px, calc(100vw - 32px));
    padding: 28px;
    border: 1px solid var(--panel-border);
    border-radius: 14px;
    background: var(--surface);
    color: var(--text);
  }
  dialog::backdrop {
    background: #00000060;
  }
  h2 {
    margin: 0 0 12px;
    font-size: 21px;
  }
  .actions {
    display: flex;
    justify-content: flex-end;
    gap: 12px;
  }
  @media (max-width: 560px) {
    .heading {
      align-items: start;
      flex-direction: column;
    }
    .search {
      flex-wrap: wrap;
    }
    .search .field:first-child {
      flex-basis: 100%;
    }
  }
</style>
