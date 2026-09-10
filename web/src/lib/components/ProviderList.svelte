<script lang="ts">
  import type { ProviderStatus } from "$lib/harnesses";
  let { providers }: { providers: ProviderStatus[] } = $props();
  const issues = {
    timeout: "The local check timed out. It will retry automatically.",
    probe_failed: "The local check failed. It will retry automatically.",
    version_unknown: "The CLI version could not be recognised.",
    auth_unavailable: "This CLI could not report a recognised sign-in state.",
  };
  function label(provider: ProviderStatus): string {
    if (provider.installation === "checking") return "Checking…";
    if (provider.installation === "not_installed") return "Not installed";
    if (provider.installation === "unknown") return "Installation unknown";
    return {
      signed_in: "Signed in",
      signed_out: "Not signed in",
      unknown: "Sign-in unknown",
    }[provider.authentication];
  }
</script>

<div class="providers">
  <ul aria-label="Providers">
    {#each providers as provider (provider.id)}
      <li>
        <span class="provider-identity">
          <strong>{provider.id === "codex" ? "Codex" : "Claude Code"}</strong>
          <span class="version"
            >{provider.installation === "installed"
              ? provider.version
                ? `v${provider.version}`
                : "Installed · version unknown"
              : ""}</span
          >
        </span>
        <span
          class="provider-status"
          class:signed-in={provider.authentication === "signed_in"}
          title={provider.issue ? issues[provider.issue] : undefined}
        >
          <span class="dot" aria-hidden="true"></span>{label(provider)}
        </span>
      </li>
    {/each}
  </ul>
  <p>
    Checked locally every 30 seconds. Sign-in reflects the CLI’s stored
    credentials; access and limits are not checked.
  </p>
</div>

<style>
  .providers {
    margin: 0 8px 18px 64px;
  }
  ul {
    list-style: none;
    margin: 0;
    padding: 0;
  }
  li {
    display: flex;
    align-items: center;
    gap: 16px;
    padding: 12px 0;
  }
  li + li {
    border-top: 1px solid var(--panel-border);
  }
  .provider-identity {
    display: flex;
    align-items: baseline;
    gap: 10px;
    flex: 1;
    min-width: 0;
    flex-wrap: wrap;
  }
  strong {
    font-size: 13px;
    font-weight: 550;
  }
  .version {
    color: var(--muted);
    font-size: 11px;
  }
  .provider-status {
    display: inline-flex;
    align-items: center;
    gap: 7px;
    color: var(--muted);
    font-size: 12px;
    white-space: nowrap;
  }
  .dot {
    width: 5px;
    height: 5px;
    border-radius: 50%;
    background: currentColor;
    opacity: 0.5;
  }
  .signed-in .dot {
    background: #6caa83;
    opacity: 1;
  }
  p {
    margin: 12px 0 0;
    color: var(--muted);
    font-size: 11px;
    line-height: 1.6;
  }
  @media (max-width: 440px) {
    .providers {
      margin-left: 12px;
    }
    li {
      gap: 10px;
    }
  }
</style>
