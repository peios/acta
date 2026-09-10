<script lang="ts">
  import { clearBrowserPush } from "$lib/browser-push.svelte";
  import { LatestRequest } from "$lib/requests.js";
  import { onMount } from "svelte";
  import { goto } from "$app/navigation";
  import {
    api,
    APIError,
    errorMessage,
    type Account,
    type SetupState,
  } from "$lib/api";
  import AppChrome from "$lib/components/chrome/AppChrome.svelte";
  import PasskeyInvitation from "$lib/components/security/PasskeyInvitation.svelte";
  import { provideAccount } from "$lib/account-context";
  let { children } = $props();
  import { provideWorkspace, type Workspace } from "$lib/workspaces";
  let workspace = $state<Workspace | null>(null);
  provideWorkspace({
    get workspace() {
      return workspace;
    },
    update(value) {
      workspace = value;
    },
  });
  let account = $state<Account | null>(null);
  provideAccount({
    get account() {
      return account!;
    },
    update(value) {
      account = value;
    },
  });
  let error = $state("");
  let busy = $state(false);
  const reads = new LatestRequest();
  async function load() {
    const read = reads.begin();
    error = "";
    try {
      const setup = await api<SetupState>("setup", undefined, {
        signal: read.signal,
      });
      if (!read.current()) return;
      if (!setup.complete) {
        await goto("/setup", { replaceState: true });
        return;
      }
      const value = await api<Account>("account", undefined, {
        signal: read.signal,
      });
      if (!read.current()) return;
      if (value.mfa_setup_required) {
        window.dispatchEvent(new Event("acta:mfa-required"));
        return;
      }
      account = value;
    } catch (e) {
      if (!read.current()) return;
      if (e instanceof APIError && e.status === 401)
        await goto("/login", { replaceState: true });
      else error = errorMessage(e);
    }
  }
  onMount(() => {
    void load();
    // Refresh access on tab focus and after a server denial. Backend checks remain
    // authoritative for requests already in flight.
    const refresh = () => {
      void load();
    };
    const expired = () => {
      reads.begin();
      account = null;
      void goto("/login", { replaceState: true });
    };
    window.addEventListener("acta:session-expired", expired);
    window.addEventListener("focus", refresh);
    window.addEventListener("acta:permissions-changed", refresh);
    return () => {
      reads.dispose();
      window.removeEventListener("acta:session-expired", expired);
      window.removeEventListener("focus", refresh);
      window.removeEventListener("acta:permissions-changed", refresh);
    };
  });
  async function logout() {
    if (busy) return;
    busy = true;
    error = "";
    try {
      const signedOutID = account?.id;
      await api("logout", {});
      if (signedOutID) await clearBrowserPush(signedOutID).catch(() => {});
      reads.begin();
      account = null;
      await goto("/login", { replaceState: true });
    } catch (e) {
      error = errorMessage(e);
    } finally {
      busy = false;
    }
  }
</script>

<svelte:head
  >{#if !account}<title>Acta</title>{/if}<meta
    name="robots"
    content="noindex"
  /></svelte:head
>
{#if account}
  <AppChrome {account} onLogout={logout} {busy}>
    {#if error}<p class="notice error" role="alert">{error}</p>{/if}
    {@render children()}
  </AppChrome>
  <PasskeyInvitation accountID={account.id} />
{:else}
  <main class="test-page">
    {#if error}<p class="notice error" role="alert">{error}</p>
      <button class="secondary" onclick={load}>Try again</button>
    {:else}<p class="loading" role="status">Connecting to Acta…</p>{/if}
  </main>
{/if}
