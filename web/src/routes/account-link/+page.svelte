<script lang="ts">
  import { onMount, tick } from "svelte";
  import { api, APIError, errorMessage } from "$lib/api";
  import AuthShell from "$lib/components/AuthShell.svelte";
  import PasswordField from "$lib/components/PasswordField.svelte";
  type LinkState = {
    purpose: "invite" | "reset";
    can_change_display_name: boolean;
    username: string;
    display_name: string | null;
    profile_version: number;
    disable_mfa: boolean;
    remove_passkeys: boolean;
  };
  let token = "",
    info = $state<LinkState | null>(null),
    password = $state(""),
    displayName = $state(""),
    error = $state(""),
    busy = $state(false),
    done = $state(false),
    fields = $state<Record<string, string>>({}),
    form = $state<HTMLFormElement>();
  async function load() {
    error = "";
    try {
      info = await api<LinkState>("account-link/open", { token });
      displayName = info.display_name ?? "";
    } catch (e) {
      error = errorMessage(e);
    }
  }
  onMount(() => {
    const fragment = new URLSearchParams(location.hash.slice(1));
    token = fragment.get("token") ?? "";
    if (token) {
      try {
        sessionStorage.setItem("acta.account-link", token);
      } catch {}
      history.replaceState(history.state, "", location.pathname);
    } else {
      try {
        token = sessionStorage.getItem("acta.account-link") ?? "";
      } catch {}
    }
    void load();
  });
  async function submit(e: SubmitEvent) {
    e.preventDefault();
    if (busy) return;
    busy = true;
    error = "";
    fields = {};
    try {
      await api("account-link/redeem", {
        token,
        new_password: password,
        display_name: displayName,
        profile_version: info?.profile_version,
      });
      password = "";
      done = true;
      try {
        sessionStorage.removeItem("acta.account-link");
      } catch {}
    } catch (e) {
      error = errorMessage(e);
      fields = e instanceof APIError ? e.fields : {};
      if (e instanceof APIError && e.code === "profile_changed") {
        info = null;
      }
    } finally {
      busy = false;
      await tick();
      form
        ?.querySelector<HTMLElement>('[aria-invalid="true"], [role="alert"]')
        ?.focus();
    }
  }
</script>

<svelte:head
  ><title
    >{info?.purpose === "invite"
      ? "Activate your account"
      : "Set your password"} · Acta</title
  ><meta name="robots" content="noindex" /><meta
    name="referrer"
    content="no-referrer"
  /></svelte:head
>
<AuthShell
  title={done
    ? "You’re ready to sign in"
    : info?.purpose === "invite"
      ? "Welcome to Acta"
      : "Set your password"}
  description={done
    ? "Your password has been saved."
    : info
      ? `For @${info.username}`
      : "Open your invitation or recovery link."}
>
  {#if done}<a class="primary sign-in" href="/login">Sign in</a
    >{:else if info}<form bind:this={form} onsubmit={submit} aria-busy={busy}>
      {#if error}<p class="notice error" role="alert" tabindex="-1">
          {error}
        </p>{/if}{#if info.purpose === "invite" && info.can_change_display_name}<div
          class="field"
        >
          <label for="display-name"
            >Display name <span class="hint">(optional)</span></label
          ><input
            id="display-name"
            autocomplete="nickname"
            bind:value={displayName}
            disabled={busy}
            aria-invalid={!!fields.display_name}
          />
          <p class="hint">Leave blank to use your username.</p>
        </div>{/if}<PasswordField
        bind:value={password}
        creating
        label="New password"
        disabled={busy}
        error={fields.new_password}
        hint="Use at least 15 characters. A few unrelated words work well."
      />{#if info.disable_mfa || info.remove_passkeys}<p class="hint">
          Your administrator has also authorised {info.disable_mfa
            ? "disabling MFA"
            : ""}{info.disable_mfa && info.remove_passkeys
            ? " and "
            : ""}{info.remove_passkeys ? "removing your passkeys" : ""} when you finish.
        </p>{/if}<button class="primary" disabled={busy}
        >{busy
          ? "Saving…"
          : info.purpose === "invite"
            ? "Activate account"
            : "Save password"}</button
      >
    </form>{:else if error}<p class="notice error" role="alert">{error}</p>
    <button class="secondary" onclick={load}>Try again</button>{:else}<p
      role="status"
      class="hint"
    >
      Checking your link…
    </p>{/if}
</AuthShell>

<style>
  .sign-in {
    display: block;
    text-align: center;
    text-decoration: none;
  }
</style>
