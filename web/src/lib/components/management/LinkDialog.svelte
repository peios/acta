<script lang="ts">
  import { tick } from "svelte";
  import { api, errorMessage } from "$lib/api";
  import type { ManagedAccount } from "$lib/management";
  let dialog = $state<HTMLDialogElement>(),
    account = $state<ManagedAccount | null>(null),
    url = $state(""),
    busy = $state(false),
    error = $state(""),
    disableMFA = $state(false),
    removePasskeys = $state(false),
    copied = $state(false);
  const id = $props.id();
  export async function open(value: ManagedAccount, created = "") {
    account = value;
    url = created;
    error = "";
    disableMFA = false;
    removePasskeys = false;
    copied = false;
    dialog?.showModal();
    await tick();
    dialog?.querySelector<HTMLInputElement>("input[readonly]")?.focus();
  }
  async function generate() {
    if (!account || busy) return;
    busy = true;
    error = "";
    try {
      const result = await api<{ url: string }>(`users/${account.id}/link`, {
        disable_mfa: disableMFA,
        remove_passkeys: removePasskeys,
      });
      url = result.url;
    } catch (e) {
      error = errorMessage(e);
    } finally {
      busy = false;
      await tick();
      dialog
        ?.querySelector<HTMLElement>(
          error ? '[role="alert"]' : "input[readonly]",
        )
        ?.focus();
    }
  }
  async function copy() {
    try {
      await navigator.clipboard.writeText(url);
      copied = true;
    } catch {
      error = "Select and copy the link from the field below.";
    }
  }
</script>

<dialog
  bind:this={dialog}
  aria-labelledby={id}
  oncancel={(e) => {
    if (busy) e.preventDefault();
  }}
>
  <h2 {id}>{account?.pending ? "Invite user" : "Reset password"}</h2>
  <p class="hint">
    {account?.display_name || account?.username}
    <span>@{account?.username}</span>
  </p>
  {#if error}<p class="notice error" role="alert" tabindex="-1">{error}</p>{/if}
  {#if url}
    <p>
      Share this link privately. It can be used once and expires after 24 hours.
    </p>
    <label for={`${id}-link`}
      >{account?.pending ? "Invitation" : "Recovery"} link</label
    ><input
      id={`${id}-link`}
      value={url}
      readonly
      onclick={(e) => e.currentTarget.select()}
    />
    <button class="secondary" onclick={copy}
      >{copied ? "Copied" : "Copy link"}</button
    >
    <p class="hint">
      {account?.pending
        ? "The account stays pending until its owner sets a password."
        : "Credentials change only when the link is redeemed. All existing sessions are then signed out."}
    </p>
  {:else}
    <p>
      {account?.pending
        ? "Generate a new invitation for this pending account."
        : "Let this user choose a new password using a private recovery link."}
    </p>
    {#if !account?.pending}<label class="choice"
        ><input type="checkbox" bind:checked={disableMFA} disabled={busy} />Also
        disable MFA</label
      ><label class="choice"
        ><input
          type="checkbox"
          bind:checked={removePasskeys}
          disabled={busy}
        />Also remove passkeys</label
      >{/if}
    <p class="hint">
      Any previous invitation or recovery link for this account will stop
      working. No credentials or protection change until redemption.
    </p>
  {/if}
  <div class="actions">
    <button class="secondary" disabled={busy} onclick={() => dialog?.close()}
      >{url ? "Done" : "Cancel"}</button
    >{#if !url}<button class="primary" disabled={busy} onclick={generate}
        >{busy ? "Generating…" : "Generate link"}</button
      >{/if}
  </div>
</dialog>

<style>
  .primary {
    width: auto;
    flex-shrink: 0;
  }
  dialog {
    width: min(480px, calc(100vw - 32px));
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
  h2 {
    margin: 0 0 12px;
    font-size: 21px;
  }
  p {
    font-size: 14px;
    line-height: 1.6;
  }
  .hint {
    font-size: 12px;
  }
  label {
    display: block;
    font-size: 13px;
    margin: 16px 0 8px;
  }
  .choice {
    display: flex;
    align-items: center;
    gap: 10px;
  }
  .choice input {
    width: 18px;
    height: 18px;
  }
  input[readonly] {
    font-family: monospace;
    margin-bottom: 12px;
  }
  .actions {
    display: flex;
    justify-content: flex-end;
    gap: 12px;
    margin-top: 24px;
  }
  .hint span {
    color: var(--muted);
  }
</style>
