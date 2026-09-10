<script lang="ts">
  import { onMount, tick } from "svelte";
  import { goto } from "$app/navigation";
  import { page } from "$app/state";
  import { api, APIError, errorMessage, type SetupState } from "$lib/api";
  import AuthShell from "$lib/components/AuthShell.svelte";
  import PasswordField from "$lib/components/PasswordField.svelte";
  import SecurityFlow from "$lib/components/security/SecurityFlow.svelte";
  import { beginFlow, cancelFlow, type Flow } from "$lib/security";
  import { authReturn } from "$lib/auth-return";
  const destination = $derived(authReturn(page.url.searchParams.get("return")));
  let flow = $state<Flow | null>(null);
  let flowBusy = $state(false),
    browserWaiting = $state(false);

  let ready = $state(false);
  let username = $state("");
  let password = $state("");
  let busy = $state(false);
  let error = $state("");
  let errorBox = $state<HTMLParagraphElement>();
  async function load() {
    error = "";
    try {
      const state = await api<SetupState>("setup");
      if (!state.complete) {
        await goto("/setup", { replaceState: true });
        return;
      }
      try {
        await api("account");
        await goto(destination, { replaceState: true });
      } catch (e) {
        if (e instanceof APIError && e.status === 401) ready = true;
        else throw e;
      }
    } catch (e) {
      error = errorMessage(e);
    }
  }
  onMount(load);
  async function submit(event: SubmitEvent) {
    event.preventDefault();
    if (busy) return;
    busy = true;
    error = "";
    try {
      const result = await api<Flow>("login", { username, password });
      password = "";
      if (result.step === "done") await complete(result);
      else flow = result;
    } catch (e) {
      error = errorMessage(e);
      await tick();
      errorBox?.focus();
    } finally {
      busy = false;
    }
  }
  async function complete(result: Flow) {
    try {
      sessionStorage.removeItem("acta.passkey-offer");
      if (result.method === "password" && result.account_id)
        sessionStorage.setItem("acta.passkey-offer", result.account_id);
    } catch {}
    flow = null;
    await goto(destination, { replaceState: true });
  }
  async function passkey() {
    if (busy) return;
    busy = true;
    error = "";
    try {
      flow = await beginFlow("login");
    } catch (e) {
      error = errorMessage(e);
    } finally {
      busy = false;
    }
  }
  async function back() {
    if (flowBusy && !browserWaiting) return;
    const current = flow;
    flow = null;
    flowBusy = false;
    browserWaiting = false;
    if (current) await cancelFlow(current).catch(() => {});
  }
</script>

<svelte:head
  ><title>Sign in · Acta</title><meta
    name="robots"
    content="noindex"
  /></svelte:head
>
<AuthShell
  title={flow ? "Verify your sign-in" : "Welcome back"}
  description={flow
    ? "Complete the next step to sign in."
    : "Sign in to your Acta account."}
>
  {#if !ready}
    {#if error}<p class="notice error" role="alert">{error}</p>
      <button class="secondary" onclick={load}>Try again</button>
    {:else}<p class="loading" role="status">Connecting to Acta…</p>{/if}
  {:else if flow}
    <SecurityFlow
      initial={flow}
      onDone={complete}
      bind:busy={flowBusy}
      bind:browserWaiting
    />
    <button class="back" disabled={flowBusy && !browserWaiting} onclick={back}
      >Back to sign in</button
    >
  {:else}
    {#if page.url.searchParams.get("created") === "1"}<p
        class="notice success"
        role="status"
      >
        Your administrator account is ready. Sign in to get started.
      </p>{/if}
    <form onsubmit={submit} aria-busy={busy}>
      {#if error}<p
          class="notice error"
          role="alert"
          tabindex="-1"
          bind:this={errorBox}
        >
          {error}
        </p>{/if}
      <div class="field">
        <label for="username">Username</label>
        <input
          id="username"
          name="username"
          type="text"
          bind:value={username}
          required
          autocomplete="username"
          autocapitalize="none"
          spellcheck="false"
        />
      </div>
      <PasswordField bind:value={password} />
      <button class="primary" type="submit" disabled={busy}
        >{busy ? "Signing in…" : "Sign in"}</button
      >
    </form>
    <div class="divider"><span>or</span></div>
    <button
      class="secondary passkey"
      type="button"
      disabled={busy}
      onclick={passkey}>Sign in with a passkey</button
    >
  {/if}
</AuthShell>

<style>
  .divider {
    display: flex;
    align-items: center;
    gap: 12px;
    margin: 20px 0;
    color: var(--muted);
    font-size: 12px;
  }
  .divider::before,
  .divider::after {
    content: "";
    height: 1px;
    background: var(--panel-border);
    flex: 1;
  }
  .passkey {
    width: 100%;
    font-size: 14px;
  }
  .back {
    display: block;
    margin: 18px auto 0;
    border: 0;
    background: transparent;
    color: var(--muted);
    min-height: 36px;
    font-size: 13px;
  }
</style>
