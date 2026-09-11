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

{#snippet powerControl(menu: boolean)}
  <button
    class:power-option={menu}
    class:icon-button={!menu}
    aria-label={thread.state === "running" ? "Kill" : "Resume"}
    title={thread.state === "running" ? "Kill" : "Resume"}
    disabled={busy ||
      settingsBusy ||
      !!pending ||
      !thread.connection_id ||
      thread.state === "starting" ||
      thread.state === "killing"}
    onclick={() => {
      if (menu) optionsPanel.hidePopover();
      control(thread.state === "running" ? "kill" : "resume");
    }}
    ><svg viewBox="0 0 24 24" aria-hidden="true"
      ><path d="M12 3v9M6.35 5.65a9 9 0 1 0 11.3 0" /></svg
    >{#if menu}{thread.state === "running" ? "Kill" : "Resume"}{/if}</button
  >
{/snippet}

<header>
  <div class="thread-heading">
    <NavigationToggle />
    <div>
      <div class="title-row">
        <h2 title={threadName(thread)}>{threadName(thread)}</h2>
        <div
          class="thread-state"
          role="status"
          title={`${thread.connection_id ? thread.state : "Harness unavailable"}${thread.committed ? "" : " · Uncommitted"}`}
        >
          <span
            class="status-dot"
            class:online={!!thread.connection_id}
            aria-hidden="true"
          ></span>
          <span class="status-label"
            >{thread.connection_id
              ? thread.state
              : "Harness unavailable"}{#if !thread.committed}
              · Uncommitted{/if}</span
          >
        </div>
      </div>
      <p title={thread.cwd}>
        {thread.provider === "claude" ? "Claude Code" : "Codex"}
        <span>·</span>
        {thread.cwd}
      </p>
    </div>
  </div>
  <div class="thread-actions">
    <div class="desktop-controls">
      {#each usage as gauge (gauge.id)}<ThreadUsageGauge {gauge} />{/each}
      {@render powerControl(false)}
    </div>
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
{#if thread.error || thread.name_sync_pending}<div
    class="thread-notices"
    role="status"
  >
    {#if thread.error}<p>{thread.error}</p>{/if}
    {#if thread.name_sync_pending}<p>
        {thread.name_sync_error ||
          "Name saved · native title will sync on resume"}
      </p>{/if}
  </div>{/if}

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
    height: 420,
    align: "end",
  })}
  onbeforetoggle={(event) => {
    optionsOpen = event.newState === "open";
  }}
>
  <div class="mobile-controls">
    {#if usage.length}<div class="menu-gauges" aria-label="Usage">
        {#each usage as gauge (gauge.id)}<ThreadUsageGauge {gauge} />{/each}
      </div>{/if}
    {@render powerControl(true)}
  </div>
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
    class="debug-option"
    aria-pressed={showDebug}
    onclick={() => (showDebug = !showDebug)}
  >
    <svg viewBox="0 0 24 24" aria-hidden="true"
      >{#if showDebug}<path d="m5 12 4 4L19 6" />{:else}<path
          d="m8 8-4 4 4 4m8-8 4 4-4 4m-3-11-2 22"
        />{/if}</svg
    >
    Show debug frames
  </button>
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
  .debug-option,
  .power-option,
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
  .debug-option:hover,
  .power-option:hover,
  .rename-option:hover,
  .delete-option:hover {
    background: var(--hover-surface);
  }
  .debug-option svg,
  .power-option svg,
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
  .power-option,
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
    margin-bottom: 16px;
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
  .title-row {
    display: flex;
    align-items: baseline;
    gap: 10px;
    min-width: 0;
    margin-bottom: 10px;
  }
  .title-row h2 {
    min-width: 0;
    margin: 0;
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
    display: inline-flex;
    align-items: center;
    gap: 6px;
    color: var(--muted);
    font-size: 12px;
    min-width: 0;
    max-width: 50%;
    flex-shrink: 0;
  }
  .status-label {
    white-space: nowrap;
    overflow: hidden;
    text-overflow: ellipsis;
  }
  .status-dot {
    width: 6px;
    height: 6px;
    flex-shrink: 0;
    border-radius: 50%;
    background: var(--muted);
  }
  .status-dot.online {
    background: #6caa83;
  }
  .thread-notices {
    color: var(--muted);
    font-size: 12px;
    margin: 0 0 16px;
  }
  .thread-notices p {
    margin: 0 0 4px;
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
  .debug-option {
    color: var(--muted);
  }
  .desktop-controls {
    display: flex;
    align-items: center;
    gap: 4px;
    flex-wrap: wrap;
  }
  .mobile-controls {
    display: none;
  }
  @media (max-width: 759px) {
    header {
      align-items: center;
      gap: 8px;
    }
    .thread-heading {
      align-items: center;
      flex: 1;
    }
    h2 {
      white-space: nowrap;
      overflow: hidden;
      text-overflow: ellipsis;
      margin-bottom: 3px;
      font-size: 18px;
    }
    header p {
      white-space: nowrap;
      overflow: hidden;
      text-overflow: ellipsis;
    }
    .thread-actions {
      max-width: none;
    }
    .desktop-controls {
      display: none;
    }
    .mobile-controls {
      display: block;
    }
    .menu-gauges {
      display: flex;
      flex-wrap: wrap;
      justify-content: center;
      gap: 8px;
      padding: 8px 0 12px;
      margin-bottom: 4px;
      border-bottom: 1px solid var(--panel-border);
    }
    .icon-button {
      width: 44px;
      min-height: 44px;
    }
    .power-option,
    .rename-option,
    .debug-option,
    .delete-option {
      min-height: 44px;
    }
    .title-row {
      margin-bottom: 3px;
      gap: 8px;
    }
    .thread-state {
      font-size: 11px;
    }
  }
</style>
