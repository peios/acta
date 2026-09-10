<script lang="ts">
  import { tick } from "svelte";
  import { errorMessage } from "$lib/api";
  import {
    beginFlow,
    cancelFlow,
    flowTitles,
    type Purpose,
    type Flow,
  } from "$lib/security";
  import SecurityFlow from "./SecurityFlow.svelte";
  let {
    onDone,
    onClose = () => {},
  }: { onDone: (flow: Flow) => void; onClose?: () => void } = $props();
  let dialog = $state<HTMLDialogElement>();
  let flow = $state<Flow | null>(null),
    error = $state(""),
    loading = $state(false),
    busy = $state(false),
    browserWaiting = $state(false),
    purpose = $state<Purpose>("passkey_add");
  const id = $props.id();
  export async function start(next: Purpose, target = "", enabled = false) {
    purpose = next;
    error = "";
    flow = null;
    dialog?.showModal();
    loading = true;
    try {
      flow = await beginFlow(next, target, enabled);
      await tick();
      dialog?.querySelector<HTMLElement>("input,form button")?.focus();
    } catch (e) {
      error = errorMessage(e);
    } finally {
      loading = false;
    }
  }
  export async function show(value: Flow) {
    purpose = value.purpose;
    flow = value;
    error = "";
    dialog?.showModal();
    await tick();
    dialog?.querySelector<HTMLElement>("input,form button")?.focus();
  }
  function close() {
    if (loading || (busy && !browserWaiting)) return;
    const pending = flow;
    flow = null;
    busy = false;
    browserWaiting = false;
    dialog?.close();
    if (pending?.id) void cancelFlow(pending).catch(() => {});
    onClose();
  }
  function done(value: Flow) {
    flow = null;
    busy = false;
    browserWaiting = false;
    dialog?.close();
    onDone(value);
  }
</script>

<dialog
  bind:this={dialog}
  class="flow-dialog"
  aria-labelledby={id}
  oncancel={(event) => {
    event.preventDefault();
    close();
  }}
>
  <div class="dialog-heading">
    <h2 {id}>{flowTitles[purpose]}</h2>
    <button
      type="button"
      class="close"
      aria-label="Close"
      disabled={loading || (busy && !browserWaiting)}
      onclick={close}>×</button
    >
  </div>
  {#if error}<p class="notice error" role="alert">
      {error}
    </p>{:else if loading}<p class="loading" role="status">
      Preparing…
    </p>{:else if flow}<SecurityFlow
      initial={flow}
      onDone={done}
      bind:busy
      bind:browserWaiting
    />{/if}
  <button
    type="button"
    class="cancel"
    disabled={loading || (busy && !browserWaiting)}
    onclick={close}>Cancel</button
  >
</dialog>

<style>
  .flow-dialog {
    width: min(460px, calc(100vw - 32px));
    max-height: calc(100svh - 32px);
    padding: 28px;
    color: var(--text);
    background: var(--surface);
    border: 1px solid var(--panel-border);
    border-radius: 14px;
    box-shadow: 0 16px 64px #00000030;
  }
  .flow-dialog::backdrop {
    background: #00000060;
  }
  .dialog-heading {
    display: flex;
    align-items: center;
    gap: 16px;
    justify-content: space-between;
    margin-bottom: 22px;
  }
  h2 {
    font-size: 20px;
    font-weight: 650;
    letter-spacing: -0.4px;
    margin: 0;
  }
  .close {
    background: transparent;
    color: var(--muted);
    border: 0;
    border-radius: 6px;
    min-width: 36px;
    min-height: 36px;
    font-size: 24px;
  }
  .close:hover {
    background: var(--hover-surface);
  }
  .cancel {
    display: block;
    background: transparent;
    border: 0;
    color: var(--muted);
    margin: 16px auto 0;
    min-height: 36px;
    font-size: 13px;
  }
</style>
