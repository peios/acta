<script lang="ts">
  import { onMount } from "svelte";
  import { api, errorMessage } from "$lib/api";
  import OptionPicker from "../OptionPicker.svelte";
  type Policy = {
    revision: number;
    enabled: boolean;
    destination: string;
    mode: string;
    anchor: string;
    backup_minutes: number;
    full_minutes: number;
    drill_minutes: number;
    max_age_minutes: number;
    archive_age_minutes: number;
    retain_full: number;
  };
  type Point = {
    label: string;
    type: string;
    finished_at: string;
    database_version: string;
    release: string;
    complete: boolean;
    integrity_at?: string;
    restored_at?: string;
  };
  type Job = {
    id: string;
    kind: string;
    state: string;
    requested_at: string;
    finished_at?: string;
    error?: string;
    backup?: string;
  };
  type View = {
    min_retain_full: number;
    max_retain_full: number;
    configured: boolean;
    policy: Policy;
    destinations: { id: string; name: string }[];
    can_drill: boolean;
    warnings: string[];
    jobs: Job[];
    repository: {
      observed_at: string;
      point_count?: number;
      points: Point[];
      archive_last?: string;
      error?: string;
    };
    alert_error?: string;
  };
  let view = $state<View>(),
    draft = $state<Policy>(),
    baseline = $state(""),
    error = $state(""),
    busy = $state(false),
    loading = $state(false);
  const dirty = $derived(draft && JSON.stringify(draft) !== baseline);
  const running = $derived(
    view?.jobs?.some((j) => j.state === "queued" || j.state === "running") ??
      false,
  );
  const points = $derived(view?.repository?.points ?? []);
  const verified = $derived(points.find((p) => p.complete && p.integrity_at));
  const restored = $derived(
    points
      .filter((p) => p.restored_at)
      .sort(
        (a, b) => Date.parse(b.restored_at!) - Date.parse(a.restored_at!),
      )[0],
  );
  function date(value?: string) {
    return value && !value.startsWith("0001-")
      ? new Date(value).toLocaleString()
      : "Not yet";
  }
  async function load(reset = false) {
    if (loading) return;
    loading = true;
    try {
      const result = await api<View>("backups");
      view = result;
      if (result.configured && (!dirty || reset)) {
        draft = structuredClone(result.policy);
        baseline = JSON.stringify(draft);
      }
      error = "";
    } catch (e) {
      error = errorMessage(e);
    } finally {
      loading = false;
    }
  }
  async function save() {
    if (!draft || busy) return;
    busy = true;
    try {
      const result = await api<Policy>("backups/policy", draft);
      draft = result;
      baseline = JSON.stringify(result);
      await load();
    } catch (e) {
      error = errorMessage(e);
    } finally {
      busy = false;
    }
  }
  async function run(kind: string) {
    if (busy) return;
    busy = true;
    try {
      await api("backups/jobs", { kind });
      await load();
    } catch (e) {
      error = errorMessage(e);
    } finally {
      busy = false;
    }
  }
  onMount(() => {
    void load();
    const timer = setInterval(() => void load(), 10000);
    return () => clearInterval(timer);
  });
</script>

<div class="backups-page">
  <div class="intro">
    <div>
      <h2>Backups and recovery</h2>
      <p>
        Protect this installation with independent backups and tested restores.
      </p>
    </div>
    <button class="secondary" disabled={loading} onclick={() => load()}
      >Refresh</button
    >
  </div>
  {#if error}<p class="notice error" role="alert">{error}</p>{/if}
  {#if !view}<p class="muted">Connecting to backup service…</p>
  {:else if !view.configured}
    <section class="empty">
      <svg
        width="32"
        height="32"
        viewBox="0 0 24 24"
        fill="none"
        stroke="currentColor"
        stroke-width="1.4"
        aria-hidden="true"
        ><path d="M4 7h16v13H4zM3 3h18v4H3zM9 11h6M12 14v4m-2-2 2 2 2-2" /></svg
      >
      <h3>Connect a backup service</h3>
      <p>
        The deployment operator needs to connect the independent backup service
        before you can configure policy here. It runs alongside PostgreSQL and
        keeps working when Acta is unavailable.
      </p>
      <p class="muted">No automatic backups are configured through Acta.</p>
    </section>
  {:else if draft}
    <div class="evidence">
      <section>
        <span>Integrity checked</span><strong
          >{date(verified?.finished_at)}</strong
        ><small>{verified?.label ?? "No complete recovery set"}</small>
      </section>
      <section>
        <span>Restore verified</span><strong
          >{date(restored?.restored_at)}</strong
        ><small
          >{restored?.label ??
            "A successful upload is not a restore test"}</small
        >
      </section>
      <section>
        <span>Transaction log archived</span><strong
          >{date(view.repository.archive_last)}</strong
        ><small>Archive evidence, not a guaranteed recovery endpoint</small>
      </section>
    </div>
    {#if view.warnings.length || view.alert_error}<div
        class="warnings"
        role="status"
      >
        {#each view.warnings as warning}<p>
            {warning}
          </p>{/each}{#if view.alert_error}<p>{view.alert_error}</p>{/if}
      </div>{/if}
    <section class="policy">
      <div class="section-heading">
        <div>
          <h3>Backup policy</h3>
          <p>
            Credentials and allowed destinations are managed by the deployment
            operator.
          </p>
        </div>
        <label class="switch"
          ><input
            type="checkbox"
            bind:checked={draft.enabled}
            disabled={busy || running}
          /> Automatic backups</label
        >
      </div>
      <form
        onsubmit={(e) => {
          e.preventDefault();
          void save();
        }}
      >
        <div class="fields">
          <div class="field">
            <span>Destination</span><OptionPicker
              label="Backup destination"
              value={draft.destination}
              options={view.destinations.map((d) => ({
                value: d.id,
                label: d.name,
              }))}
              onchange={(v) => {
                if (draft) draft.destination = v;
              }}
              disabled={busy || running}
            />
          </div>
          <div class="field">
            <span>Recovery mode</span><OptionPicker
              label="Recovery mode"
              value={draft.mode}
              options={[
                { value: "snapshot", label: "Scheduled recovery points" },
                { value: "continuous", label: "Continuous transaction logs" },
              ]}
              onchange={(v) => {
                if (draft) draft.mode = v;
              }}
              disabled={busy || running}
            />
          </div>
          <label class="field"
            >Backup interval (minutes)<input
              type="number"
              min="5"
              max="525600"
              required
              bind:value={draft.backup_minutes}
              disabled={busy || running}
            /></label
          >
          <label class="field"
            >Full backup interval (minutes)<input
              type="number"
              min={draft.backup_minutes}
              max="525600"
              required
              bind:value={draft.full_minutes}
              disabled={busy || running}
            /></label
          >
          <label class="field"
            >Full backups to retain<input
              type="number"
              min={view.min_retain_full}
              max={view.max_retain_full}
              required
              bind:value={draft.retain_full}
              disabled={busy || running}
            /></label
          >
          <label class="field"
            >Restore drill interval (minutes)<input
              type="number"
              min="0"
              max="525600"
              required
              bind:value={draft.drill_minutes}
              disabled={busy || running || !view.can_drill}
            /><small
              >0 means manual only. A recovery identity must be provisioned for
              drills.</small
            ></label
          >
          <label class="field"
            >Alert when backup is older than (minutes)<input
              type="number"
              min={draft.backup_minutes}
              max="1051200"
              required
              bind:value={draft.max_age_minutes}
              disabled={busy || running}
            /></label
          >
          {#if draft.mode === "continuous"}<label class="field"
              >Maximum archive age (minutes)<input
                type="number"
                min="1"
                max="1440"
                required
                bind:value={draft.archive_age_minutes}
                disabled={busy || running}
              /><small>PostgreSQL archive settings must meet this limit.</small
              ></label
            >{/if}
          <label class="field"
            >Schedule anchor (UTC)<input
              type="text"
              required
              bind:value={draft.anchor}
              disabled={busy || running}
            /><small
              >ISO date and time. Missed intervals coalesce into one run.</small
            ></label
          >
        </div>
        <div class="form-footer">
          <p>
            The last full backup verified by a restore drill is protected from
            automatic expiry.
          </p>
          <div>
            <button
              type="button"
              class="secondary"
              disabled={!dirty || busy}
              onclick={() => load(true)}>Reset</button
            ><button class="primary" disabled={!dirty || busy || running}
              >{busy ? "Saving…" : "Save policy"}</button
            >
          </div>
        </div>
      </form>
    </section>
    <section>
      <div class="section-heading">
        <div>
          <h3>Operations</h3>
          <p>
            Restoration to production is performed separately by the deployment
            operator.
          </p>
        </div>
        <div class="actions">
          <button
            class="secondary"
            disabled={busy || running || dirty}
            onclick={() => run("full")}>Back up now</button
          ><button
            class="secondary"
            disabled={busy || running || dirty || !view.can_drill || !verified}
            onclick={() => run("drill")}>Test restore</button
          >
        </div>
      </div>
      {#if !view.jobs.length}<p class="muted">No operations yet.</p>{:else}<div
          class="jobs"
        >
          {#each view.jobs as job (job.id)}<div class="job">
              <div>
                <strong
                  >{job.kind === "drill"
                    ? "Restore drill"
                    : job.kind === "full"
                      ? "Full backup"
                      : "Incremental backup"}</strong
                ><span class:failed={job.state === "failed"}>{job.state}</span>
              </div>
              <time>{date(job.requested_at)}</time>{#if job.error}<p
                  class="failed"
                >
                  {job.error}
                </p>{/if}
            </div>{/each}
        </div>{/if}
    </section>
    <section>
      <div class="section-heading">
        <div>
          <h3>Recovery points</h3>
          <p>
            Repository checked {date(view.repository.observed_at)}. Missing or
            stale evidence is never treated as healthy.
            {#if (view.repository.point_count ?? 0) > points.length}Showing {points.length}
              of {view.repository.point_count} recovery points. The full catalog is
              available through the recovery CLI.{/if}
          </p>
        </div>
      </div>
      {#if !points.length}<p class="muted">
          No recovery points found.
        </p>{:else}<div class="points">
          {#each points as point (point.label)}<div class="point">
              <div>
                <strong>{date(point.finished_at)}</strong><small
                  >{point.type} · PostgreSQL {point.database_version} · {point.release ||
                    "Unknown release"}</small
                >
              </div>
              <span
                >{point.restored_at
                  ? "Restore verified"
                  : point.integrity_at
                    ? "Integrity checked"
                    : point.complete
                      ? "Awaiting verification"
                      : "Database only"}</span
              ><code>{point.label}</code>
            </div>{/each}
        </div>{/if}
    </section>
  {/if}
</div>

<style>
  .backups-page {
    max-width: 1100px;
    margin: 0 auto;
    display: grid;
    gap: 28px;
    padding-bottom: 32px;
  }
  h2,
  h3,
  p {
    margin: 0;
  }
  h2 {
    font-size: 20px;
    font-weight: 600;
  }
  h3 {
    font-size: 16px;
    font-weight: 600;
  }
  .intro,
  .section-heading {
    display: flex;
    align-items: center;
    justify-content: space-between;
    gap: 20px;
  }
  .intro p,
  .section-heading p {
    margin-top: 6px;
    color: var(--muted);
    font-size: 13px;
    line-height: 1.6;
  }
  .empty {
    border: 1px dashed var(--panel-border);
    border-radius: 14px;
    padding: 44px;
    display: grid;
    gap: 16px;
    justify-items: center;
    text-align: center;
  }
  .empty p {
    max-width: 540px;
    line-height: 1.7;
  }
  .empty svg {
    color: var(--muted);
  }
  .evidence {
    display: grid;
    grid-template-columns: repeat(3, minmax(0, 1fr));
    gap: 12px;
  }
  .evidence section {
    display: flex;
    flex-direction: column;
    gap: 10px;
    padding: 20px;
    border: 1px solid var(--panel-border);
    border-radius: 12px;
    background: var(--surface);
    min-width: 0;
  }
  .evidence span,
  small,
  .muted {
    color: var(--muted);
  }
  .evidence span {
    font-size: 12px;
  }
  .evidence strong {
    font-size: 15px;
    font-weight: 550;
  }
  .evidence small {
    font-size: 11px;
    overflow-wrap: anywhere;
  }
  .warnings {
    padding: 14px 18px;
    border-left: 2px solid var(--accent);
    background: var(--hover-surface);
    border-radius: 0 8px 8px 0;
  }
  .warnings p {
    font-size: 13px;
    line-height: 1.8;
  }
  .policy {
    border: 1px solid var(--panel-border);
    border-radius: 12px;
    padding: 22px;
  }
  .fields {
    display: grid;
    grid-template-columns: repeat(2, minmax(0, 1fr));
    gap: 22px;
    margin-top: 24px;
  }
  .field {
    display: flex;
    flex-direction: column;
    gap: 8px;
    font-size: 13px;
    min-width: 0;
  }
  .field input {
    width: 100%;
    min-width: 0;
  }
  .field small {
    font-size: 12px;
    line-height: 1.5;
  }
  .switch {
    display: flex;
    align-items: center;
    gap: 8px;
    white-space: nowrap;
    font-size: 13px;
  }
  .switch input {
    width: auto;
  }
  .form-footer {
    display: flex;
    align-items: center;
    justify-content: space-between;
    gap: 20px;
    margin-top: 24px;
    padding-top: 18px;
    border-top: 1px solid var(--panel-border);
  }
  .form-footer p {
    font-size: 12px;
    line-height: 1.6;
    color: var(--muted);
    max-width: 480px;
  }
  .form-footer div,
  .actions {
    display: flex;
    gap: 8px;
  }
  .actions button,
  .form-footer button {
    white-space: nowrap;
  }
  .jobs,
  .points {
    margin-top: 16px;
  }
  .job,
  .point {
    padding: 14px 0;
    border-top: 1px solid var(--panel-border);
    display: flex;
    align-items: center;
    justify-content: space-between;
    flex-wrap: wrap;
    gap: 8px;
    font-size: 13px;
  }
  .job div {
    display: flex;
    gap: 12px;
    align-items: center;
  }
  .job strong,
  .point strong {
    font-weight: 500;
  }
  .job span,
  .job time,
  .point span {
    color: var(--muted);
    font-size: 12px;
  }
  .job p {
    width: 100%;
    font-size: 12px;
  }
  .point div {
    display: grid;
    gap: 5px;
  }
  .point code {
    width: 100%;
    font-size: 11px;
    color: var(--muted);
    overflow-wrap: anywhere;
  }
  .failed {
    color: var(--danger, #e49b91) !important;
  }
  @media (max-width: 720px) {
    .evidence,
    .fields {
      grid-template-columns: 1fr;
    }
    .intro,
    .section-heading,
    .form-footer {
      align-items: flex-start;
      flex-direction: column;
    }
    .policy {
      padding: 16px;
    }
    .empty {
      padding: 28px 18px;
    }
    .actions {
      flex-wrap: wrap;
    }
  }
</style>
