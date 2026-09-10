<script lang="ts">
  import { untrack } from "svelte";
  import { api, APIError, errorMessage } from "$lib/api";
  import MarkdownView from "$lib/components/tasks/MarkdownView.svelte";
  type Preference = { content: string; revision: number; can_write: boolean };
  let {
    scope,
    initial,
    onsaved,
  }: {
    scope: "site" | "user";
    initial: Preference;
    onsaved: () => Promise<void>;
  } = $props();
  let baseline = $state(untrack(() => initial));
  let content = $state(untrack(() => initial.content));
  let busy = $state(false),
    error = $state(""),
    saved = $state(false);
  let latest = $state<Preference | null>(null);
  let dirty = $derived(content !== baseline.content);
  let length = $derived(Array.from(content).length);
  async function save() {
    busy = true;
    error = "";
    saved = false;
    try {
      baseline = await api<Preference>("guide/preferences", {
        scope,
        content,
        revision: baseline.revision,
      });
      content = baseline.content;
      latest = null;
      saved = true;
      await onsaved();
    } catch (e) {
      error = errorMessage(e);
      if (e instanceof APIError && e.code === "guide_changed") {
        try {
          latest = (await api<Record<"site" | "user", Preference>>("guide"))[
            scope
          ];
        } catch {
          /* Keep the draft and original error if the refresh also fails. */
        }
      }
    } finally {
      busy = false;
    }
  }
</script>

<section class="preference">
  <div class="heading">
    <div>
      <h2>{scope === "site" ? "Site preferences" : "My preferences"}</h2>
      <p>
        {scope === "site"
          ? "Essential policy for everyone on this installation."
          : "Essential policy for you and all your agents, across every project."}
      </p>
    </div>
    {#if !baseline.can_write}<span class="badge">Read only</span>{/if}
  </div>
  {#if baseline.can_write}
    <div class="guidance" id={`${scope}-guidance`}>
      <strong>Keep this short and essential.</strong> Guide preferences are
      included every time an agent reads the guide. Use them only for policy
      that should apply across all work within this scope: {scope === "site"
        ? "everyone on this installation"
        : "all your agents and projects"}.
      <p>
        Put selectively useful conventions and gotchas in memories.
        Task-specific instructions and decisions belong on the task.
      </p>
    </div>
    <label class="sr-only" for={`${scope}-preferences`}
      >{scope === "site" ? "Site preferences" : "My preferences"} Markdown</label
    >
    <textarea
      id={`${scope}-preferences`}
      aria-describedby={`${scope}-guidance`}
      bind:value={content}
      disabled={busy}
      rows="8"
      placeholder="Only policy that always needs to be present…"
      oninput={() => (saved = false)}></textarea>
    <div class="footer">
      <span class:over={length > 8000}
        >{length.toLocaleString()} / 8,000 characters · Markdown</span
      >
      <div class="actions">
        <span role="status">{saved && !dirty ? "Saved" : ""}</span>
        {#if dirty}<button
            class="quiet"
            disabled={busy}
            onclick={() => {
              content = baseline.content;
              error = "";
              latest = null;
            }}>Reset</button
          >{/if}
        <button
          class="save"
          disabled={busy || !dirty || length > 8000 || !!latest}
          onclick={save}>{busy ? "Saving…" : "Save preferences"}</button
        >
      </div>
    </div>
  {:else if baseline.content.trim()}
    <MarkdownView value={baseline.content} />
  {:else}<p class="empty">No site preferences have been added.</p>{/if}
  {#if error}<p class="error" role="alert">{error}</p>{/if}
  {#if latest}
    <div class="conflict">
      <h3>Latest saved version</h3>
      {#if latest.content.trim()}<MarkdownView
          value={latest.content}
        />{:else}<p>These preferences were cleared.</p>{/if}
      <p>
        Your draft is still above. Copy anything you want to keep before loading
        the latest version.
      </p>
      <button
        class="quiet"
        onclick={() => {
          baseline = latest!;
          content = baseline.content;
          latest = null;
          error = "";
        }}>Load latest and discard draft</button
      >
    </div>
  {/if}
</section>

<style>
  .preference {
    padding: 24px;
    border: 1px solid var(--panel-border);
    border-radius: 16px;
    background: var(--surface);
  }
  .heading {
    display: flex;
    align-items: start;
    justify-content: space-between;
    gap: 16px;
  }
  h2 {
    font-size: 18px;
    margin: 0 0 6px;
  }
  h3 {
    font-size: 15px;
  }
  p {
    color: var(--muted);
    font-size: 14px;
    line-height: 1.6;
    margin: 0;
  }
  .badge {
    font-size: 12px;
    color: var(--muted);
    white-space: nowrap;
  }
  .guidance {
    color: var(--muted);
    font-size: 13px;
    line-height: 1.65;
    margin: 20px 0 14px;
    max-width: 76ch;
  }
  .guidance strong {
    color: var(--text);
    font-weight: 550;
  }
  .guidance p {
    font-size: inherit;
    margin-top: 8px;
  }
  textarea {
    display: block;
    width: 100%;
    box-sizing: border-box;
    resize: vertical;
    min-height: 170px;
    border: 1px solid var(--panel-border);
    border-radius: 10px;
    background: color-mix(in srgb, var(--text) 3%, transparent);
    color: var(--text);
    font: inherit;
    font-size: 14px;
    line-height: 1.65;
    padding: 14px 16px;
  }
  textarea:focus-visible {
    outline: 2px solid var(--accent);
    outline-offset: 2px;
  }
  .footer,
  .actions {
    display: flex;
    gap: 12px;
    align-items: center;
  }
  .footer {
    justify-content: space-between;
    margin-top: 12px;
    color: var(--muted);
    font-size: 12px;
    flex-wrap: wrap;
  }
  button {
    width: auto;
    font-size: 13px;
    padding: 8px 12px;
    border-radius: 8px;
  }
  .quiet {
    background: transparent;
    color: var(--text);
    border: 1px solid var(--panel-border);
  }
  .save {
    background: var(--accent);
    color: var(--on-accent);
    border: 1px solid transparent;
  }
  button:disabled {
    opacity: 0.45;
  }
  .error,
  .over {
    color: var(--danger);
  }
  .error {
    margin-top: 12px;
  }
  .conflict {
    border-top: 1px solid var(--panel-border);
    margin-top: 18px;
    padding-top: 8px;
  }
  .conflict button {
    margin-top: 12px;
  }
  .empty {
    margin-top: 16px;
  }
  .sr-only {
    position: absolute;
    width: 1px;
    height: 1px;
    padding: 0;
    overflow: hidden;
    clip: rect(0, 0, 0, 0);
    white-space: nowrap;
  }
  @media (max-width: 600px) {
    .preference {
      padding: 18px;
    }
  }
</style>
