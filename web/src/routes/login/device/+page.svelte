<script lang="ts">
  import { onMount } from "svelte";
  import { page } from "$app/state";
  import { goto } from "$app/navigation";
  import AuthShell from "$lib/components/AuthShell.svelte";
  import { api, APIError, errorMessage, type Account } from "$lib/api";
  import { rememberAuthReturn } from "$lib/auth-return";
  import AuthorizationIdentity from "$lib/components/authorization/AuthorizationIdentity.svelte";
  let selected = $state(""),
    identityReady = $state(false);
  let code = $state(""),
    account = $state<Account | null>(null),
    description = $state(""),
    error = $state(""),
    busy = $state(false),
    done = $state("");
  const returnPath = () => "/login/device?code=" + encodeURIComponent(code);
  async function load() {
    busy = true;
    error = "";
    description = "";
    try {
      account = await api<Account>("account");
      if (account.mfa_setup_required) {
        rememberAuthReturn(returnPath());
        await goto("/mfa-required");
        return;
      }
      if (code.trim()) {
        const d = await api<{ description: string }>(
          "device?code=" + encodeURIComponent(code),
        );
        description = d.description;
        const normalized = code.replace(/[-\s]/g, "").toUpperCase();
        code = normalized.slice(0, 4) + "-" + normalized.slice(4);
      }
    } catch (e) {
      if (e instanceof APIError && e.status === 401) {
        await goto("/login?return=" + encodeURIComponent(returnPath()));
      } else {
        error = errorMessage(e);
      }
    } finally {
      busy = false;
    }
  }
  onMount(() => {
    code = page.url.searchParams.get("code") || "";
    void load();
  });
  async function decide(approve: boolean) {
    busy = true;
    error = "";
    try {
      await api("device/approve", { code, approve, account_id: selected });
      done = approve ? "approved" : "denied";
    } catch (e) {
      error = errorMessage(e);
    } finally {
      busy = false;
    }
  }
  async function switchAccount() {
    busy = true;
    try {
      await api("logout", {});
      await goto("/login?return=" + encodeURIComponent(returnPath()));
    } catch (e) {
      error = errorMessage(e);
    } finally {
      busy = false;
    }
  }
</script>

<svelte:head
  ><title>Authorize CLI · Acta</title><meta
    name="robots"
    content="noindex"
  /><meta name="referrer" content="no-referrer" /></svelte:head
>
<AuthShell
  title={done === "approved"
    ? "CLI connected"
    : done === "denied"
      ? "Request declined"
      : "Connect your CLI"}
  description={done
    ? "You can return to your terminal."
    : "Approve the sign-in request from your Acta CLI."}
>
  {#if error}<p class="notice error" role="alert">{error}</p>{/if}
  {#if !done}
    {#if description && account}
      <AuthorizationIdentity
        {account}
        bind:selected
        bind:ready={identityReady}
        {busy}
      />
      <p class="device">{description}</p>
      <p class="code">{code.toUpperCase()}</p>
      <p class="hint">
        Check that this code matches your terminal. Only approve a request you
        started.
      </p>
      <button
        class="primary"
        disabled={busy || !identityReady}
        onclick={() => decide(true)}>Authorize CLI</button
      >
      <button
        class="secondary full"
        disabled={busy}
        onclick={() => decide(false)}>Cancel request</button
      >
      <button class="switch" disabled={busy} onclick={switchAccount}
        >Use another account</button
      >
    {:else}
      <form
        onsubmit={(e) => {
          e.preventDefault();
          void load();
        }}
      >
        <div class="field">
          <label for="device-code">Code from your terminal</label><input
            id="device-code"
            bind:value={code}
            placeholder="ABCD-2345"
            autocomplete="one-time-code"
            autocapitalize="characters"
            spellcheck="false"
            required
            maxlength="20"
          />
        </div>
        <button class="primary" disabled={busy}
          >{busy ? "Connecting…" : "Continue"}</button
        >
      </form>
    {/if}
  {:else}<p class="notice" role="status">
      {done === "approved"
        ? `Your CLI will finish signing in automatically. You can manage its session in User Settings → ${selected ? "Agents" : "Security"}.`
        : "This CLI request was not authorized."}
    </p>{/if}
</AuthShell>

<style>
  .code {
    font-size: 28px;
    letter-spacing: 0.14em;
    font-family: monospace;
    text-align: center;
    padding: 18px;
    border: 1px solid var(--panel-border);
    border-radius: 10px;
  }
  .device,
  .hint {
    color: var(--muted);
    font-size: 14px;
    overflow-wrap: anywhere;
  }
  .hint {
    margin-bottom: 24px;
  }
  .full {
    width: 100%;
    margin-top: 12px;
  }
  .switch {
    display: block;
    margin: 18px auto 0;
    background: transparent;
    border: 0;
    color: var(--muted);
    padding: 10px;
  }
</style>
