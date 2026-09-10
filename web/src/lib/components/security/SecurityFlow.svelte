<script lang="ts">
  import { onDestroy, onMount, tick, untrack } from "svelte";
  import { APIError, errorMessage } from "$lib/api";
  import { advanceFlow, type Flow } from "$lib/security";
  import { browserPasskey } from "$lib/passkeys";
  import PasswordField from "../PasswordField.svelte";
  import PasswordFields from "./PasswordFields.svelte";
  let {
    initial,
    onDone,
    busy = $bindable(false),
    browserWaiting = $bindable(false),
  }: {
    initial: Flow;
    onDone: (flow: Flow) => void;
    busy?: boolean;
    browserWaiting?: boolean;
  } = $props();
  let flow = $state(untrack(() => initial));
  let password = $state(""),
    newPassword = $state(""),
    code = $state(""),
    name = $state(""),
    error = $state(""),
    copied = $state(false),
    acknowledged = $state(false),
    recovery = $state(false);
  let fields = $state<Record<string, string>>({});
  let form = $state<HTMLFormElement>();
  let controller: AbortController | undefined;
  const id = $props.id();
  let alive = true;
  function focusStep() {
    if (!alive) return;
    (
      form?.querySelector<HTMLElement>('[aria-invalid="true"]') ??
      form?.querySelector<HTMLElement>('[role="alert"]') ??
      form?.querySelector<HTMLElement>("input, button")
    )?.focus();
  }
  onMount(focusStep);
  onDestroy(() => {
    alive = false;
    controller?.abort();
  });
  async function accept(next: Flow) {
    if (!alive) return;
    flow = next;
    password = "";
    newPassword = "";
    code = "";
    fields = {};
    error = "";
    if (next.step === "done") {
      onDone(next);
      return;
    }
  }
  async function failure(cause: unknown) {
    if (!alive) return;
    error =
      cause instanceof DOMException &&
      (cause.name === "NotAllowedError" || cause.name === "AbortError")
        ? "The passkey request was cancelled or timed out. You can try again."
        : errorMessage(cause);
    fields = cause instanceof APIError ? cause.fields : {};
  }
  async function send(action: string, values: Record<string, unknown> = {}) {
    if (busy) return;
    busy = true;
    error = "";
    try {
      await accept(await advanceFlow(flow, action, values));
    } catch (e) {
      await failure(e);
    } finally {
      if (alive) {
        busy = false;
        await tick();
        focusStep();
      }
    }
  }
  async function passkey(creating: boolean) {
    if (busy) return;
    busy = true;
    error = "";
    try {
      let next = flow;
      if (creating)
        next = await advanceFlow(flow, "registration_begin", { name });
      else if (flow.purpose !== "login" || flow.step !== "passkey_verify")
        next = await advanceFlow(flow, "passkey_begin");
      if (!alive) return;
      flow = next;
      controller = new AbortController();
      browserWaiting = true;
      const credential = await browserPasskey(
        next,
        creating,
        controller.signal,
      );
      if (!alive) return;
      browserWaiting = false;
      await accept(
        await advanceFlow(
          next,
          creating ? "registration_finish" : "passkey_finish",
          { credential },
        ),
      );
    } catch (e) {
      await failure(e);
    } finally {
      if (alive) browserWaiting = false;
      if (alive) {
        busy = false;
        await tick();
        focusStep();
      }
    }
  }
  function submit(event: SubmitEvent) {
    event.preventDefault();
    if (flow.step === "verify" || flow.step === "password")
      void send("password", { password, new_password: newPassword });
    else if (flow.step === "code") void send("code", { code, recovery });
    else if (flow.step === "totp_setup") void send("setup_verify", { code });
    else if (flow.step === "passkey_name" || flow.step === "passkey_create")
      void passkey(true);
  }
  async function copyCodes() {
    try {
      await navigator.clipboard.writeText(flow.codes?.join("\n") ?? "");
      copied = true;
    } catch {
      error =
        "Copy was unavailable. Download the codes or select them manually.";
    }
  }
  function downloadCodes() {
    const url = URL.createObjectURL(
      new Blob(
        [
          "Acta recovery codes — keep these private. Each code can be used once.\n\n" +
            flow.codes?.join("\n"),
        ],
        { type: "text/plain" },
      ),
    );
    const link = document.createElement("a");
    link.href = url;
    link.download = "acta-recovery-codes.txt";
    link.click();
    URL.revokeObjectURL(url);
  }
</script>

<form bind:this={form} onsubmit={submit} aria-busy={busy}>
  {#if error}<p class="notice error" role="alert" tabindex="-1">{error}</p>{/if}
  {#if flow.step === "verify" || flow.step === "password"}
    {#if flow.step === "password"}<PasswordFields
        bind:password
        bind:newPassword
        {busy}
        {fields}
      />
    {:else}<p class="hint">
        Confirm it’s you before changing your security settings.
      </p>
      <PasswordField
        bind:value={password}
        disabled={busy}
        error={fields.password}
      />{/if}
    <button class="primary" disabled={busy}
      >{busy
        ? "Verifying…"
        : flow.step === "password"
          ? "Change password"
          : "Continue"}</button
    >
    {#if flow.step === "verify"}<button
        class="secondary"
        type="button"
        disabled={busy}
        onclick={() => passkey(false)}>Use a passkey</button
      >{/if}
  {:else if flow.step === "code"}
    <p class="hint">
      {recovery
        ? "Enter one of your saved recovery codes. It can only be used once."
        : "Enter the current code from your authenticator app."}
    </p>
    <div class="field">
      <label for={`${id}-code`}
        >{recovery ? "Recovery code" : "Authenticator code"}</label
      ><input
        id={`${id}-code`}
        bind:value={code}
        autocomplete="one-time-code"
        inputmode={recovery ? "text" : "numeric"}
        required
        disabled={busy}
        aria-invalid={!!fields.code}
      />
    </div>
    <button class="primary" disabled={busy}
      >{busy ? "Verifying…" : "Verify"}</button
    >
    <button
      type="button"
      class="text-button"
      disabled={busy}
      onclick={() => {
        recovery = !recovery;
        code = "";
        error = "";
      }}
      >{recovery ? "Use an authenticator code" : "Use a recovery code"}</button
    >
  {:else if flow.step === "passkey_verify"}
    <p class="hint">
      Use your device, password manager or security key to verify your passkey.
    </p>
    <button
      class="primary"
      type="button"
      disabled={busy}
      onclick={() => passkey(false)}
      >{browserWaiting
        ? "Waiting for your browser…"
        : "Continue with passkey"}</button
    >
  {:else if flow.step === "passkey_name" || flow.step === "passkey_create"}
    <p class="hint">
      Your browser will help you save a passkey to your device, password manager
      or security key.
    </p>
    <div class="field">
      <label for={`${id}-name`}>Passkey name</label><input
        id={`${id}-name`}
        bind:value={name}
        placeholder="For example, my laptop"
        required
        disabled={busy}
        aria-invalid={!!fields.name}
      />
    </div>
    <button class="primary" disabled={busy}
      >{browserWaiting ? "Waiting for your browser…" : "Create passkey"}</button
    >
  {:else if flow.step === "totp_setup"}
    <p class="hint">
      Scan this QR code with your authenticator app, then enter a code to check
      it works.
    </p>
    <img
      class="qr"
      src={flow.qr}
      alt="QR code for your authenticator app"
      width="240"
      height="240"
    />
    <details>
      <summary>Enter a setup key instead</summary>
      <p class="secret">{flow.secret}</p>
    </details>
    <div class="field">
      <label for={`${id}-setup-code`}>Authenticator code</label><input
        id={`${id}-setup-code`}
        bind:value={code}
        inputmode="numeric"
        autocomplete="one-time-code"
        required
        disabled={busy}
        aria-invalid={!!fields.code}
      />
    </div>
    <button class="primary" disabled={busy}
      >{busy ? "Verifying…" : "Verify authenticator"}</button
    >
  {:else if flow.step === "recovery_codes"}
    <p class="hint">
      Save these codes somewhere private. Each replaces one authenticator code
      if you lose access to your app. You’ll still need your password or
      passkey.
    </p>
    <ul class="codes">
      {#each flow.codes ?? [] as item}<li>{item}</li>{/each}
    </ul>
    <div class="code-actions">
      <button class="secondary" type="button" onclick={copyCodes}
        >{copied ? "Copied" : "Copy codes"}</button
      ><button class="secondary" type="button" onclick={downloadCodes}
        >Download</button
      >
    </div>
    <label class="acknowledge"
      ><input type="checkbox" bind:checked={acknowledged} disabled={busy} />I’ve
      saved these recovery codes</label
    >
    <p class="hint">
      {flow.purpose === "recovery"
        ? "Your previous codes stop working when you finish."
        : "Your new authenticator takes effect when you finish."}
    </p>
    <button
      class="primary"
      type="button"
      disabled={!acknowledged || busy}
      onclick={() => send("acknowledge")}
      >{busy ? "Saving…" : "Finish setup"}</button
    >
  {:else if flow.step === "confirm"}
    <p class="hint">
      {flow.purpose === "passkey_remove"
        ? "This passkey will no longer be able to sign you in."
        : flow.purpose === "mfa_disable"
          ? "Your authenticator and recovery codes will stop working."
          : "Apply the new authenticator-code requirement to passkey sign-ins."} Other
      sessions will be signed out.
    </p>
    <button
      class="primary"
      type="button"
      disabled={busy}
      onclick={() => send("confirm")}
      >{busy ? "Saving…" : "Confirm change"}</button
    >
  {/if}
</form>

<style>
  form {
    gap: 20px;
  }
  .qr {
    display: block;
    max-width: 100%;
    height: auto;
    margin: 0 auto;
    background: white;
    border-radius: 8px;
    padding: 8px;
  }
  .secret {
    font-family: monospace;
    overflow-wrap: anywhere;
    font-size: 14px;
    line-height: 1.6;
    user-select: all;
  }
  .codes {
    list-style: none;
    margin: 0;
    padding: 16px;
    border: 1px solid var(--panel-border);
    border-radius: 8px;
    display: grid;
    gap: 10px;
    font-family: monospace;
    font-size: 14px;
    text-align: center;
  }
  .code-actions {
    display: flex;
    gap: 10px;
    flex-wrap: wrap;
  }
  .acknowledge {
    display: flex;
    align-items: center;
    gap: 10px;
    font-size: 13px;
    line-height: 1.5;
  }
  .acknowledge input {
    width: 18px;
    height: 18px;
    flex-shrink: 0;
  }
  .text-button {
    border: 0;
    background: transparent;
    color: var(--accent);
    min-height: 36px;
    font-size: 13px;
  }
  summary {
    font-size: 12px;
    color: var(--muted);
    cursor: pointer;
  }
</style>
