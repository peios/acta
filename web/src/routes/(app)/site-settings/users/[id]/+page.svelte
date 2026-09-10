<script lang="ts">
  import { untrack } from "svelte";
  import { page } from "$app/state";
  import { api, errorMessage, type Account } from "$lib/api";
  import { useAccount } from "$lib/account-context";
  import { takeInvitation, type ManagedAccount } from "$lib/management";
  import ProfileForm from "$lib/components/settings/ProfileForm.svelte";
  import LinkDialog from "$lib/components/management/LinkDialog.svelte";
  import GroupMemberships from "$lib/components/management/GroupMemberships.svelte";
  import PermissionsDialog from "$lib/components/management/PermissionsDialog.svelte";
  import { checkPermission, canIssueLink } from "$lib/permissions";
  let permissions = $state<PermissionsDialog>();
  const current = useAccount();
  let user = $state<ManagedAccount | null>(null),
    error = $state(""),
    busy = $state(false),
    link = $state<LinkDialog>(),
    confirm = $state<HTMLDialogElement>();
  let loadGeneration = 0;
  async function load(id: string) {
    const generation = ++loadGeneration;
    error = "";
    if (user?.id !== id) user = null;
    try {
      const result = await api<ManagedAccount>(`users/${id}`);
      if (generation !== loadGeneration) return;
      user = result;
      const created = takeInvitation(id);
      if (created && canIssueLink(current.account, user))
        void link?.open(user, created);
    } catch (e) {
      if (generation === loadGeneration) error = errorMessage(e);
    }
  }
  $effect(() => {
    const id = page.params.id;
    if (id)
      untrack(() => {
        void load(id);
      });
  });
  function saved(value: Account) {
    user = { ...user!, ...value };
    if (value.id === current.account.id) current.update(value);
  }
  async function toggle() {
    if (!user || busy) return;
    busy = true;
    error = "";
    try {
      await api(`users/${user.id}/disabled`, {
        disabled: user.status !== "disabled",
      });
      confirm?.close();
      await load(user.id);
    } catch (e) {
      error = errorMessage(e);
      confirm?.close();
    } finally {
      busy = false;
    }
  }
</script>

<div class="user-page">
  <a class="back" href="/site-settings/users">← All users</a>
  {#if error && checkPermission(current.account, "site.users.view")}<p
      class="notice error"
      role="alert"
    >
      {error}
    </p>{/if}
  {#if !checkPermission(current.account, "site.users.view")}<p
      class="notice error"
    >
      You don’t have permission to view this page.
    </p>
  {:else if user}<div class="heading">
      <h2>{user.display_name || user.username}</h2>
      <span>{user.status}</span>
    </div>
    {#key user.id}<ProfileForm
        account={user}
        endpoint={`users/${user.id}`}
        managed
        onSaved={saved}
      />{/key}
    {#if checkPermission(current.account, "site.permissions.manage")}<section>
        <h3>Permissions</h3>
        <p>Choose what this account can do and whether MFA is required.</p>
        <button class="secondary" onclick={() => permissions?.open(user!)}
          >Manage permissions</button
        >
      </section>{/if}
    <GroupMemberships account={user} onChanged={() => load(user!.id)} />
    {#if canIssueLink(current.account, user)}<section>
        <h3>{user.pending ? "Invitation" : "Security"}</h3>
        <p>
          {user.status === "disabled"
            ? "Re-enable this account before issuing an invitation or recovery link."
            : user.pending
              ? "This account is waiting for its owner to set a password."
              : "Generate a private link so this user can choose a new password."}
        </p>
        <button
          class="secondary"
          disabled={user.status === "disabled"}
          onclick={() => link?.open(user!)}
          >{user.pending ? "Generate invitation" : "Reset password"}</button
        >
      </section>{/if}
    {#if checkPermission(current.account, "site.users.disable")}<section>
        <h3>Account status</h3>
        <p>
          {user.status === "disabled"
            ? "Re-enabling allows a fresh sign-in. Revoked sessions and links stay invalid."
            : "Disabling immediately blocks access and revokes existing sessions and outstanding links. Account details and work history are preserved."}
        </p>
        <button
          class="secondary"
          disabled={busy || user.last_active_superuser}
          onclick={() => confirm?.showModal()}
          >{user.status === "disabled"
            ? "Re-enable account"
            : "Disable account"}</button
        >
        {#if user.last_active_superuser}<p>
            The last active Superuser cannot be disabled.
          </p>{/if}
      </section>{/if}
  {:else if !error}<p class="hint" role="status">Loading account…</p>{/if}
</div>
<LinkDialog bind:this={link} />
<PermissionsDialog
  bind:this={permissions}
  onSaved={(value) => {
    saved(value);
    void load(value.id);
  }}
/>
<dialog
  bind:this={confirm}
  aria-labelledby="status-title"
  oncancel={(e) => {
    if (busy) e.preventDefault();
  }}
>
  <h2 id="status-title">
    {user?.status === "disabled" ? "Re-enable" : "Disable"} @{user?.username}?
  </h2>
  <p>
    {user?.status === "disabled"
      ? "The account will become available again. Its owner must sign in afresh."
      : "This account will lose access immediately. Existing sessions and invitation or recovery links will be revoked."}
  </p>
  <div class="actions">
    <button class="secondary" disabled={busy} onclick={() => confirm?.close()}
      >Cancel</button
    ><button class="primary" disabled={busy} onclick={toggle}
      >{busy ? "Updating…" : "Confirm"}</button
    >
  </div>
</dialog>

<style>
  .primary {
    width: auto;
    flex-shrink: 0;
  }
  .user-page {
    max-width: 640px;
    width: 100%;
    padding: 8px 0 40px;
  }
  .back {
    font-size: 13px;
    text-decoration: none;
    color: var(--muted);
  }
  .heading {
    display: flex;
    gap: 16px;
    align-items: center;
    margin: 24px 0;
  }
  .heading h2 {
    margin: 0;
    font-size: 23px;
    overflow-wrap: anywhere;
  }
  .heading span {
    font-size: 12px;
    color: var(--muted);
    text-transform: capitalize;
  }
  section {
    padding: 28px 0;
    border-top: 1px solid var(--panel-border);
  }
  h3 {
    font-size: 16px;
    margin: 0;
  }
  section p,
  dialog p {
    font-size: 13px;
    line-height: 1.7;
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
  dialog h2 {
    font-size: 20px;
    margin: 0;
  }
  .actions {
    display: flex;
    gap: 12px;
    justify-content: flex-end;
    margin-top: 24px;
  }
</style>
