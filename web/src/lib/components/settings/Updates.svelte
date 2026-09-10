<script lang="ts">
  import { onMount } from "svelte";
  import { api } from "$lib/api";
  type Release = { version: string; notes: string };
  type Job = {
    id: string;
    version: string;
    phase: string;
    error?: string;
    updated_at: string;
  };
  type View = {
    configured: boolean;
    repository?: string;
    current?: Release;
    available?: { id: string; release: Release; blocked: string };
    checked_at?: string;
    check_error?: string;
    jobs?: Job[];
  };
  let view = $state<View | null>(null);
  let error = $state("");
  let pending = $state(false);
  let disconnected = $state(false);
  let confirming = $state(false);
  const phases: Record<string, string> = {
    preparing: "Verifying images and recovery",
    quiescing: "Entering maintenance",
    snapshotting: "Saving the recovery point",
    applying: "Installing release",
    validating: "Verifying the updated application",
    committing: "Bringing Acta online",
    restoring: "Recovering the previous release",
    rollback_committing: "Bringing the previous release online",
    succeeded: "Update completed",
    rolled_back: "Previous release recovered",
    failed: "Update not installed",
  };
  let active = $derived(
    view?.jobs?.[0] &&
      !["succeeded", "rolled_back", "failed"].includes(view.jobs[0].phase)
      ? view.jobs[0]
      : null,
  );
  async function refresh() {
    try {
      view = await api<View>("updates");
      disconnected = false;
    } catch (e) {
      if (view) disconnected = true;
      else error = (e as Error).message;
    }
  }
  async function action(name: string, body: unknown = {}) {
    pending = true;
    error = "";
    try {
      await api(`updates/${name}`, body);
      confirming = false;
      await refresh();
    } catch (e) {
      error = (e as Error).message;
    } finally {
      pending = false;
    }
  }
  onMount(() => {
    void refresh();
    const timer = setInterval(() => void refresh(), 3000);
    return () => clearInterval(timer);
  });
</script>

<section class="updates">
  <header>
    <div>
      <h1>Updates</h1>
      <p>Keep your Acta installation up to date.</p>
    </div>
    {#if view?.configured}<button
        class="secondary"
        disabled={pending || !!active}
        onclick={() => action("check")}>Check for updates</button
      >{/if}
  </header>
  {#if error}<p class="notice error" role="alert">{error}</p>{/if}
  {#if !view}<p class="muted">Loading update status…</p>
  {:else if !view.configured}<div class="panel">
      <h2>Connect a deployment updater</h2>
      <p>
        Updates are installed by a separate service on your server. Configure it
        with your release channel and recovery service to enable updates here.
      </p>
    </div>
  {:else}
    <div class="panel installed">
      <div>
        <span class="eyebrow">Installed version</span>
        <h2>{view.current?.version}</h2>
      </div>
      <span class="channel">{view.repository}</span>
    </div>
    {#if disconnected}<p class="notice" role="status">
        Acta is temporarily unavailable. The updater continues independently;
        this page will reconnect automatically.
      </p>{/if}
    {#if active}<div class="panel progress" role="status">
        <span class="spinner" aria-hidden="true"></span>
        <div>
          <h2>{phases[active.phase] ?? active.phase}</h2>
          <p>{active.version} · You can close this page and return later.</p>
          {#if active.error}<p class="notice error">{active.error}</p>
            <button
              class="secondary"
              disabled={pending}
              onclick={() => action("retry")}>Continue recovery</button
            >{/if}
        </div>
      </div>
    {:else if view.available}<div class="panel available">
        <span class="eyebrow">Available update</span>
        <h2>{view.available.release.version}</h2>
        <p class="notes">{view.available.release.notes}</p>
        {#if view.available.blocked}<p class="notice">
            {view.available.blocked}
          </p>{:else if confirming}<div class="confirm">
            <p>
              Acta will be temporarily unavailable while this update is
              installed. A full backup and restore check must pass before
              maintenance begins.
            </p>
            <div class="actions">
              <button class="secondary" onclick={() => (confirming = false)}
                >Cancel</button
              ><button
                disabled={pending}
                onclick={() => action("install", { id: view?.available?.id })}
                >Install update</button
              >
            </div>
          </div>{:else}<button
            disabled={pending}
            onclick={() => (confirming = true)}>Update Acta</button
          >{/if}
      </div>
    {:else}<p class="muted">
        {view.checked_at
          ? "You’re up to date on this release channel."
          : "Check for available releases to get started."}
      </p>{/if}
    {#if view.check_error}<p class="notice error">{view.check_error}</p>{/if}
    {#if view.checked_at}<p class="checked">
        Last checked {new Date(view.checked_at).toLocaleString()}
      </p>{/if}
    {#if view.jobs?.length}<section class="history">
        <h2>Update history</h2>
        {#each view.jobs as job (job.id)}<div class="history-row">
            <div>
              <strong>{job.version}</strong><span
                >{phases[job.phase] ?? job.phase}</span
              >{#if job.error && job !== active}<p>{job.error}</p>{/if}
            </div>
            <time datetime={job.updated_at}
              >{new Date(job.updated_at).toLocaleString()}</time
            >
          </div>{/each}
      </section>{/if}
  {/if}
</section>

<style>
  .updates {
    width: min(100%, 800px);
    margin-inline: auto;
    padding: 1.5rem 0;
  }
  header,
  .installed,
  .history-row,
  .actions {
    display: flex;
    align-items: center;
    justify-content: space-between;
    gap: 1rem;
  }
  header {
    margin-bottom: 2rem;
    flex-wrap: wrap;
  }
  h1 {
    font-size: 1.65rem;
    margin: 0;
  }
  h2 {
    font-size: 1.05rem;
    margin: 0.3rem 0 0.6rem;
  }
  p {
    line-height: 1.6;
    margin: 0.4rem 0;
    color: var(--muted);
  }
  .panel {
    border: 1px solid var(--border);
    border-radius: 16px;
    padding: 1.4rem;
    margin-block: 1rem;
    background: var(--surface);
  }
  .eyebrow,
  .checked,
  .channel,
  time {
    font-size: 0.8rem;
    color: var(--muted);
  }
  .eyebrow {
    display: block;
  }
  .installed h2 {
    font-size: 1.45rem;
    margin-bottom: 0;
  }
  .channel {
    overflow-wrap: anywhere;
  }
  .notes {
    white-space: pre-wrap;
    margin-bottom: 1rem;
  }
  .progress {
    display: flex;
    gap: 1rem;
    align-items: flex-start;
  }
  .spinner {
    width: 18px;
    height: 18px;
    border: 2px solid var(--border);
    border-top-color: currentColor;
    border-radius: 50%;
    animation: spin 1s linear infinite;
    margin-top: 0.4rem;
    flex-shrink: 0;
  }
  .actions {
    justify-content: flex-end;
  }
  .confirm {
    border-top: 1px solid var(--border);
    padding-top: 1rem;
  }
  .history {
    margin-top: 2rem;
  }
  .history-row {
    padding: 1rem 0;
    border-bottom: 1px solid var(--border);
    align-items: flex-start;
  }
  .history-row span {
    display: block;
    color: var(--muted);
    font-size: 0.85rem;
    margin-top: 0.25rem;
  }
  .history-row time {
    white-space: nowrap;
  }
  .history-row p {
    font-size: 0.85rem;
  }
  .checked {
    margin-top: 1rem;
  }
  .muted {
    color: var(--muted);
  }
  @keyframes spin {
    to {
      transform: rotate(360deg);
    }
  }
  @media (prefers-reduced-motion: reduce) {
    .spinner {
      animation: none;
    }
  }
  @media (max-width: 540px) {
    .updates {
      padding: 1rem 0;
    }
    .installed,
    .history-row {
      align-items: flex-start;
      flex-direction: column;
    }
    .panel {
      padding: 1rem;
    }
  }
</style>
