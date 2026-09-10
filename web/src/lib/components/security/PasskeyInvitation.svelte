<script lang="ts">
  import { onMount } from "svelte";
  import {
    offerPasskey,
    rememberPasskeyChoice,
    type Flow,
  } from "$lib/security";
  import FlowDialog from "./FlowDialog.svelte";
  let { accountID }: { accountID: string } = $props();
  let invite = $state<HTMLDialogElement>();
  let flowDialog = $state<FlowDialog>();
  const id = $props.id();
  onMount(() => {
    try {
      const pending = sessionStorage.getItem("acta.passkey-offer");
      sessionStorage.removeItem("acta.passkey-offer");
      if (pending === accountID && offerPasskey(accountID)) invite?.showModal();
    } catch {
      /* Browser preference is optional. */
    }
  });
  function dismiss() {
    rememberPasskeyChoice(accountID);
    invite?.close();
  }
  function create() {
    invite?.close();
    void flowDialog?.start("passkey_add");
  }
  function complete(flow: Flow) {
    if (flow.step === "done") rememberPasskeyChoice(accountID);
  }
</script>

<dialog
  bind:this={invite}
  aria-labelledby={id}
  oncancel={(event) => {
    event.preventDefault();
    dismiss();
  }}
>
  <p class="eyebrow">An easier way back</p>
  <h2 {id}>Sign in with a passkey</h2>
  <p class="intro">
    Use your device, password manager or security key the next time you sign in.
  </p>
  <button class="primary" onclick={create}>Create a passkey</button><button
    class="later"
    onclick={dismiss}>Not now</button
  >
</dialog>
<FlowDialog bind:this={flowDialog} onDone={complete} />

<style>
  dialog {
    width: min(440px, calc(100vw - 32px));
    padding: 32px;
    border: 1px solid var(--panel-border);
    border-radius: 14px;
    background: var(--surface);
    color: var(--text);
    box-shadow: var(--shadow);
  }
  dialog::backdrop {
    background: #00000060;
  }
  h2 {
    margin: 0 0 14px;
    font-size: 24px;
    letter-spacing: -0.5px;
  }
  .intro {
    font-size: 14px;
  }
  .later {
    display: block;
    margin: 14px auto 0;
    min-height: 38px;
    background: transparent;
    border: 0;
    color: var(--muted);
    font-size: 13px;
  }
</style>
