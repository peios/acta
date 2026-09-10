<script lang="ts">
  import { onMount } from "svelte";
  import { api, errorMessage, type Account } from "$lib/api";
  import type { ManagedAccount } from "$lib/management";
  let {
    account,
    selected = $bindable(""),
    ready = $bindable(false),
    busy = false,
  }: {
    account: Account;
    selected?: string;
    ready?: boolean;
    busy?: boolean;
  } = $props();
  let agents = $state<ManagedAccount[]>([]),
    error = $state("");
  const id = $props.id();
  const identity = $derived(agents.find((a) => a.id === selected) ?? account);
  async function load() {
    ready = false;
    error = "";
    try {
      agents = (
        await api<{ agents: ManagedAccount[] }>("agents")
      ).agents.filter((a) => a.status === "active");
      ready = true;
    } catch (e) {
      error = errorMessage(e);
    }
  }
  onMount(load);
</script>

<div class="identity">
  {#if error}<p class="notice error" role="alert">{error}</p>
    <button class="secondary" onclick={load}>Try again</button>
  {:else if !ready}<p class="hint" role="status">Loading your identities…</p>
  {:else}<label for={id}>Act as</label>
    <select {id} bind:value={selected} disabled={busy}>
      <option value=""
        >{account.display_name || account.username} (@{account.username}) · You</option
      >
      {#each agents as agent (agent.id)}<option value={agent.id}
          >{agent.display_name || agent.username} (@{agent.username})</option
        >{/each}
    </select>
    <p class="hint">
      This connection will act as <strong>@{identity.username}</strong>.
    </p>
  {/if}
</div>

<style>
  .identity {
    margin: 22px 0;
  }
  label {
    display: block;
    font-size: 13px;
    margin-bottom: 8px;
  }
  select {
    width: 100%;
  }
  .hint {
    margin-top: 8px;
    font-size: 12px;
    color: var(--muted);
    line-height: 1.6;
    overflow-wrap: anywhere;
  }
  strong {
    font-weight: 500;
    color: var(--text);
  }
</style>
