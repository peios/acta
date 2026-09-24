<script lang="ts">
  import { anchoredPopover } from "$lib/anchored-popover";
  import {
    useCodeHosts,
    type CodeHost,
    type CodeProvider,
  } from "$lib/code-hosts.svelte";
  import ProviderIcon from "$lib/components/threads/ProviderIcon.svelte";
  const hosts = useCodeHosts();
  const selected = $derived(
    hosts.hosts.find((host) => host.id === hosts.selected),
  );
  const id = $props.id();
  let trigger: HTMLButtonElement;
  let popup: HTMLDivElement;
  let open = $state(false);
  function status(host: CodeHost) {
    return hosts.error
      ? "Status unavailable"
      : host.online
        ? "Online"
        : "Offline";
  }
  function close() {
    popup.hidePopover();
    trigger.focus({ preventScroll: true });
  }
  function providerLabel(provider: CodeProvider) {
    const name = provider.id === "claude" ? "Claude Code" : "Codex";
    const auth = hosts.error ? "unknown" : provider.auth;
    return `${name}: ${auth === "signed_in" ? "signed in" : auth === "signed_out" ? "signed out" : "auth status unknown"}`;
  }
</script>

<button
  class="switcher"
  bind:this={trigger}
  popovertarget={id}
  aria-label="Switch host"
  aria-haspopup="dialog"
  aria-expanded={open}
  title={selected ? `${selected.name} · ${status(selected)}` : "Switch host"}
>
  <span
    >{selected?.name ??
      (hosts.loading ? "Loading hosts…" : "Select host")}</span
  ><span aria-hidden="true">⌄</span>
</button>
<div
  {id}
  bind:this={popup}
  popover="auto"
  role="dialog"
  aria-label="Switch host"
  class="host-picker"
  use:anchoredPopover={() => ({ anchor: trigger, width: 300, height: 340 })}
  ontoggle={(event) => {
    open = event.newState === "open";
    if (open)
      popup
        .querySelector<HTMLButtonElement>("button")
        ?.focus({ preventScroll: true });
  }}
>
  <header>
    <strong>Your hosts</strong><button
      class="close"
      aria-label="Close host picker"
      onclick={close}>×</button
    >
  </header>
  {#if hosts.error}
    <p role="status">{hosts.error}</p>
  {:else if hosts.loading}
    <p role="status">Loading your hosts…</p>
  {/if}
  {#each hosts.hosts as host (host.id)}
    <button
      class="host-option"
      class:selected={host.id === hosts.selected}
      aria-label={`${host.name}, ${status(host)}${host.id === hosts.selected ? ", selected" : ""}${(
        host.provider_status?.providers ?? []
      )
        .filter((p) => p.installed)
        .map((p) => `, ${providerLabel(p)}`)
        .join("")}`}
      aria-pressed={host.id === hosts.selected}
      onclick={() => {
        hosts.selected = host.id;
        close();
      }}
    >
      <svg
        width="20"
        height="20"
        viewBox="0 0 24 24"
        fill="none"
        stroke="currentColor"
        stroke-width="1.5"
        aria-hidden="true"
        ><rect x="3" y="3" width="18" height="13" rx="2" /><path
          d="M8 21h8m-4-5v5"
        /></svg
      >
      <span
        ><strong>{host.name}</strong><small
          ><span>{status(host)} · {host.os} · {host.arch}</span>
          {#each host.provider_status?.providers ?? [] as provider (provider.id)}
            {#if provider.installed}
              <span
                class="provider"
                class:auth-warning={!!hosts.error ||
                  provider.auth !== "signed_in"}
                title={providerLabel(provider)}
                aria-hidden="true"><ProviderIcon provider={provider.id} /></span
              >
            {/if}
          {/each}</small
        ></span
      >
      {#if host.id === hosts.selected}<span aria-hidden="true">✓</span>{/if}
    </button>
  {/each}
  {#if !hosts.loading && !hosts.error && !hosts.hosts.length}
    <p>
      No hosts connected yet. Run <code>acta-code-host</code> on your machine using
      your Acta login.
    </p>
  {/if}
</div>

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
  .host-picker {
    position: fixed;
    inset: auto;
    margin: 0;
    padding: 6px;
    color: var(--text);
    background: var(--surface);
    border: 1px solid var(--panel-border);
    border-radius: 12px;
    box-shadow: 0 20px 60px #0004;
    max-height: min(440px, calc(100dvh - 24px));
    overflow-y: auto;
  }
  header {
    display: flex;
    align-items: center;
    justify-content: space-between;
    padding: 4px 7px 8px 12px;
    font-size: 12px;
  }
  button {
    color: inherit;
  }
  .close {
    width: 32px;
    height: 32px;
    border: 0;
    border-radius: 6px;
    background: transparent;
    font-size: 21px;
    color: var(--muted);
  }
  .close:hover {
    background: var(--hover-surface);
  }
  .host-option {
    display: flex;
    align-items: center;
    gap: 12px;
    width: 100%;
    min-height: 58px;
    padding: 12px;
    border: 0;
    border-radius: 8px;
    background: transparent;
    text-align: left;
  }
  .host-option:hover,
  .host-option.selected {
    background: var(--scope-active);
  }
  .host-option > span:first-of-type {
    min-width: 0;
    overflow-wrap: anywhere;
    flex: 1;
    display: flex;
    flex-direction: column;
    gap: 5px;
  }
  .host-option strong {
    font-size: 14px;
    font-weight: 550;
  }
  .host-option small {
    display: flex;
    align-items: center;
    flex-wrap: wrap;
    gap: 7px;
    font-size: 11px;
    color: var(--muted);
  }
  .provider {
    display: inline-flex;
    width: 15px;
    height: 15px;
    color: var(--muted);
  }
  .provider :global(svg) {
    width: 15px;
    height: 15px;
  }
  .provider.auth-warning {
    color: #d98a42;
  }
  p {
    margin: 12px;
    color: var(--muted);
    font-size: 11px;
  }
</style>
