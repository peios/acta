<script lang="ts">
  import { onMount } from "svelte";
  import { page } from "$app/state";
  import { goto } from "$app/navigation";
  import AuthShell from "$lib/components/AuthShell.svelte";
  import { api, APIError, errorMessage, type Account } from "$lib/api";
  import { rememberAuthReturn } from "$lib/auth-return";

  import AuthorizationIdentity from "$lib/components/authorization/AuthorizationIdentity.svelte";
  let migration = $state(false);
  let readMemories = $state(true),
    writeMemories = $state(true);
  let readTasks = $state(true),
    writeTasks = $state(true);
  let selected = $state(""),
    identityReady = $state(false);
  let account = $state<Account | null>(null);
  let clientName = $state("");
  let error = $state("");
  let loading = $state(true);
  let busy = $state(false);
  let decision = $state<boolean | null>(null);
  const request = $derived(page.url.searchParams.get("request") || "");
  const returnPath = () =>
    "/login/oauth?request=" + encodeURIComponent(request);
  const loginPath = () => "/login?return=" + encodeURIComponent(returnPath());
  async function load() {
    loading = true;
    error = "";
    try {
      account = await api<Account>("account");
      if (account.mfa_setup_required) {
        rememberAuthReturn(returnPath());
        await goto("/mfa-required");
        return;
      }
      const consent = await api<{ client_name: string }>(
        "oauth/consent?request=" + encodeURIComponent(request),
      );
      clientName = consent.client_name;
    } catch (e) {
      if (e instanceof APIError && e.status === 401) await goto(loginPath());
      else error = errorMessage(e);
    } finally {
      loading = false;
    }
  }
  async function decide(approve: boolean) {
    if (busy) return;
    busy = true;
    error = "";
    try {
      const result = await api<{ redirect: string }>("oauth/approve", {
        request,
        approve,
        account_id: selected,
        tools: [
          "identity.read",
          ...(readMemories ? ["memories.read"] : []),
          ...(writeMemories ? ["memories.write"] : []),
          ...(readTasks ? ["tasks.read"] : []),
          ...(writeTasks ? ["tasks.write"] : []),
          ...(migration ? ["migration.assistant"] : []),
        ],
      });
      decision = approve;
      window.location.assign(result.redirect);
    } catch (e) {
      error = errorMessage(e);
      busy = false;
    }
  }
  async function switchAccount() {
    busy = true;
    error = "";
    try {
      await api("logout", {});
      await goto(loginPath());
    } catch (e) {
      error = errorMessage(e);
    } finally {
      busy = false;
    }
  }
  onMount(load);
</script>

<svelte:head>
  <title>Connect to Acta · Acta</title>
  <meta name="robots" content="noindex" />
  <meta name="referrer" content="no-referrer" />
</svelte:head>

<AuthShell
  title={decision === true
    ? "Connection authorized"
    : decision === false
      ? "Request declined"
      : "Connect to Acta"}
  description={decision === null
    ? "Choose whether to give this client access to your account."
    : "You can return to your MCP client."}
>
  {#if error}<p class="notice error" role="alert">{error}</p>{/if}
  {#if decision !== null}
    <p class="notice" role="status">
      {decision
        ? `This client has been authorized to use the selected tools within the account’s permissions. You can revoke its connection in User Settings → ${selected ? "Agents" : "Security"}.`
        : "This connection request was not authorized."}
    </p>
  {:else if loading}
    <p class="hint" role="status">Loading connection request…</p>
  {:else if clientName && account}
    <div class="client">
      <strong>{clientName}</strong>
      <span>Name provided by the client</span>
    </div>
    <AuthorizationIdentity
      {account}
      bind:selected
      bind:ready={identityReady}
      {busy}
    />
    <div class="access">
      <h2>Tools this connection can use</h2>
      <p>Read the selected account’s ID, username and display name.</p>
      <label
        ><input type="checkbox" bind:checked={readTasks} disabled={busy} /> Read tasks,
        documents, workspaces, statuses and assignees</label
      >
      <label
        ><input type="checkbox" bind:checked={writeTasks} disabled={busy} /> Create
        and edit tasks; upload and delete task documents</label
      >
      <label
        ><input type="checkbox" bind:checked={readMemories} disabled={busy} /> Read
        accessible memories</label
      >
      <label
        ><input type="checkbox" bind:checked={writeMemories} disabled={busy} /> Create,
        edit and delete memories within the account’s permissions</label
      >
    </div>
    {#if account?.permissions.includes("site.superuser")}
      <div class="access migration-access">
        <label
          ><input
            type="checkbox"
            bind:checked={migration}
            disabled={busy}
          />Migration Assistant</label
        >
        <p>
          Full content impersonation: create and edit tasks, comments, memories,
          documents and historical activity as any user or agent, including
          protected dates and task references. Notifications are suppressed; the
          actual operator is recorded. Cannot grant permissions or access
          credentials.
        </p>
      </div>
    {/if}
    <p class="hint">
      You can revoke this connection in User Settings → {selected
        ? "Agents"
        : "Security"}. New tools will need your approval.
    </p>
    <button
      class="primary"
      disabled={busy || !identityReady}
      onclick={() => decide(true)}
      >{busy ? "Please wait…" : "Authorize connection"}</button
    >
    <button class="secondary full" disabled={busy} onclick={() => decide(false)}
      >Cancel</button
    >
    <button class="switch" disabled={busy} onclick={switchAccount}
      >Use another account</button
    >
  {:else}
    <p class="hint">
      Return to your MCP client to start a new connection request.
    </p>
  {/if}
</AuthShell>

<style>
  .migration-access {
    margin-top: 16px;
  }
  .access label {
    display: flex;
    align-items: center;
    gap: 10px;
    margin-top: 14px;
  }
  .access input {
    width: 16px;
    height: 16px;
    min-height: 0;
    padding: 0;
    flex-shrink: 0;
  }
  .client {
    padding-bottom: 22px;
    border-bottom: 1px solid var(--panel-border);
    overflow-wrap: anywhere;
  }
  .client strong {
    display: block;
    font-size: 19px;
    margin-bottom: 5px;
  }
  .client span,
  .hint,
  .access p {
    color: var(--muted);
    font-size: 13px;
    line-height: 1.6;
  }
  .access {
    border: 1px solid var(--panel-border);
    border-radius: 10px;
    padding: 16px;
  }
  .access h2 {
    font-size: 14px;
    margin: 0 0 6px;
  }
  .access p {
    margin: 0;
  }
  .hint {
    margin: 18px 0 24px;
  }
  .full {
    width: 100%;
    margin-top: 12px;
  }
  .switch {
    display: block;
    margin: 18px auto 0;
    padding: 10px;
    border: 0;
    background: transparent;
    color: var(--muted);
    font-size: 13px;
  }
</style>
