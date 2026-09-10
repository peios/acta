<script lang="ts">
  import { untrack } from "svelte";
  import { api, APIError, errorMessage } from "$lib/api";
  import type { Group } from "$lib/groups";
  let {
    group,
    onSaved,
    onCancel,
    busy = $bindable(false),
  }: {
    group?: Group;
    onSaved: (g: Group) => void;
    onCancel?: () => void;
    busy?: boolean;
  } = $props();
  let name = $state(untrack(() => group?.name ?? "")),
    description = $state(untrack(() => group?.description ?? "")),
    error = $state(""),
    fields = $state<Record<string, string>>({}),
    saved = $state(false);
  const id = $props.id();
  const dirty = $derived(
    name !== (group?.name ?? "") || description !== (group?.description ?? ""),
  );
  async function save(e: SubmitEvent) {
    e.preventDefault();
    if (busy) return;
    busy = true;
    error = "";
    fields = {};
    saved = false;
    try {
      const value = await api<Group>(
        group ? `groups/${group.id}/profile` : "groups",
        {
          name,
          description,
          ...(group ? { permissions_version: group.permissions_version } : {}),
        },
      );
      name = value.name;
      description = value.description;
      onSaved(value);
      saved = true;
    } catch (e) {
      error = errorMessage(e);
      fields = e instanceof APIError ? e.fields : {};
    } finally {
      busy = false;
    }
  }
</script>

<form onsubmit={save} aria-busy={busy}>
  {#if error}<p class="notice error" role="alert">{error}</p>{/if}
  <div class="field">
    <label for={`${id}-name`}>Group name</label><input
      id={`${id}-name`}
      bind:value={name}
      required
      disabled={busy}
      aria-invalid={!!fields.name}
    />
    <p class="hint">A distinct name, up to 100 characters.</p>
  </div>
  <div class="field">
    <label for={`${id}-description`}
      >Description <span class="hint">(optional)</span></label
    ><textarea
      id={`${id}-description`}
      rows="3"
      maxlength="500"
      bind:value={description}
      disabled={busy}
      aria-invalid={!!fields.description}></textarea>
  </div>
  <div class="actions">
    {#if onCancel}<button
        class="secondary"
        type="button"
        disabled={busy}
        onclick={onCancel}>Cancel</button
      >{/if}<button class="primary" disabled={busy || (!!group && !dirty)}
      >{busy ? "Saving…" : group ? "Save changes" : "Create group"}</button
    >{#if saved}<span class="hint" role="status">Changes saved</span>{/if}
  </div>
</form>

<style>
  form {
    max-width: 520px;
    gap: 24px;
  }
  textarea {
    font: inherit;
    font-size: 14px;
    color: var(--text);
    background: var(--surface);
    border: 1px solid var(--border);
    border-radius: 8px;
    padding: 12px;
    resize: vertical;
    width: 100%;
  }
  .actions {
    display: flex;
    align-items: center;
    gap: 12px;
    flex-wrap: wrap;
  }
  .primary {
    width: auto;
  }
</style>
