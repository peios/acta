<script lang="ts">
  import { onMount } from "svelte";
  import { goto } from "$app/navigation";
  import { api, APIError, errorMessage, type Account } from "$lib/api";
  import AuthShell from "$lib/components/AuthShell.svelte";
  import SecurityFlow from "$lib/components/security/SecurityFlow.svelte";
  import { beginFlow, cancelFlow, type Flow } from "$lib/security";
  let flow = $state<Flow | null>(null),
    error = $state(""),
    loading = $state(false),
    busy = $state(false),
    browserWaiting = $state(false);
  import { consumeAuthReturn } from "$lib/auth-return";
  const destination = () => consumeAuthReturn("/user-settings/security");
  async function start() {
    loading = true;
    error = "";
    try {
      const account = await api<Account>("account");
      if (!account.mfa_setup_required) {
        await goto(destination(), { replaceState: true });
        return;
      }
      flow = await beginFlow("mfa_setup");
    } catch (e) {
      if (e instanceof APIError && e.status === 401)
        await goto("/login", { replaceState: true });
      else error = errorMessage(e);
    } finally {
      loading = false;
    }
  }
  async function done() {
    flow = null;
    await goto(destination(), { replaceState: true });
  }
  async function logout() {
    if (loading || busy || browserWaiting) return;
    loading = true;
    error = "";
    try {
      await api("logout", {});
      if (flow) void cancelFlow(flow);
      flow = null;
      await goto("/login", { replaceState: true });
    } catch (e) {
      error = errorMessage(e);
    } finally {
      loading = false;
    }
  }
  onMount(start);
</script>

<svelte:head
  ><title>Set up MFA · Acta</title><meta
    name="robots"
    content="noindex"
  /></svelte:head
>
<AuthShell
  title="Secure your account"
  description="Your administrator requires multi-factor authentication. Set up an authenticator to continue to Acta."
>
  {#if error}<p class="notice error" role="alert">{error}</p>{/if}
  {#if flow}{#key flow.id}<SecurityFlow
        initial={flow}
        onDone={done}
        bind:busy
        bind:browserWaiting
      />{/key}
  {:else if loading}<p class="hint" role="status">
      Preparing your authenticator setup…
    </p>
  {:else}<button class="primary" onclick={start}>Set up authenticator</button
    >{/if}
  <button
    class="secondary signout"
    disabled={loading || busy || browserWaiting}
    onclick={logout}>Sign out</button
  >
</AuthShell>

<style>
  .signout {
    width: 100%;
    margin-top: 24px;
  }
</style>
