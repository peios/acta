<script lang="ts">
  import { tick, untrack } from "svelte";
  import { beforeNavigate } from "$app/navigation";
  import { api, APIError, errorMessage, type Account } from "$lib/api";
  import { useAccount } from "$lib/account-context";
  import { checkPermission } from "$lib/permissions";
  import PreviousUsernames from "./PreviousUsernames.svelte";

  let {
    account,
    onSaved,
    endpoint = "account",
    managed = false,
    owned = false,
  }: {
    account: Account;
    onSaved: (account: Account) => void;
    endpoint?: string;
    managed?: boolean;
    owned?: boolean;
  } = $props();
  const current = useAccount();
  const canManage = $derived(
    owned || !managed || checkPermission(current.account, "site.users.edit"),
  );
  const editingSelf = $derived(
    !owned && (!managed || current.account.id === account.id),
  );
  const canUsername = $derived(
    canManage &&
      (!editingSelf || checkPermission(current.account, "own.username")),
  );
  const canDisplay = $derived(
    canManage &&
      (!editingSelf || checkPermission(current.account, "own.display_name")),
  );
  const segment = (value: Account) => value.username_segment ?? value.username;
  const prefix = $derived(account.owner_id ? `${account.owner_username}/` : "");
  let original = $state(untrack(() => account));
  let username = $state(untrack(() => segment(original)));
  let displayName = $state(untrack(() => original.display_name ?? ""));
  // If access is removed while editing, discard only the now-protected fields
  // so a hidden stale edit cannot prevent saving another permitted field.
  $effect(() => {
    if (!canUsername) username = segment(original);
    if (!canDisplay) displayName = original.display_name ?? "";
  });
  let busy = $state(false);
  let saved = $state(false);
  let error = $state("");
  let errorCode = $state("");
  let fields = $state<Record<string, string>>({});
  let form = $state<HTMLFormElement>();
  let confirmation = $state<HTMLDialogElement>();
  const id = $props.id();
  const normalizedUsername = $derived(
    username.replace(/[A-Z]/g, (letter) => letter.toLowerCase()),
  );
  const dirty = $derived(
    normalizedUsername !== segment(original) ||
      displayName !== (original.display_name ?? ""),
  );
  const renaming = $derived(normalizedUsername !== segment(original));

  beforeNavigate((navigation) => {
    if (!dirty && !busy) return;
    // Leaving the document uses the browser's native unsaved-changes prompt.
    if (
      navigation.willUnload ||
      busy ||
      !window.confirm("Leave Profile and discard your unsaved changes?")
    )
      navigation.cancel();
  });

  function edited(field: string) {
    saved = false;
    delete fields[field];
    if (errorCode === "validation") {
      error = "";
      errorCode = "";
    }
  }
  function reset(value: Account) {
    original = value;
    username = segment(value);
    displayName = value.display_name ?? "";
    onSaved(value);
  }
  function usePreviousUsername(name: string) {
    username = prefix ? name.slice(prefix.length) : name;
    edited("username");
    form?.querySelector<HTMLInputElement>('[name="username"]')?.focus();
  }
  async function showError(cause: unknown) {
    error = errorMessage(cause);
    errorCode = cause instanceof APIError ? cause.code : "";
    fields = cause instanceof APIError ? cause.fields : {};
    await tick();
    (
      form?.querySelector<HTMLElement>('[aria-invalid="true"]') ??
      form?.querySelector<HTMLElement>(".notice.error")
    )?.focus();
  }
  function submit(event: SubmitEvent) {
    event.preventDefault();
    if (busy || !dirty) return;
    saved = false;
    error = "";
    fields = {};
    errorCode = "";
    if (renaming) confirmation?.showModal();
    else void save();
  }
  async function save() {
    if (busy || !dirty) return;
    confirmation?.close();
    busy = true;
    try {
      const value = await api<Account>(`${endpoint}/profile`, {
        username: normalizedUsername,
        display_name: displayName,
        profile_version: original.profile_version,
      });
      reset(value);
      saved = true;
    } catch (cause) {
      await showError(cause);
    } finally {
      busy = false;
    }
  }
  async function reload() {
    if (busy) return;
    busy = true;
    try {
      reset(await api<Account>(endpoint));
      error = "";
      errorCode = "";
      fields = {};
      saved = false;
      await tick();
      form?.querySelector("input")?.focus();
    } catch (cause) {
      await showError(cause);
    } finally {
      busy = false;
    }
  }
</script>

<section class="profile-page" aria-label="Profile details">
  <p class="intro">
    {managed
      ? "Manage this account’s name and identity."
      : "How you appear to other people and agents in Acta."}
  </p>
  <form bind:this={form} onsubmit={submit} aria-busy={busy}>
    {#if error}
      <div class="notice error" role="alert" tabindex="-1">
        <p>{error}</p>
        {#if errorCode === "profile_changed"}
          <p>Loading the latest profile will replace the edits in this form.</p>
          <button
            type="button"
            class="secondary"
            disabled={busy}
            onclick={reload}>Load latest profile</button
          >
        {:else if errorCode === "unauthenticated"}<a href="/login"
            >Sign in again</a
          >{/if}
      </div>
    {/if}
    <div class="field">
      <label for={`${id}-display`}
        >Display name <span class="optional">(optional)</span></label
      >
      {#if canDisplay}<input
          id={`${id}-display`}
          name="display_name"
          autocomplete="nickname"
          bind:value={displayName}
          readonly={busy}
          aria-invalid={!!fields.display_name}
          aria-describedby={`${id}-display-hint${fields.display_name ? ` ${id}-display-error` : ""}`}
          oninput={() => edited("display_name")}
        />
      {:else}<p class="readonly-value">
          {account.display_name || account.username}
        </p>{/if}
      {#if canDisplay}<p id={`${id}-display-hint`} class="hint">
          Leave blank to use your username. Up to 100 characters.
        </p>{/if}
      {#if fields.display_name}<p
          id={`${id}-display-error`}
          class="field-error"
        >
          {fields.display_name}
        </p>{/if}
    </div>
    <div class="field">
      <label for={`${id}-username`}>Username</label>
      {#if canUsername}<div
          class="username-input"
          style:--prefix-width={`${prefix.length + 2}ch`}
        >
          <span aria-hidden="true">@{prefix}</span>
          <input
            id={`${id}-username`}
            name="username"
            autocomplete="username"
            autocapitalize="none"
            spellcheck="false"
            required
            maxlength="32"
            bind:value={username}
            readonly={busy}
            aria-invalid={!!fields.username}
            aria-describedby={`${id}-username-hint${fields.username ? ` ${id}-username-error` : ""}`}
            oninput={() => edited("username")}
          />
        </div>
      {:else}<p class="readonly-value">@{account.username}</p>{/if}
      {#if canUsername}<p id={`${id}-username-hint`} class="hint">
          1–32 letters or numbers, with dots, hyphens or underscores between
          them. Usernames are case-insensitive.
        </p>
        <p class="hint">
          Changing the username reserves the previous name for this account.
          Existing references stay connected.
        </p>{/if}
      {#if fields.username}<p id={`${id}-username-error`} class="field-error">
          {fields.username}
        </p>{/if}
      <PreviousUsernames
        names={original.previous_usernames}
        onChoose={canUsername ? usePreviousUsername : undefined}
        canChoose={(name) => !prefix || name.startsWith(prefix)}
        {busy}
      />
    </div>
    {#if canUsername || canDisplay}<div class="actions">
        <button type="submit" class="primary" disabled={!dirty || busy}
          >{busy ? "Saving…" : "Save changes"}</button
        >
        <span class="save-status" role="status"
          >{#if saved}<svg
              viewBox="0 0 20 20"
              width="16"
              height="16"
              fill="none"
              stroke="currentColor"
              stroke-width="1.7"
              aria-hidden="true"><path d="m4 10 4 4 8-8" /></svg
            >Changes saved{/if}</span
        >
      </div>{/if}
  </form>
</section>

<dialog
  bind:this={confirmation}
  class="rename-dialog"
  aria-labelledby={`${id}-confirm-title`}
  aria-describedby={`${id}-confirm-description`}
>
  <h2 id={`${id}-confirm-title`}>
    Change {managed ? "this" : "your"} username?
  </h2>
  <p class="rename-preview">
    <span>@{original.username}</span><span aria-label="to">→</span><strong
      >@{normalizedUsername}</strong
    >
  </p>
  <p id={`${id}-confirm-description`}>
    The previous username will remain reserved for this account. Existing
    references stay connected.
  </p>
  {#if !owned}<p class="hint">
      The new username will be used for future sign-ins.
    </p>{/if}
  <div class="dialog-actions">
    <button
      type="button"
      class="secondary"
      onclick={() => confirmation?.close()}>Cancel</button
    >
    <button type="button" class="primary" onclick={save}>Change username</button
    >
  </div>
</dialog>

<style>
  .readonly-value {
    margin: 0;
    font-size: 14px;
    overflow-wrap: anywhere;
  }
  .profile-page {
    width: 100%;
    max-width: 520px;
    padding: 8px 0 32px;
  }
  .intro {
    font-size: 14px;
    margin-bottom: 32px;
  }
  form {
    gap: 28px;
  }
  .username-input {
    position: relative;
  }
  .username-input > span {
    position: absolute;
    left: 13px;
    top: 50%;
    transform: translateY(-50%);
    color: var(--muted);
  }
  .username-input input {
    padding-left: calc(20px + var(--prefix-width));
  }
  .actions {
    display: flex;
    align-items: center;
    gap: 18px;
    padding-top: 2px;
    flex-wrap: wrap;
  }
  .primary {
    width: auto;
  }
  .primary:disabled {
    cursor: default;
  }
  .save-status {
    display: inline-flex;
    align-items: center;
    gap: 7px;
    font-size: 13px;
    color: var(--success-text);
  }
  .notice p {
    margin: 0;
  }
  .notice p + p,
  .notice button {
    margin-top: 10px;
  }
  .rename-dialog {
    width: min(460px, calc(100vw - 32px));
    max-height: calc(100svh - 32px);
    padding: 28px;
    color: var(--text);
    background: var(--surface);
    border: 1px solid var(--panel-border);
    border-radius: 14px;
    box-shadow: 0 16px 64px #00000030;
  }
  .rename-dialog::backdrop {
    background: #00000060;
  }
  .rename-dialog[open] {
    animation: appear 140ms ease-out;
  }
  h2 {
    margin: 0 0 20px;
    font-size: 19px;
    font-weight: 650;
    letter-spacing: -0.3px;
  }
  .rename-dialog p {
    font-size: 14px;
    line-height: 1.6;
    color: var(--muted);
  }
  .rename-dialog .hint {
    font-size: 12px;
  }
  .rename-preview {
    display: flex;
    align-items: baseline;
    flex-wrap: wrap;
    gap: 10px;
    overflow-wrap: anywhere;
  }
  .rename-preview strong {
    color: var(--text);
    font-weight: 600;
  }
  .dialog-actions {
    display: flex;
    justify-content: flex-end;
    gap: 10px;
    flex-wrap: wrap;
    margin-top: 26px;
  }
  @keyframes appear {
    from {
      opacity: 0;
      transform: translateY(5px);
    }
    to {
      opacity: 1;
      transform: translateY(0);
    }
  }
  @media (prefers-reduced-motion: reduce) {
    .rename-dialog[open] {
      animation: none;
    }
  }
</style>
