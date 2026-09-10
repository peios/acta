<script lang="ts">
  import { onMount, tick } from "svelte";
  import { goto } from "$app/navigation";
  import { api, APIError, errorMessage } from "$lib/api";
  import { useAccount } from "$lib/account-context";
  import type { ManagedAccount } from "$lib/management";
  const owner = useAccount();
  let agents = $state<ManagedAccount[]>([]),
    loading = $state(true),
    error = $state("");
  let dialog = $state<HTMLDialogElement>(),
    username = $state(""),
    displayName = $state(""),
    busy = $state(false),
    createError = $state("");
  let fields = $state<Record<string, string>>({});
  async function load() {
    loading = true;
    error = "";
    try {
      agents = (await api<{ agents: ManagedAccount[] }>("agents")).agents;
    } catch (e) {
      error = errorMessage(e);
    } finally {
      loading = false;
    }
  }
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
      const agent = await api<ManagedAccount>("agents", {
        username,
        display_name: displayName,
      });
      dialog?.close();
      await goto(`/user-settings/agents/${agent.id}?created=1`);
    } catch (e) {
      createError = errorMessage(e);
      fields = e instanceof APIError ? e.fields : {};
      await tick();
      dialog
        ?.querySelector<HTMLElement>('[aria-invalid="true"], [role="alert"]')
        ?.focus();
    } finally {
      busy = false;
    }
  }
  onMount(load);
</script>

<div class="agents-page">
  <div class="heading">
    <p>Manage the agents that act on your behalf.</p>
    <button class="primary" onclick={create}>Create agent</button>
  </div>
  {#if error}<p class="notice error" role="alert">{error}</p>
    <button class="secondary" onclick={load}>Try again</button>{/if}
  {#if loading}<p class="hint" role="status">Loading agents…</p>
  {:else if !error && !agents.length}<div class="empty">
      <h2>Your agents live here</h2>
      <p>
        Create an identity, choose its permissions, then authorize a connection
        to act as it.
      </p>
    </div>{/if}
  <ul aria-label="Agents">
    {#each agents as agent (agent.id)}<li>
        <a href={`/user-settings/agents/${agent.id}`}
          ><span class="avatar" aria-hidden="true"
            >{Array.from(
              agent.display_name || agent.username_segment || agent.username,
            )[0]?.toUpperCase()}</span
          ><span class="identity"
            ><strong>{agent.display_name || agent.username_segment}</strong
            ><span>@{agent.username}</span></span
          ><span class="status">{agent.status}</span><span aria-hidden="true"
            >›</span
          ></a
        >
      </li>{/each}
  </ul>
</div>
<dialog
  bind:this={dialog}
  aria-labelledby="create-agent-title"
  oncancel={(e) => {
    if (busy) e.preventDefault();
  }}
>
  <h2 id="create-agent-title">Create agent</h2>
  <p>An identity you manage, with permissions you choose.</p>
  <form onsubmit={submit}>
    {#if createError}<p class="notice error" role="alert" tabindex="-1">
        {createError}
      </p>{/if}
    <div class="field">
      <label for="agent-username">Username</label>
      <div class="username">
        <span>@{owner.account.username}/</span><input
          id="agent-username"
          bind:value={username}
          required
          maxlength="32"
          autocomplete="off"
          autocapitalize="none"
          spellcheck="false"
          aria-invalid={!!fields.username}
          aria-describedby="agent-username-hint"
          readonly={busy}
        />
      </div>
      <p id="agent-username-hint" class="hint">
        {fields.username ||
          "1–32 letters or numbers, with dots, hyphens or underscores between them."}
      </p>
    </div>
    <div class="field">
      <label for="agent-display"
        >Display name <span class="hint">(optional)</span></label
      ><input
        id="agent-display"
        bind:value={displayName}
        aria-invalid={!!fields.display_name}
        aria-describedby="agent-display-hint"
        readonly={busy}
      />
      <p id="agent-display-hint" class="hint">
        {fields.display_name || "Leave blank to use the username."}
      </p>
    </div>
    <p class="hint">
      Your agent starts with no permissions. You’ll choose its access next.
    </p>
    <div class="actions">
      <button
        type="button"
        class="secondary"
        disabled={busy}
        onclick={() => dialog?.close()}>Cancel</button
      ><button class="primary" disabled={busy}
        >{busy ? "Creating…" : "Create agent"}</button
      >
    </div>
  </form>
</dialog>

<style>
  .agents-page {
    max-width: 800px;
    width: 100%;
    padding: 8px 0 40px;
  }
  .heading {
    display: flex;
    align-items: center;
    justify-content: space-between;
    gap: 24px;
    margin-bottom: 28px;
    flex-wrap: wrap;
  }
  .heading p {
    font-size: 14px;
    color: var(--muted);
    margin: 0;
  }
  .primary {
    width: auto;
    white-space: nowrap;
  }
  ul {
    list-style: none;
    padding: 0;
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
    transition: background 140ms;
  }
  li a:hover {
    background: var(--hover-surface);
  }
  .avatar {
    display: grid;
    place-items: center;
    width: 38px;
    height: 38px;
    flex-shrink: 0;
    background: var(--hover-surface);
    border: 1px solid var(--panel-border);
    border-radius: 10px;
    font-size: 14px;
  }
  .identity {
    display: flex;
    flex-direction: column;
    gap: 5px;
    flex: 1;
    min-width: 0;
    overflow-wrap: anywhere;
  }
  .identity strong {
    font-size: 14px;
    font-weight: 550;
  }
  .identity > span,
  .status {
    font-size: 12px;
    color: var(--muted);
  }
  .status {
    text-transform: capitalize;
  }
  .empty {
    padding: 44px 28px;
    border: 1px dashed var(--panel-border);
    border-radius: 12px;
    text-align: center;
  }
  .empty h2 {
    font-size: 17px;
    margin: 0 0 12px;
  }
  .empty p {
    font-size: 13px;
    color: var(--muted);
    line-height: 1.7;
    margin: 0 auto;
    max-width: 370px;
  }
  dialog {
    width: min(460px, calc(100vw - 32px));
    max-height: calc(100svh - 32px);
    padding: 28px;
    background: var(--surface);
    color: var(--text);
    border: 1px solid var(--panel-border);
    border-radius: 14px;
  }
  dialog::backdrop {
    background: #00000060;
  }
  dialog h2 {
    font-size: 21px;
    margin: 0 0 12px;
  }
  dialog > p {
    font-size: 13px;
    line-height: 1.6;
    color: var(--muted);
    margin-bottom: 24px;
  }
  .username {
    display: flex;
    align-items: center;
    border: 1px solid var(--border);
    border-radius: 8px;
    padding-left: 13px;
    background: var(--input-bg);
  }
  .username span {
    font-size: 13px;
    color: var(--muted);
    white-space: nowrap;
  }
  .username input {
    border: 0;
    min-width: 0;
  }
  .actions {
    display: flex;
    justify-content: flex-end;
    gap: 12px;
    margin-top: 8px;
  }
  @media (prefers-reduced-motion: reduce) {
    li a {
      transition: none;
    }
  }
</style>
