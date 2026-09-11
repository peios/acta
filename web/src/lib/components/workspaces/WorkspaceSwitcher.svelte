<script lang="ts">
  import { onDestroy, tick } from "svelte";
  import { api, errorMessage } from "$lib/api";
  import { useAccount } from "$lib/account-context";
  import { checkPermission } from "$lib/permissions";
  import { anchoredPopover } from "$lib/anchored-popover";
  import { useWorkspace, workspacePath, type Workspace } from "$lib/workspaces";
  import CreateWorkspaceDialog from "./CreateWorkspaceDialog.svelte";
  import "$lib/components/management/management.css";
  const current = useWorkspace(),
    account = useAccount(),
    id = $props.id();
  let dialog: HTMLDialogElement,
    dropdown: HTMLDivElement,
    trigger: HTMLButtonElement;
  let create = $state<CreateWorkspaceDialog>(),
    list = $state<Workspace[]>([]),
    query = $state(""),
    error = $state(""),
    loading = $state(false),
    more = $state(false),
    offset = $state(0),
    active = $state(0),
    mode = $state<"mobile" | "desktop" | null>(null);
  let generation = 0,
    timer: ReturnType<typeof setTimeout> | undefined;
  const panel = () => (mode === "mobile" ? dialog : dropdown);
  async function load(reset = true) {
    const g = ++generation;
    loading = true;
    error = "";
    const start = reset ? 0 : offset;
    try {
      const r = await api<{ workspaces: Workspace[]; more: boolean }>(
        `workspaces?q=${encodeURIComponent(query)}&offset=${start}`,
      );
      if (g !== generation) return;
      list = reset ? r.workspaces : [...list, ...r.workspaces];
      offset = start + r.workspaces.length;
      more = r.more;
    } catch (e) {
      if (g === generation) error = errorMessage(e);
    } finally {
      if (g === generation) loading = false;
    }
  }
  function invalidate() {
    clearTimeout(timer);
    generation++;
    mode = null;
  }
  function close() {
    if (dialog.open) dialog.close();
    if (dropdown.matches(":popover-open")) dropdown.hidePopover();
    invalidate();
    trigger.focus({ preventScroll: true });
  }
  function open(event: MouseEvent) {
    const mobile = window.matchMedia("(max-width: 759px)").matches;
    if (mobile) event.preventDefault();
    if (mode) {
      if (mobile) close();
      return;
    }
    query = "";
    list = [];
    more = false;
    active = 0;
    offset = 0;
    mode = mobile ? "mobile" : "desktop";
    if (mobile) {
      dialog.showModal();
      // Focus during the tap so iOS can open its software keyboard.
      dialog.querySelector("input")?.focus({ preventScroll: true });
    }
    // Desktop uses the native invoker so clicking it again closes the popup.
    void load();
  }
  function search() {
    clearTimeout(timer);
    generation++;
    list = [];
    more = false;
    active = 0;
    offset = 0;
    error = "";
    loading = true;
    timer = setTimeout(() => void load(), 200);
  }
  async function keys(e: KeyboardEvent) {
    if (e.isComposing) return;
    if (e.key === "ArrowDown" || e.key === "ArrowUp") {
      e.preventDefault();
      if (!list.length) return;
      active =
        (active + (e.key === "ArrowDown" ? 1 : -1) + list.length) % list.length;
      await tick();
      panel()
        .querySelector(`[data-index="${active}"]`)
        ?.scrollIntoView({ block: "nearest" });
    } else if (e.key === "Enter") {
      e.preventDefault();
      if (!loading)
        panel()
          .querySelector<HTMLAnchorElement>(`[data-index="${active}"]`)
          ?.click();
    }
  }
  onDestroy(invalidate);
</script>

<button
  class="switcher"
  bind:this={trigger}
  popovertarget={`${id}-dropdown`}
  aria-label="Switch workspace"
  aria-haspopup="dialog"
  aria-expanded={mode !== null}
  title={current.workspace?.name ?? "Workspaces"}
  onclick={open}
>
  <span>{current.workspace?.name ?? "Workspaces"}</span><span aria-hidden="true"
    >⌄</span
  >
</button>

{#snippet picker(prefix: string)}
  <div class="search-input">
    <svg
      width="20"
      height="20"
      viewBox="0 0 24 24"
      fill="none"
      stroke="currentColor"
      stroke-width="1.6"
      aria-hidden="true"
      ><circle cx="10.5" cy="10.5" r="6.5" /><path d="m16 16 5 5" /></svg
    >
    <input
      bind:value={query}
      oninput={search}
      onkeydown={keys}
      placeholder="Find a workspace…"
      aria-label="Search workspaces"
      role="combobox"
      aria-autocomplete="list"
      aria-expanded={mode !== null}
      aria-controls={`${prefix}-results`}
      aria-activedescendant={list[active]
        ? `${prefix}-result-${active}`
        : undefined}
    />
    <button class="close" aria-label="Close workspace selector" onclick={close}>
      <svg
        width="20"
        height="20"
        viewBox="0 0 24 24"
        fill="none"
        stroke="currentColor"
        stroke-width="1.8"
        aria-hidden="true"><path d="m6 6 12 12M18 6 6 18" /></svg
      >
    </button>
  </div>
  <div class="search-body">
    <div
      id={`${prefix}-results`}
      role="listbox"
      aria-label="Workspaces"
      aria-busy={loading}
    >
      {#each list as w, i (w.id)}
        <a
          href={workspacePath(w)}
          id={`${prefix}-result-${i}`}
          data-index={i}
          role="option"
          aria-selected={active === i}
          class:active={active === i}
          aria-current={current.workspace?.id === w.id ? "page" : undefined}
          onclick={close}
          onpointermove={() => (active = i)}
        >
          <span class="identity"
            ><strong>{w.name}</strong><span>{w.slug}</span></span
          >
          {#if current.workspace?.id === w.id}<svg
              class="current"
              width="18"
              height="18"
              viewBox="0 0 24 24"
              fill="none"
              stroke="currentColor"
              stroke-width="1.8"
              aria-label="Current workspace"><path d="m5 12 4 4L19 6" /></svg
            >{/if}
        </a>
      {/each}
    </div>
    {#if error}<p class="error" role="alert">
        {error}<button onclick={() => load(offset === 0)}>Try again</button>
      </p>
    {:else if loading}<p class="empty" role="status">Loading workspaces…</p>
    {:else if !list.length}<p class="empty" role="status">
        No accessible workspaces found.
      </p>{/if}
    {#if more}<button
        class="more"
        disabled={loading}
        onclick={() => load(false)}>Load more</button
      >{/if}
  </div>
  {#if checkPermission(account.account, "site.workspaces.create") && !account.account.owner_id}
    <button
      class="create"
      onclick={() => {
        close();
        create?.open();
      }}>＋ Create workspace</button
    >
  {/if}
{/snippet}

<div
  bind:this={dropdown}
  id={`${id}-dropdown`}
  class="workspace-dropdown"
  popover="auto"
  role="dialog"
  aria-label="Switch workspace"
  use:anchoredPopover={() => ({ anchor: trigger, width: 320, height: 460 })}
  ontoggle={(e) => {
    if (e.newState === "open")
      dropdown.querySelector("input")?.focus({ preventScroll: true });
  }}
  onbeforetoggle={(e) => {
    if (e.newState === "closed") invalidate();
  }}
>
  {@render picker(`${id}-desktop`)}
</div>
<dialog
  bind:this={dialog}
  class="workspace-search"
  aria-label="Switch workspace"
  oncancel={(e) => {
    e.preventDefault();
    close();
  }}
>
  <h2>Switch workspace</h2>
  {@render picker(`${id}-mobile`)}
</dialog>
<CreateWorkspaceDialog bind:this={create} />

<style>
  .switcher {
    display: flex;
    align-items: center;
    gap: 9px;
    min-width: 0;
    flex: 1;
    background: transparent;
    border: 0;
    border-radius: 7px;
    color: var(--text);
    padding: 8px 4px;
    text-align: left;
    font-size: 14px;
    font-weight: 600;
  }
  .switcher:hover {
    background: var(--hover-surface);
  }
  .switcher span:first-of-type {
    flex: 1;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }
  .workspace-dropdown,
  .workspace-search {
    padding: 0;
    color: var(--text);
    background: var(--surface);
    border: 1px solid var(--panel-border);
    border-radius: 12px;
    box-shadow: 0 20px 60px #0004;
  }
  .workspace-dropdown {
    position: fixed;
    inset: auto;
    margin: 0;
    overflow: auto;
  }
  .workspace-dropdown:popover-open {
    display: flex;
    flex-direction: column;
  }
  .search-input {
    display: flex;
    flex-shrink: 0;
    align-items: center;
    gap: 10px;
    padding: 10px 12px;
    border-bottom: 1px solid var(--panel-border);
    color: var(--muted);
  }
  input {
    min-width: 0;
    flex: 1;
    width: 100%;
    border: 0;
    outline: 0;
    box-shadow: none;
    background: transparent;
    color: var(--text);
    font: inherit;
    font-size: 14px;
    padding: 8px 0;
  }
  .close {
    display: grid;
    place-items: center;
    flex: 0 0 32px;
    height: 32px;
    padding: 0;
    border: 0;
    border-radius: 8px;
    background: transparent;
    color: var(--muted);
  }
  .close:hover {
    background: var(--hover-surface);
  }
  .search-body {
    min-height: 0;
    overflow-y: auto;
    overscroll-behavior: contain;
    padding: 6px;
  }
  a {
    display: flex;
    align-items: center;
    gap: 12px;
    padding: 12px;
    border-radius: 8px;
    color: var(--text);
    text-decoration: none;
  }
  a.active,
  a:hover {
    background: var(--hover-surface);
  }
  .identity {
    display: flex;
    flex-direction: column;
    gap: 4px;
    min-width: 0;
    flex: 1;
  }
  strong {
    font-size: 14px;
    font-weight: 550;
    overflow-wrap: anywhere;
  }
  .identity > span {
    color: var(--muted);
    font-size: 12px;
    overflow-wrap: anywhere;
  }
  .current {
    flex-shrink: 0;
    color: var(--accent);
  }
  .empty {
    padding: 20px 12px;
    color: var(--muted);
    text-align: center;
    font-size: 13px;
  }
  .error {
    padding: 12px;
    color: var(--danger);
    font-size: 13px;
  }
  .error button,
  .more {
    border: 0;
    background: transparent;
    color: var(--accent);
    padding: 10px;
  }
  .create {
    flex-shrink: 0;
    border: 0;
    border-top: 1px solid var(--panel-border);
    padding: 14px 18px;
    text-align: left;
    background: transparent;
    color: var(--text);
  }
  .create:hover {
    background: var(--hover-surface);
  }
  .workspace-search {
    position: fixed;
    top: var(--mobile-viewport-top, 0px);
    left: 0;
    right: 0;
    bottom: auto;
    margin: 0;
    width: 100%;
    max-width: none;
    height: var(--mobile-viewport-height, 100dvh);
    max-height: none;
    box-sizing: border-box;
    border: 0;
    border-radius: 0;
    overflow: hidden;
  }
  .workspace-search[open] {
    display: flex;
    flex-direction: column;
  }
  .workspace-search::backdrop {
    background: #0007;
  }
  h2 {
    order: 0;
    margin: 0;
    font-size: 16px;
    font-weight: 550;
    padding: max(20px, env(safe-area-inset-top)) 18px 16px;
    border-bottom: 1px solid var(--panel-border);
  }
  .workspace-search .search-body {
    order: 1;
    flex: 1;
    padding: 12px max(8px, env(safe-area-inset-right)) 12px
      max(8px, env(safe-area-inset-left));
  }
  .workspace-search .create {
    order: 2;
    min-height: 44px;
  }
  .workspace-search .search-input {
    order: 3;
    border-bottom: 0;
    border-top: 1px solid var(--panel-border);
    padding: 8px max(12px, env(safe-area-inset-right))
      max(12px, env(safe-area-inset-bottom))
      max(16px, env(safe-area-inset-left));
  }
  .workspace-search input {
    min-height: 44px;
    font-size: 16px;
  }
  .workspace-search .close {
    flex-basis: 44px;
    height: 44px;
    background: var(--hover-surface);
  }
</style>
