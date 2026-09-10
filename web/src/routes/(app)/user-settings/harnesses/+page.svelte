<script lang="ts">
  import { onMount } from "svelte";
  import { fade } from "svelte/transition";
  import { watchHarnesses, type HarnessView } from "$lib/harnesses";
  import ProviderList from "$lib/components/ProviderList.svelte";
  import RelativeTime from "$lib/components/RelativeTime.svelte";
  let view = $state<HarnessView>({
    state: "connecting",
    connections: [],
    message: "",
  });
  let reducedMotion = $state(false);
  onMount(() => {
    reducedMotion = window.matchMedia(
      "(prefers-reduced-motion: reduce)",
    ).matches;
    return watchHarnesses((next) => {
      view = next;
    });
  });
</script>

<div class="harnesses-page">
  <p class="intro">Development machines connected to your account.</p>
  {#if view.state === "error"}
    <p class="notice error" role="alert">{view.message}</p>
  {:else if view.state !== "live"}
    <p class="hint connection-message" role="status">
      {view.state === "connecting"
        ? "Connecting…"
        : "Connection interrupted. Reconnecting…"}
    </p>
  {:else if view.connections.length === 0}
    <div class="empty" in:fade={{ duration: reducedMotion ? 0 : 140 }}>
      <span class="machine large" aria-hidden="true">{@render machine()}</span>
      <h2>Connect your development machine</h2>
      <p>Run this command from a signed-in Acta CLI profile.</p>
      <code>acta2 harness</code>
      <p class="profile-hint">
        Use <code>-p &lt;profile&gt;</code> to choose another profile.
      </p>
    </div>
  {:else}
    <ul aria-label="Connected harnesses">
      {#each view.connections as connection (connection.id)}
        <li in:fade={{ duration: reducedMotion ? 0 : 140 }}>
          <details>
            <summary>
              <span class="machine" aria-hidden="true">{@render machine()}</span
              >
              <span class="identity">
                <strong>{connection.hostname}</strong>
                <span
                  >Connected <RelativeTime
                    value={connection.connected_at}
                  /></span
                >
              </span>
              <span class="status"
                ><span class="dot" aria-hidden="true"></span>Connected</span
              >
              <svg
                class="chevron"
                width="16"
                height="16"
                viewBox="0 0 24 24"
                fill="none"
                stroke="currentColor"
                stroke-width="1.6"
                aria-hidden="true"><path d="m9 5 7 7-7 7" /></svg
              >
            </summary>
            <ProviderList providers={connection.providers} />
          </details>
        </li>
      {/each}
    </ul>
    <p class="footnote">
      Only active connections appear here. Run <code>acta2 harness</code> on another
      machine to connect it.
    </p>
  {/if}
</div>

{#snippet machine()}
  <svg
    width="21"
    height="21"
    viewBox="0 0 24 24"
    fill="none"
    stroke="currentColor"
    stroke-width="1.5"
    stroke-linecap="round"
    stroke-linejoin="round"
    ><rect x="3" y="3" width="18" height="13" rx="2" /><path
      d="M8 21h8m-4-5v5"
    /></svg
  >
{/snippet}

<style>
  .harnesses-page {
    max-width: 800px;
    width: 100%;
    padding: 8px 0 40px;
  }
  .intro {
    margin: 0 0 28px;
    font-size: 14px;
    color: var(--muted);
  }
  .connection-message {
    padding: 24px 0;
  }
  ul {
    list-style: none;
    padding: 0;
    margin: 0;
  }
  li {
    border-bottom: 1px solid var(--panel-border);
  }
  summary {
    cursor: pointer;
    list-style: none;
    display: flex;
    align-items: center;
    gap: 16px;
    padding: 20px 8px;
    border-radius: 8px;
  }
  summary::-webkit-details-marker {
    display: none;
  }
  summary:hover {
    background: var(--hover-surface);
  }
  summary:focus-visible {
    outline: 2px solid var(--muted);
    outline-offset: 2px;
  }
  .chevron {
    flex-shrink: 0;
    color: var(--muted);
    transition: transform 160ms ease;
  }
  details[open] .chevron {
    transform: rotate(90deg);
  }
  @media (prefers-reduced-motion: reduce) {
    .chevron {
      transition: none;
    }
  }
  .machine {
    display: grid;
    place-items: center;
    width: 40px;
    height: 40px;
    flex-shrink: 0;
    border-radius: 11px;
    color: var(--muted);
    background: var(--hover-surface);
  }
  .identity {
    display: flex;
    flex-direction: column;
    gap: 6px;
    flex: 1;
    min-width: 0;
    overflow-wrap: anywhere;
  }
  .identity strong {
    font-size: 14px;
    font-weight: 550;
  }
  .identity > span {
    font-size: 12px;
    color: var(--muted);
  }
  .status {
    display: inline-flex;
    align-items: center;
    gap: 7px;
    font-size: 12px;
    color: var(--muted);
    white-space: nowrap;
  }
  .dot {
    width: 6px;
    height: 6px;
    border-radius: 50%;
    background: #6caa83;
  }
  .empty {
    padding: 42px 24px;
    border: 1px solid var(--panel-border);
    border-radius: 12px;
    text-align: center;
  }
  .large {
    margin: 0 auto 20px;
    width: 48px;
    height: 48px;
  }
  h2 {
    font-size: 17px;
    font-weight: 550;
    margin: 0 0 12px;
  }
  .empty p {
    font-size: 13px;
    color: var(--muted);
    line-height: 1.7;
    margin: 0 0 20px;
  }
  .empty > code {
    display: inline-block;
    padding: 11px 18px;
    background: var(--input-bg);
    border: 1px solid var(--panel-border);
    border-radius: 8px;
    font-size: 13px;
    user-select: all;
  }
  .empty .profile-hint {
    font-size: 12px;
    margin: 18px 0 0;
  }
  .footnote {
    color: var(--muted);
    font-size: 12px;
    line-height: 1.7;
    margin-top: 22px;
  }
  @media (max-width: 440px) {
    summary {
      gap: 10px;
      padding-inline: 0;
    }
    .status {
      font-size: 11px;
    }
  }
</style>
