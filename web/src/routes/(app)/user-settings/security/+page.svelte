<script lang="ts">
  import { onMount } from "svelte";
  import { api, errorMessage } from "$lib/api";
  import { useAccount } from "$lib/account-context";
  import {
    rememberPasskeyChoice,
    type Flow,
    type Purpose,
    type SecurityState,
  } from "$lib/security";
  import FlowDialog from "$lib/components/security/FlowDialog.svelte";
  import SessionList from "$lib/components/settings/SessionList.svelte";
  const account = useAccount();
  let securityState = $state<SecurityState | null>(null),
    error = $state(""),
    message = $state(""),
    busy = $state(false);
  let dialog = $state<FlowDialog>();
  async function load() {
    try {
      securityState = await api<SecurityState>("security");
      error = "";
    } catch (e) {
      error = errorMessage(e);
    }
  }
  onMount(load);
  async function done(flow: Flow) {
    message =
      flow.purpose === "password"
        ? "Password changed. Other sessions have been signed out."
        : "Security settings updated.";
    error = "";
    if (flow.purpose === "passkey_add")
      rememberPasskeyChoice(account.account.id);
    await load();
  }
  function start(purpose: Purpose, target = "", enabled = false) {
    error = "";
    message = "";
    void dialog?.start(purpose, target, enabled);
  }
  async function revoke(id = "") {
    if (busy) return;
    busy = true;
    error = "";
    message = "";
    try {
      await api("security/revoke", { id });
      message = id ? "Session signed out." : "Other sessions signed out.";
      await load();
    } catch (e) {
      error = errorMessage(e);
    } finally {
      busy = false;
    }
  }
  function date(value: string | null) {
    return value
      ? new Intl.DateTimeFormat(undefined, {
          dateStyle: "medium",
          timeStyle: "short",
        }).format(new Date(value))
      : "Never used";
  }
</script>

<div class="security-page">
  <p class="intro">
    Manage how you sign in and where your account is being used.
  </p>
  {#if error}<p
      id="security-error"
      class="notice error"
      role="alert"
      tabindex="-1"
    >
      {error}
    </p>{/if}
  <p class="status" role="status">{message}</p>
  {#if !securityState}<p class="loading" role="status">
      Loading your security settings…
    </p>
    {#if error}<button class="secondary" onclick={load}>Try again</button>{/if}
  {:else}
    <section aria-labelledby="password-heading">
      <h2 id="password-heading">Password</h2>
      <p class="description">
        Changing your password signs out all other sessions.
      </p>
      <button class="secondary" onclick={() => start("password")}
        >Change password</button
      >
    </section>
    <section aria-labelledby="passkeys-heading">
      <div class="section-heading">
        <h2 id="passkeys-heading">Passkeys</h2>
        <button class="secondary" onclick={() => start("passkey_add")}
          >New passkey</button
        >
      </div>
      <p class="description">
        Sign in using your device, password manager or security key.
      </p>
      {#if securityState.passkeys.length === 0}<p class="empty">
          You haven’t added a passkey yet.
        </p>{:else}<ul class="rows">
          {#each securityState.passkeys as key (key.id)}<li>
              <div class="row-info">
                <strong>{key.name}</strong><span
                  >Added {date(key.created_at)}</span
                ><span
                  >{key.last_used_at
                    ? `Last used ${date(key.last_used_at)}`
                    : "Never used"}</span
                >
              </div>
              <button
                class="text-button"
                aria-label={`Remove ${key.name}`}
                onclick={() => start("passkey_remove", key.id)}>Remove</button
              >
            </li>{/each}
        </ul>{/if}
    </section>
    <section aria-labelledby="authenticator-heading">
      <div class="section-heading">
        <h2 id="authenticator-heading">Authenticator</h2>
        <span class:enabled={securityState.mfa} class="state-label"
          >{securityState.mfa ? "Enabled" : "Not enabled"}</span
        >
      </div>
      <p class="description">
        Use codes from an authenticator app as an additional sign-in step.
      </p>
      {#if account.account.require_mfa}<p class="hint">
          MFA is required for your account.
        </p>{/if}
      {#if !securityState.mfa}<button
          class="secondary"
          onclick={() => start("mfa_setup")}>Set up authenticator</button
        >
      {:else}
        <div class="button-row">
          <button class="secondary" onclick={() => start("mfa_setup")}
            >Replace authenticator</button
          >
          {#if !account.account.require_mfa}<button
              class="text-button"
              onclick={() => start("mfa_disable")}>Disable</button
            >{/if}
        </div>
        <div class="preference">
          <div>
            <h3>Codes after passkey sign-in</h3>
            <p class="hint">
              Require an authenticator code after using a passkey, including
              when confirming security changes.
            </p>
          </div>
          <button
            class="secondary"
            aria-label={securityState.extra_code
              ? "Turn off authenticator codes after passkey sign-in"
              : "Require authenticator codes after passkey sign-in"}
            onclick={() => start("mfa_policy", "", !securityState!.extra_code)}
            >{securityState.extra_code ? "Turn off" : "Turn on"}</button
          >
        </div>
        <div class="recovery">
          <h3>Recovery codes</h3>
          <p class="description">
            {securityState.recovery_remaining} of 10 codes remaining. Each can replace
            one authenticator code.
          </p>
          <button class="secondary" onclick={() => start("recovery")}
            >Generate new codes</button
          >
        </div>
      {/if}
    </section>
    <SessionList
      canMigrate={account.account.permissions.includes("site.superuser")}
      sessions={securityState.sessions}
      {busy}
      onRevoke={revoke}
      onAccessChanged={load}
    />
  {/if}
</div>
<FlowDialog bind:this={dialog} onDone={done} onClose={load} />

<style>
  .security-page {
    width: 100%;
    max-width: 640px;
    padding: 8px 0 40px;
  }
  .intro {
    font-size: 14px;
    margin-bottom: 0;
  }
  .status {
    font-size: 13px;
    color: var(--success-text);
    margin: 12px 0 0;
  }
  .status:empty {
    display: none;
  }
  section {
    padding: 30px 0;
    border-bottom: 1px solid var(--panel-border);
  }
  section:last-child {
    border: 0;
  }
  .section-heading {
    display: flex;
    justify-content: space-between;
    align-items: center;
    gap: 16px;
    flex-wrap: wrap;
  }
  h2 {
    font-size: 16px;
    font-weight: 650;
    margin: 0;
    letter-spacing: -0.2px;
  }
  h3 {
    font-size: 13px;
    font-weight: 600;
    margin: 0 0 8px;
  }
  .description {
    font-size: 13px;
    line-height: 1.65;
    color: var(--muted);
    margin: 10px 0 20px;
  }
  .secondary {
    font-size: 12px;
    min-height: 38px;
    padding: 8px 12px;
  }
  .text-button {
    border: 0;
    background: transparent;
    color: var(--accent);
    font-size: 12px;
    min-height: 38px;
    padding: 8px;
    border-radius: 5px;
    white-space: nowrap;
  }
  .text-button:hover {
    background: var(--hover-surface);
  }
  .empty {
    font-size: 13px;
    color: var(--muted);
    margin: 0;
  }
  .rows {
    list-style: none;
    padding: 0;
    margin: 0;
  }
  .rows li {
    display: flex;
    justify-content: space-between;
    align-items: center;
    gap: 16px;
    padding: 16px 0;
    border-top: 1px solid var(--panel-border);
  }
  .rows li:first-child {
    border-top: 0;
    padding-top: 0;
  }
  .rows li:last-child {
    padding-bottom: 0;
  }
  .row-info {
    display: grid;
    gap: 6px;
    min-width: 0;
  }
  .row-info strong {
    font-size: 13px;
    overflow-wrap: anywhere;
    font-weight: 600;
  }
  .row-info > span {
    color: var(--muted);
    font-size: 12px;
    line-height: 1.5;
  }
  .state-label {
    font-size: 12px;
    color: var(--muted);
  }
  .state-label.enabled {
    color: var(--success-text);
  }
  .button-row {
    display: flex;
    gap: 12px;
    align-items: center;
    flex-wrap: wrap;
  }
  .preference {
    display: flex;
    align-items: center;
    gap: 20px;
    justify-content: space-between;
    margin-top: 28px;
  }
  .preference .secondary {
    flex-shrink: 0;
  }
  .recovery {
    margin-top: 28px;
  }
  .notice {
    margin-top: 20px;
  }
</style>
