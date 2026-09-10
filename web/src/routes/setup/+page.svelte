<script lang="ts">
  import { onMount, tick } from "svelte";
  import { goto } from "$app/navigation";
  import { api, APIError, errorMessage, type SetupState } from "$lib/api";
  import AuthShell from "$lib/components/AuthShell.svelte";
  import PasswordField from "$lib/components/PasswordField.svelte";

  let setup = $state<SetupState | null>(null);
  let code = $state("");
  let username = $state("");
  let displayName = $state("");
  let password = $state("");
  let busy = $state(false);
  let error = $state("");
  let fields = $state<Record<string, string>>({});
  let form = $state<HTMLFormElement>();
  let errorBox = $state<HTMLParagraphElement>();

  async function load() {
    error = "";
    try {
      setup = await api<SetupState>("setup");
      if (setup.complete) await goto("/login", { replaceState: true });
    } catch (e) {
      error = errorMessage(e);
    }
  }
  onMount(load);

  async function submit(event: SubmitEvent) {
    event.preventDefault();
    if (busy || !setup) return;
    busy = true;
    error = "";
    fields = {};
    try {
      if (!setup.unlocked) {
        await api("setup/unlock", { code });
        code = "";
        setup.unlocked = true;
        await tick();
        form?.querySelector<HTMLInputElement>("#username")?.focus();
      } else {
        await api("setup/complete", {
          username,
          password,
          display_name: displayName,
        });
        password = "";
        await goto("/login?created=1", { replaceState: true });
      }
    } catch (e) {
      if (e instanceof APIError) {
        if (e.code === "setup_complete") {
          await goto("/login", { replaceState: true });
          return;
        }
        if (e.code === "setup_expired") setup.unlocked = false;
        fields = e.fields;
      }
      error = errorMessage(e);
      await tick();
      const invalid = form?.querySelector<HTMLInputElement>(
        '[aria-invalid="true"]',
      );
      if (invalid) invalid.focus();
      else errorBox?.focus();
    } finally {
      busy = false;
    }
  }
</script>

<svelte:head
  ><title>Set up Acta</title><meta
    name="robots"
    content="noindex"
  /></svelte:head
>
<AuthShell
  title={setup?.unlocked ? "Your first account" : "Welcome to Acta"}
  description={setup?.unlocked
    ? "Create your administrator account. You’ll use it to manage this installation."
    : "Let’s make this space yours. First, enter the setup code shown in your server terminal."}
  eyebrow={setup?.unlocked
    ? "Step 2 of 2 · Administrator"
    : "Step 1 of 2 · Set up your site"}
>
  {#if !setup}
    {#if error}<p class="notice error" role="alert">{error}</p>
      <button class="secondary" onclick={load}>Try again</button>
    {:else}<p class="loading" role="status">Connecting to Acta…</p>{/if}
  {:else if !setup.complete}
    <form onsubmit={submit} bind:this={form} aria-busy={busy}>
      {#if error}<p
          class="notice error"
          role="alert"
          tabindex="-1"
          bind:this={errorBox}
        >
          {error}
        </p>{/if}
      {#if !setup.unlocked}
        <div class="field">
          <label for="code">Setup code</label>
          <input
            id="code"
            name="code"
            type="text"
            bind:value={code}
            required
            autocomplete="off"
            autocapitalize="characters"
            spellcheck="false"
            aria-invalid={fields.code ? true : undefined}
            aria-describedby="code-hint"
          />
          <p class="hint" id="code-hint">
            Enter the 8-character code from your server terminal. Uppercase or
            lowercase is fine. It’s valid for one hour after Acta starts.
          </p>
        </div>
        <button class="primary" type="submit" disabled={busy}
          >{busy ? "Checking code…" : "Continue"}</button
        >
      {:else}
        <div class="field">
          <label for="username">Username</label>
          <input
            id="username"
            name="username"
            type="text"
            bind:value={username}
            required
            maxlength="32"
            autocomplete="username"
            autocapitalize="none"
            spellcheck="false"
            aria-invalid={fields.username ? true : undefined}
            aria-describedby={fields.username
              ? "username-hint username-error"
              : "username-hint"}
          />
          <p class="hint" id="username-hint">
            1–32 letters or numbers, with dots, hyphens or underscores in
            between. Uppercase becomes lowercase.
          </p>
          {#if fields.username}<p class="field-error" id="username-error">
              {fields.username}
            </p>{/if}
        </div>
        <div class="field">
          <label for="display-name"
            >Display name <span class="optional">(optional)</span></label
          >
          <input
            id="display-name"
            name="display_name"
            type="text"
            bind:value={displayName}
            autocomplete="name"
            aria-invalid={fields.display_name ? true : undefined}
            aria-describedby={fields.display_name
              ? "display-name-hint display-name-error"
              : "display-name-hint"}
          />
          <p class="hint" id="display-name-hint">
            What people will see. Up to 100 characters; it doesn’t need to be
            unique.
          </p>
          {#if fields.display_name}<p
              class="field-error"
              id="display-name-error"
            >
              {fields.display_name}
            </p>{/if}
        </div>
        <PasswordField
          bind:value={password}
          creating
          error={fields.password}
          hint={`${setup.password_min}–${setup.password_max} characters. A few unrelated words work well. Spaces are welcome.`}
        />
        <button class="primary" type="submit" disabled={busy}
          >{busy ? "Creating account…" : "Create administrator account"}</button
        >
      {/if}
    </form>
  {/if}
</AuthShell>
