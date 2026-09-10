<script lang="ts">
  import { onMount } from "svelte";
  import { api, errorMessage } from "$lib/api";
  import MarkdownView from "$lib/components/tasks/MarkdownView.svelte";
  import PreferenceEditor from "$lib/components/guide/PreferenceEditor.svelte";
  type Preference = { content: string; revision: number; can_write: boolean };
  type Guide = {
    markdown: string;
    built_in: string;
    site: Preference;
    user: Preference;
  };
  let data = $state<Guide | null>(null),
    markdown = $state("");
  let tab = $state<"guide" | "preferences">("guide"),
    error = $state("");
  async function load() {
    error = "";
    try {
      const result = await api<Guide>("guide");
      data = result;
      markdown = result.markdown;
    } catch (e) {
      error = errorMessage(e);
    }
  }
  async function refreshPreview() {
    try {
      markdown = (await api<Guide>("guide")).markdown;
      error = "";
    } catch (e) {
      error = "The guide preview could not refresh. " + errorMessage(e);
    }
  }
  function tabKey(event: KeyboardEvent) {
    if (!["ArrowLeft", "ArrowRight", "Home", "End"].includes(event.key)) return;
    event.preventDefault();
    tab =
      event.key === "Home"
        ? "guide"
        : event.key === "End"
          ? "preferences"
          : tab === "guide"
            ? "preferences"
            : "guide";
    document.getElementById(`${tab}-tab`)?.focus();
  }
  onMount(load);
</script>

<svelte:head><title>Guide · Acta</title></svelte:head>
<div class="guide-page">
  <header>
    <div class="eyebrow">Working with Acta</div>
    <h1>Agent guide</h1>
    <p>
      The guide your agents receive, with essential policy for your site and for
      you.
    </p>
  </header>
  <div class="tabs" role="tablist" aria-label="Agent guide">
    <button
      id="guide-tab"
      role="tab"
      onkeydown={tabKey}
      aria-selected={tab === "guide"}
      aria-controls="guide-panel"
      tabindex={tab === "guide" ? 0 : -1}
      onclick={() => (tab = "guide")}>Current guide</button
    >
    <button
      id="preferences-tab"
      role="tab"
      onkeydown={tabKey}
      aria-selected={tab === "preferences"}
      aria-controls="preferences-panel"
      tabindex={tab === "preferences" ? 0 : -1}
      onclick={() => (tab = "preferences")}>Preferences</button
    >
  </div>
  {#if error}<p role="alert" class="error">{error}</p>
    {#if !data}<button onclick={load}>Try again</button>{/if}{/if}
  {#if data}
    <div
      id="guide-panel"
      role="tabpanel"
      aria-labelledby="guide-tab"
      hidden={tab !== "guide"}
    >
      <div class="preview-note">
        <span
          >Read-only preview · exactly what <code>acta_guide</code> returns for you
          and your agents.</span
        ><button onclick={refreshPreview}>Refresh</button>
      </div>
      <article><MarkdownView value={markdown} /></article>
    </div>
    <div
      id="preferences-panel"
      role="tabpanel"
      aria-labelledby="preferences-tab"
      hidden={tab !== "preferences"}
    >
      <p class="policy-note">
        Acta maintains the built-in guide. These appendices add your policy to
        it. Personal preferences take precedence over conflicting site defaults;
        neither changes permissions. Saved changes are included the next time an
        agent reads the guide.
      </p>
      <div class="editors">
        <PreferenceEditor
          scope="user"
          initial={data.user}
          onsaved={refreshPreview}
        />
        <PreferenceEditor
          scope="site"
          initial={data.site}
          onsaved={refreshPreview}
        />
      </div>
    </div>
  {:else if !error}<p class="loading" role="status">Loading guide…</p>{/if}
</div>

<style>
  .guide-page {
    max-width: 960px;
    margin: 0 auto;
    padding-bottom: 36px;
  }
  header {
    margin-bottom: 26px;
  }
  .eyebrow {
    font-size: 12px;
    color: var(--muted);
    margin-bottom: 8px;
  }
  h1 {
    margin: 0 0 10px;
    font-size: 28px;
    letter-spacing: -0.5px;
  }
  header p,
  .policy-note {
    font-size: 14px;
    color: var(--muted);
    line-height: 1.65;
    margin: 0;
  }
  .tabs {
    display: flex;
    gap: 24px;
    border-bottom: 1px solid var(--panel-border);
    margin-bottom: 24px;
  }
  .tabs button {
    border: 0;
    border-bottom: 2px solid transparent;
    border-radius: 0;
    padding: 0 0 12px;
    background: transparent;
    color: var(--muted);
    font-size: 14px;
    width: auto;
  }
  .tabs button[aria-selected="true"] {
    border-bottom-color: var(--accent);
    color: var(--text);
  }
  .preview-note {
    display: flex;
    gap: 12px;
    align-items: center;
    justify-content: space-between;
    font-size: 12px;
    color: var(--muted);
    margin-bottom: 20px;
    line-height: 1.6;
  }
  .preview-note button {
    width: auto;
    background: transparent;
    color: var(--muted);
    font-size: 12px;
    padding: 6px 10px;
    border: 1px solid var(--panel-border);
    border-radius: 7px;
  }
  article {
    border: 1px solid var(--panel-border);
    background: var(--surface);
    border-radius: 16px;
    padding: 28px 32px;
  }
  .policy-note {
    max-width: 78ch;
    margin-bottom: 24px;
  }
  .editors {
    display: grid;
    gap: 24px;
  }
  .error {
    color: var(--danger);
  }
  .loading {
    color: var(--muted);
  }
  @media (max-width: 600px) {
    article {
      padding: 20px;
    }
  }
</style>
