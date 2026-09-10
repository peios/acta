<script lang="ts">
  import { untrack, tick } from "svelte";
  import { beforeNavigate } from "$app/navigation";
  import { api, APIError, errorMessage } from "$lib/api";
  import { workspaceChanged, type Workspace } from "$lib/workspaces";
  let {
    workspace = null,
    onSaved,
    cancel,
    busy = $bindable(false),
  }: {
    workspace?: Workspace | null;
    onSaved: (w: Workspace) => void;
    cancel?: () => void;
    busy?: boolean;
  } = $props();
  let name = $state(untrack(() => workspace?.name ?? "")),
    slug = $state(untrack(() => workspace?.slug ?? "")),
    description = $state(untrack(() => workspace?.description ?? ""));
  let slugEdited = $state(false),
    stale = $state(false),
    error = $state(""),
    saved = $state(false),
    fields = $state<Record<string, string>>({}),
    form = $state<HTMLFormElement>();
  let baseline = $state(untrack(() => workspace));
  const id = $props.id();
  const dirty = $derived(
    name !== (baseline?.name ?? "") ||
      slug !== (baseline?.slug ?? "") ||
      description !== (baseline?.description ?? ""),
  );
  beforeNavigate((n) => {
    if (
      dirty &&
      (busy ||
        n.willUnload ||
        !window.confirm("Discard your unsaved workspace changes?"))
    )
      n.cancel();
  });
  async function reloadDetails() {
    if (!baseline || busy) return;
    if (
      dirty &&
      !window.confirm("Discard edits and reload the latest workspace details?")
    )
      return;
    busy = true;
    try {
      const w = await api<Workspace>(`workspaces/${baseline.id}`);
      baseline = w;
      name = w.name;
      slug = w.slug;
      description = w.description;
      stale = false;
      error = "";
      fields = {};
      onSaved(w);
    } catch (e) {
      error = errorMessage(e);
    } finally {
      busy = false;
    }
  }
  function suggest() {
    if (!baseline && !slugEdited)
      slug = name
        .toLowerCase()
        .normalize("NFKD")
        .replace(/[\u0300-\u036f]/g, "")
        .replace(/[^a-z0-9]+/g, "-")
        .replace(/^-|-$/g, "")
        .slice(0, 63)
        .replace(/-$/, "");
  }
  async function save(e: SubmitEvent) {
    e.preventDefault();
    if (busy) return;
    busy = true;
    error = "";
    fields = {};
    saved = false;
    stale = false;
    try {
      const w = await api<Workspace>(
        baseline ? `workspaces/${baseline.id}/profile` : "workspaces",
        { name, slug, description, version: baseline?.version },
      );
      baseline = w;
      name = w.name;
      slug = w.slug;
      description = w.description;
      saved = true;
      workspaceChanged();
      onSaved(w);
    } catch (e) {
      error = errorMessage(e);
      fields = e instanceof APIError ? e.fields : {};
      stale = e instanceof APIError && e.code === "permissions_changed";
      await tick();
      form
        ?.querySelector<HTMLElement>('[aria-invalid="true"], [role="alert"]')
        ?.focus();
    } finally {
      busy = false;
    }
  }
</script>

<form bind:this={form} onsubmit={save}>
  {#if error}<p class="notice error" role="alert" tabindex="-1">{error}</p>
    {#if stale}<button
        type="button"
        class="secondary"
        disabled={busy}
        onclick={reloadDetails}>Reload details</button
      >{/if}{/if}
  <div class="field">
    <label for={`${id}-name`}>Name</label><input
      id={`${id}-name`}
      bind:value={name}
      oninput={suggest}
      required
      disabled={busy}
      aria-invalid={!!fields.name}
      aria-describedby={`${id}-name-help`}
    />
    <p id={`${id}-name-help`} class:field-error={!!fields.name} class="hint">
      {fields.name || "The name people see. Up to 100 characters."}
    </p>
  </div>
  <div class="field">
    <label for={`${id}-slug`}>Slug</label><input
      id={`${id}-slug`}
      bind:value={slug}
      oninput={() => (slugEdited = true)}
      required
      maxlength="63"
      disabled={busy}
      spellcheck="false"
      autocapitalize="none"
      aria-invalid={!!fields.slug}
      aria-describedby={`${id}-slug-help`}
    />
    <p id={`${id}-slug-help`} class:field-error={!!fields.slug} class="hint">
      {fields.slug || "Used in the URL. Letters, numbers and hyphens."}
    </p>
    <p class="hint url">/workspaces/{slug || "your-workspace"}</p>
    {#if baseline}<p class="hint">
        Previous slugs stay reserved and old links continue to work.
      </p>{/if}
  </div>
  <div class="field">
    <label for={`${id}-description`}
      >Description <span class="hint">(optional)</span></label
    ><textarea
      id={`${id}-description`}
      bind:value={description}
      maxlength="1000"
      rows="3"
      disabled={busy}
      aria-invalid={!!fields.description}
      aria-describedby={`${id}-description-help`}></textarea>
    <p class="hint" id={`${id}-description-help`}>
      {fields.description || "A little context for the people working here."}
    </p>
  </div>
  {#if baseline?.previous_slugs.length}<details>
      <summary>Previous slugs ({baseline.previous_slugs.length})</summary>
      <ul>
        {#each baseline.previous_slugs as previous}<li>{previous}</li>{/each}
      </ul>
    </details>{/if}
  <div class="management-actions">
    {#if cancel}<button
        type="button"
        class="secondary"
        disabled={busy}
        onclick={cancel}>Cancel</button
      >{/if}<button class="primary" disabled={busy || !dirty || stale}
      >{busy
        ? "Saving…"
        : baseline
          ? "Save changes"
          : "Create workspace"}</button
    >{#if saved}<span class="hint" role="status">Saved.</span>{/if}
  </div>
</form>

<style>
  form {
    max-width: 640px;
    margin-top: 24px;
  }
  textarea {
    font: inherit;
    width: 100%;
    padding: 12px;
    border: 1px solid var(--border);
    border-radius: 7px;
    background: var(--surface);
    color: var(--text);
    resize: vertical;
    min-height: 90px;
  }
  .url {
    overflow-wrap: anywhere;
  }
  details {
    font-size: 13px;
    color: var(--muted);
  }
  li {
    padding: 5px 0;
  }
  .management-actions {
    margin-top: 0;
  }
</style>
