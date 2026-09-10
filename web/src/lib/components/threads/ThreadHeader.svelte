<script lang="ts">
  import type { Thread } from "$lib/threads.svelte";
  import NavigationToggle from "$lib/components/chrome/NavigationToggle.svelte";
  import ThreadUsageGauge from "$lib/components/threads/ThreadUsageGauge.svelte";
  import { anchoredPopover } from "$lib/anchored-popover";
  import { threadName } from "$lib/threads.svelte";
  import type { threadUsage } from "$lib/thread-usage.js";
  const optionsId = $props.id();
  let optionsTrigger = $state<HTMLButtonElement>();
  let optionsOpen = $state(false);
  let optionsPanel: HTMLDivElement;
  let renameDialog: HTMLDialogElement;
  let renameInput: HTMLInputElement;
  let name = $state("");
  let renaming = $state(false);
  let renameError = $state("");
  async function rename() {
    renaming = true;
    renameError = "";
    try {
      await onrename(name.trim());
      renameDialog.close();
    } catch (e) {
      renameError = e instanceof Error ? e.message : "Could not rename thread.";
    } finally {
      renaming = false;
    }
  }
  let deleteDialog: HTMLDialogElement;
  let deleting = $state(false);
  let deleteError = $state("");
  async function remove() {
    deleting = true;
    deleteError = "";
    try {
      await ondelete();
      deleteDialog.close();
    } catch (e) {
      deleteError = e instanceof Error ? e.message : "Could not delete thread.";
    } finally {
      deleting = false;
    }
  }
  let {
    thread,
    usage,
    showDebug = $bindable(false),
    busy,
    settingsBusy,
    pending,
    control,
    ondelete,
    onrename,
  }: {
    thread: Thread;
    usage: ReturnType<typeof threadUsage>;
    showDebug: boolean;
    busy: boolean;
    settingsBusy: boolean;
    pending: boolean;
    control: (action: "kill" | "resume") => void;
    onrename: (name: string) => Promise<void>;
    ondelete: () => Promise<void>;
  } = $props();
</script>

<header>
  <div class="thread-heading">
    <NavigationToggle />
    <div>
      <h2>{threadName(thread)}</h2>
      <p title={thread.cwd}>
        {thread.provider === "claude" ? "Claude Code" : "Codex"}
        <span>·</span>
        {thread.cwd}
      </p>
    </div>
  </div>
  <div class="thread-actions">
    {#each usage as gauge (gauge.id)}<ThreadUsageGauge {gauge} />{/each}
    <button
      class="icon-button"
      aria-label={thread.state === "running" ? "Kill" : "Resume"}
      title={thread.state === "running" ? "Kill" : "Resume"}
      disabled={busy ||
        settingsBusy ||
        !!pending ||
        !thread.connection_id ||
        thread.state === "starting" ||
        thread.state === "killing"}
      onclick={() => control(thread.state === "running" ? "kill" : "resume")}
      ><svg viewBox="0 0 24 24" aria-hidden="true"
        ><path d="M12 3v9M6.35 5.65a9 9 0 1 0 11.3 0" /></svg
      ></button
    >
    <button
      class="icon-button"
      bind:this={optionsTrigger}
      popovertarget={optionsId}
      aria-label="Thread options"
      title="Thread options"
      aria-haspopup="dialog"
      aria-expanded={optionsOpen}
      ><svg viewBox="0 0 24 24" aria-hidden="true"
        ><circle cx="12" cy="5" r="1" /><circle cx="12" cy="12" r="1" /><circle
          cx="12"
          cy="19"
          r="1"
        /></svg
      ></button
    >
  </div>
</header>
<div class="thread-state" role="status">
  <span class:online={!!thread.connection_id}></span>{thread.connection_id
    ? thread.state
    : "Harness unavailable"}{#if !thread.committed}<span>Uncommitted</span
    >{/if}{#if thread.error}<p>{thread.error}</p>{/if}
  {#if thread.name_sync_pending}<p>
      {thread.name_sync_error ||
        "Name saved · native title will sync on resume"}
    </p>{/if}
</div>

<div
  bind:this={optionsPanel}
  id={optionsId}
  class="thread-options"
  popover="auto"
  role="dialog"
  aria-label="Thread options"
  use:anchoredPopover={() => ({
    anchor: optionsTrigger,
    width: 240,
    height: 164,
    align: "end",
  })}
  onbeforetoggle={(event) => {
    optionsOpen = event.newState === "open";
  }}
>
  <label
    ><input type="checkbox" role="switch" bind:checked={showDebug} />Show debug
    frames</label
  >
  <button
    class="rename-option"
    disabled={busy || settingsBusy || pending || !thread.connection_id}
    title={!thread.connection_id
      ? "Connect the harness to rename this thread"
      : undefined}
    onclick={() => {
      optionsPanel.hidePopover();
      name = threadName(thread);
      renameError = "";
      renameDialog.showModal();
      renameInput.focus();
      renameInput.select();
    }}
    ><svg viewBox="0 0 24 24" aria-hidden="true"
      ><path d="m15 4 5 5M4 20l5-1L20 8a2 2 0 0 0-5-5L4 14z" /></svg
    >Rename thread</button
  >
  <button
    class="delete-option"
    onclick={() => {
      optionsPanel.hidePopover();
      deleteError = "";
      deleteDialog.showModal();
    }}
    ><svg viewBox="0 0 24 24" aria-hidden="true"
      ><path d="M3 6h18M9 6V3h6v3M5 6l1 15h12l1-15M10 10v7m4-7v7" /></svg
    >Delete thread</button
  >
</div>

<dialog
  bind:this={renameDialog}
  class="management-dialog"
  aria-label="Rename thread"
  oncancel={(e) => {
    if (renaming) e.preventDefault();
  }}
>
  <form
    onsubmit={(e) => {
      e.preventDefault();
      void rename();
    }}
  >
    <h2>Rename thread</h2>
    <label class="rename-label" for={`${optionsId}-name`}>Name</label>
    <input
      bind:this={renameInput}
      id={`${optionsId}-name`}
      bind:value={name}
      required
      maxlength="120"
      disabled={renaming}
      autocomplete="off"
    />
    {#if renameError}<p class="notice error" role="alert">{renameError}</p>{/if}
    <div class="management-actions">
      <button
        type="button"
        class="secondary"
        disabled={renaming}
        onclick={() => renameDialog.close()}>Cancel</button
      >
      <button type="submit" class="primary" disabled={renaming || !name.trim()}
        >{renaming ? "Renaming…" : "Rename"}</button
      >
    </div>
  </form>
</dialog>

<dialog
  bind:this={deleteDialog}
  class="management-dialog"
  aria-label="Delete thread"
  oncancel={(e) => {
    if (deleting) e.preventDefault();
  }}
>
  <h2>Delete {threadName(thread)}?</h2>
  <p class="hint">
    This permanently deletes this thread and its history from Acta. Its provider
    session and files on your development machine are kept. A running session
    will keep running.
  </p>
  {#if deleteError}<p class="notice error" role="alert">{deleteError}</p>{/if}
  <div class="management-actions">
    <button
      class="secondary"
      disabled={deleting}
      onclick={() => deleteDialog.close()}>Cancel</button
    >
    <button
      class="secondary delete-confirm"
      disabled={deleting}
      onclick={() => void remove()}
      >{deleting ? "Deleting…" : "Delete thread"}</button
    >
  </div>
</dialog>

<style>
  .rename-option,
  .delete-option {
    display: flex;
    align-items: center;
    gap: 10px;
    width: 100%;
    padding: 9px 8px;
    border: 0;
    border-radius: 6px;
    background: transparent;
    color: var(--danger);
    font-size: 13px;
    text-align: left;
  }
  .rename-option:hover,
  .delete-option:hover {
    background: var(--hover-surface);
  }
  .rename-option svg,
  .delete-option svg {
    width: 17px;
    height: 17px;
    fill: none;
    stroke: currentColor;
    stroke-width: 1.6;
    stroke-linecap: round;
    stroke-linejoin: round;
  }
  .rename-option {
    color: var(--text);
  }
  .rename-label {
    display: block;
    margin-bottom: 8px;
    color: var(--muted);
    font-size: 13px;
  }
  h2 {
    overflow-wrap: anywhere;
  }
  .delete-confirm {
    color: var(--danger);
    border-color: color-mix(in srgb, var(--danger) 35%, transparent);
    background: color-mix(in srgb, var(--danger) 12%, var(--surface));
  }

  header {
    flex-shrink: 0;
    display: flex;
    align-items: flex-start;
    justify-content: space-between;
    gap: 20px;
  }
  header > div {
    min-width: 0;
  }
  .thread-heading {
    display: flex;
    align-items: flex-start;
    gap: 10px;
  }
  .thread-heading > div {
    min-width: 0;
  }
  h2 {
    font-size: 20px;
    font-weight: 550;
    margin: 0 0 10px;
  }
  header p {
    font-size: 12px;
    color: var(--muted);
    margin: 0;
    overflow-wrap: anywhere;
  }
  header p span {
    padding: 0 6px;
  }
  .thread-actions {
    display: flex;
    align-items: center;
    flex-wrap: wrap;
    justify-content: flex-end;
    max-width: 65%;
    gap: 4px;
    flex-shrink: 0;
  }
  .icon-button {
    display: inline-flex;
    align-items: center;
    justify-content: center;
    width: 36px;
    min-height: 36px;
    padding: 0;
    border: 0;
    border-radius: 8px;
    background: transparent;
    color: var(--muted);
    transition:
      background 140ms,
      color 140ms;
  }
  .icon-button:hover {
    background: var(--hover-surface);
    color: var(--text);
  }
  .icon-button svg {
    width: 19px;
    height: 19px;
    fill: none;
    stroke: currentColor;
    stroke-width: 1.7;
    stroke-linecap: round;
  }
  .thread-state {
    display: flex;
    align-items: center;
    gap: 8px;
    color: var(--muted);
    font-size: 12px;
    margin: 18px 0 28px;
    flex-wrap: wrap;
  }
  .thread-state > span {
    width: 6px;
    height: 6px;
    border-radius: 50%;
    background: var(--muted);
  }
  .thread-state .online {
    background: #6caa83;
  }
  .thread-state p {
    flex-basis: 100%;
    margin: 4px 0 0;
  }
  .thread-options {
    position: fixed;
    inset: auto;
    margin: 0;
    padding: 8px;
    border: 1px solid var(--panel-border);
    border-radius: 12px;
    background: var(--surface);
    color: var(--text);
    box-shadow: 0 12px 36px #0003;
  }
  .thread-options label {
    display: flex;
    align-items: center;
    gap: 10px;
    padding: 8px;
    font-size: 13px;
    cursor: pointer;
    border-radius: 6px;
  }
  .thread-options label:hover {
    background: var(--hover-surface);
  }
</style>
