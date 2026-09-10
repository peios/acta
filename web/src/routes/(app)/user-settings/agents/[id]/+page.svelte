<script lang="ts">
  import { useAccount } from "$lib/account-context";
  const account = useAccount();
  import AgentWorkspaceSettings from "$lib/components/workspaces/AgentWorkspaceSettings.svelte";
  import { untrack } from "svelte";
  import { page } from "$app/state";
  import { api, errorMessage, type Account } from "$lib/api";
  import type { ManagedAccount } from "$lib/management";
  import type { SecurityState } from "$lib/security";
  import ProfileForm from "$lib/components/settings/ProfileForm.svelte";
  import SessionList from "$lib/components/settings/SessionList.svelte";
  import PermissionsDialog from "$lib/components/management/PermissionsDialog.svelte";
  let agent = $state<ManagedAccount | null>(null),
    sessions = $state<SecurityState["sessions"]>([]),
    error = $state(""),
    message = $state(""),
    busy = $state(false);
  let permissions = $state<PermissionsDialog>(),
    confirm = $state<HTMLDialogElement>();
  let generation = 0,
    offered = "";
  async function load(id: string) {
    const current = ++generation;
    error = "";
    if (agent?.id !== id) {
      agent = null;
      sessions = [];
    }
    try {
      const [value, state] = await Promise.all([
        api<ManagedAccount>(`agents/${id}`),
        api<{ sessions: SecurityState["sessions"] }>(`agents/${id}/sessions`),
      ]);
      if (current !== generation) return;
      agent = value;
      sessions = state.sessions;
      if (page.url.searchParams.get("created") === "1" && offered !== id) {
        offered = id;
        void permissions?.open(value);
      }
    } catch (e) {
      if (current === generation) error = errorMessage(e);
    }
  }
  $effect(() => {
    const id = page.params.id;
    if (id) untrack(() => void load(id));
  });
  function saved(value: Account) {
    agent = { ...agent!, ...value };
  }
  async function revoke(id = "") {
    if (!agent || busy) return;
    busy = true;
    error = "";
    message = "";
    try {
      await api(`agents/${agent.id}/revoke`, { id });
      message = id ? "Session signed out." : "All agent sessions signed out.";
      await load(agent.id);
    } catch (e) {
      error = errorMessage(e);
    } finally {
      busy = false;
    }
  }
  async function toggle() {
    if (!agent || busy) return;
    busy = true;
    error = "";
    message = "";
    try {
      await api(`agents/${agent.id}/disabled`, {
        disabled: agent.status !== "disabled",
      });
      confirm?.close();
      await load(agent.id);
    } catch (e) {
      error = errorMessage(e);
      confirm?.close();
    } finally {
      busy = false;
    }
  }
</script>

<div class="agent-page">
  <a class="back" href="/user-settings/agents">← All agents</a>
  {#if error}<p class="notice error" role="alert">{error}</p>{/if}
  {#if message}<p class="hint" role="status">{message}</p>{/if}
  {#if agent}
    <div class="heading">
      <h2>{agent.display_name || agent.username_segment}</h2>
      <span>{agent.status}</span>
    </div>
    {#key agent.id}<ProfileForm
        account={agent}
        endpoint={`agents/${agent.id}`}
        managed
        owned
        onSaved={saved}
      />{/key}
    <section>
      <h3>Permissions</h3>
      <p>
        Choose which of your permissions this agent can use. Permissions you
        lose are also removed from this agent.
      </p>
      <button class="secondary" onclick={() => permissions?.open(agent!)}
        >Manage permissions</button
      >
    </section>
    {#key agent.id}<AgentWorkspaceSettings agentID={agent.id} />{/key}
    {#key agent.id}<SessionList
        canMigrate={account.account.permissions.includes("site.superuser")}
        {sessions}
        {busy}
        accountID={agent.id}
        onAccessChanged={() => load(agent!.id)}
        onRevoke={revoke}
        allLabel="Sign out all sessions"
      />{/key}
    <section>
      <h3>Agent status</h3>
      <p>
        {agent.status === "disabled"
          ? "Re-enable this agent to authorize new connections. Previous sessions stay signed out."
          : "Disabling signs out every connection. The agent’s identity and work history are preserved."}
      </p>
      <button
        class="secondary"
        disabled={busy}
        onclick={() => confirm?.showModal()}
        >{agent.status === "disabled"
          ? "Re-enable agent"
          : "Disable agent"}</button
      >
    </section>
  {:else if !error}<p class="hint" role="status">Loading agent…</p>{/if}
</div>
<PermissionsDialog
  bind:this={permissions}
  onSaved={(value) => {
    saved(value);
    void load(value.id);
  }}
/>
<dialog
  bind:this={confirm}
  aria-labelledby="agent-status-title"
  oncancel={(e) => {
    if (busy) e.preventDefault();
  }}
>
  <h2 id="agent-status-title">
    {agent?.status === "disabled" ? "Re-enable" : "Disable"} @{agent?.username}?
  </h2>
  <p>
    {agent?.status === "disabled"
      ? "You can authorize new connections after re-enabling. Old sessions will not be restored."
      : "All of this agent’s sessions will be signed out immediately."}
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
  .agent-page {
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
    align-items: center;
    gap: 16px;
    margin: 24px 0;
  }
  .heading h2 {
    font-size: 23px;
    margin: 0;
    overflow-wrap: anywhere;
  }
  .heading span {
    font-size: 12px;
    text-transform: capitalize;
    color: var(--muted);
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
    max-height: calc(100svh - 32px);
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
    overflow-wrap: anywhere;
  }
  .actions {
    display: flex;
    justify-content: flex-end;
    gap: 12px;
    margin-top: 24px;
  }
  .primary {
    width: auto;
  }
</style>
