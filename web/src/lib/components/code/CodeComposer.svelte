<script lang="ts">
  import { codeProviderName } from "$lib/code-threads.svelte";
  import { errorMessage } from "$lib/api";
  let {
    provider,
    disabled,
    busy,
    onSend,
  }: {
    provider: string;
    disabled: boolean;
    busy: boolean;
    onSend: (id: string, text: string) => Promise<void>;
  } = $props();
  let draft = $state("");
  let submitting = $state(false),
    error = $state("");
  let requestID = "",
    requestText = "";
  const tooLong = $derived(new TextEncoder().encode(draft).length > 4096);
  async function send() {
    if (disabled || busy || submitting || !draft.trim() || tooLong) return;
    if (!requestID || requestText !== draft) {
      requestID = crypto.randomUUID();
      requestText = draft;
    }
    submitting = true;
    error = "";
    try {
      await onSend(requestID, requestText);
      draft = "";
      requestID = "";
      requestText = "";
    } catch (e) {
      error = errorMessage(e);
    } finally {
      submitting = false;
    }
  }
</script>

<div class="composer-wrap">
  <div class="composer" role="group" aria-label="Message composer">
    <textarea
      aria-label="Message draft"
      placeholder="Ask, plan, or describe a change…"
      bind:value={draft}
      disabled={submitting}
      onkeydown={(event) => {
        if (event.key === "Enter" && !event.shiftKey && !event.isComposing) {
          event.preventDefault();
          void send();
        }
      }}
      rows="3"
      maxlength="4096"></textarea>
    <div class="controls">
      <div class="left-controls">
        <button
          type="button"
          class="icon-button"
          aria-label="Add attachment (preview)"
          aria-disabled="true"
          title="Add attachment · coming soon"
        >
          <svg viewBox="0 0 24 24" aria-hidden="true"
            ><path d="M12 4v16M4 12h16" /></svg
          >
        </button>
        <button
          type="button"
          class="approval"
          aria-label="Approval mode: Ask before edits (preview)"
          aria-disabled="true"
          title="Ask before edits · preview only"
        >
          <svg viewBox="0 0 24 24" aria-hidden="true"
            ><path d="m12 3 8 4v6c0 4-4 7-8 9-4-2-8-5-8-9V7z" /><path
              d="m10 9 3 3-3 3"
            /></svg
          >
          <span>Ask before edits</span>
        </button>
      </div>
      <div class="right-controls">
        <button
          type="button"
          class="model"
          aria-label={`Model and effort: ${codeProviderName(provider)}, Default (preview)`}
          aria-disabled="true"
          title="Model and effort · preview only"
        >
          <span>{codeProviderName(provider)}</span><span class="effort"
            >Default</span
          >
          <svg class="chevron" viewBox="0 0 24 24" aria-hidden="true"
            ><path d="m6 9 6 6 6-6" /></svg
          >
        </button>
        <button
          type="button"
          class="icon-button mic"
          aria-label="Dictate (preview)"
          aria-disabled="true"
          title="Dictation · coming soon"
        >
          <svg viewBox="0 0 24 24" aria-hidden="true"
            ><rect x="9" y="3" width="6" height="12" rx="3" /><path
              d="M5 11v1a7 7 0 0 0 14 0v-1M12 19v3"
            /></svg
          >
        </button>
        <button
          type="button"
          class="send"
          aria-label="Send message"
          disabled={disabled || busy || submitting || !draft.trim() || tooLong}
          onclick={() => void send()}
          title="Send message"
        >
          <svg viewBox="0 0 24 24" aria-hidden="true"
            ><path d="M12 20V4m-7 7 7-7 7 7" /></svg
          >
        </button>
      </div>
    </div>
  </div>
  {#if error}<p class="send-error" role="alert">{error}</p>{/if}
  <p class="preview-note">
    {tooLong
      ? "Message exceeds 4 KiB."
      : busy
        ? "Turn running · waiting for provider completion"
        : "Enter to send · Shift+Enter for a new line"}
  </p>
</div>

<style>
  .send:disabled {
    opacity: 0.4;
    cursor: default;
  }
  .send-error {
    color: var(--danger);
    font-size: 12px;
  }
  .composer-wrap {
    flex-shrink: 0;
    padding: 12px 20px 8px;
    container-type: inline-size;
  }
  .composer {
    background: var(--hover-surface);
    border: 1px solid var(--panel-border);
    border-radius: 22px;
    padding: 15px 13px 11px;
  }
  .composer:focus-within {
    border-color: color-mix(in srgb, var(--muted) 45%, var(--panel-border));
  }
  textarea {
    display: block;
    width: 100%;
    min-height: 65px;
    max-height: 180px;
    height: auto;
    margin: 0 0 9px;
    padding: 0 3px;
    border: 0;
    border-radius: 0;
    background: transparent;
    color: var(--text);
    font-family: inherit;
    font-size: 14px;
    line-height: 1.5;
    resize: none;
    box-shadow: none;
    outline: none;
  }
  textarea::placeholder {
    color: var(--muted);
  }
  .controls,
  .left-controls,
  .right-controls {
    display: flex;
    align-items: center;
    gap: 8px;
    min-width: 0;
  }
  .controls {
    justify-content: space-between;
    gap: 12px;
  }
  .right-controls {
    margin-left: auto;
    gap: 7px;
  }
  button {
    display: inline-flex;
    align-items: center;
    justify-content: center;
    gap: 6px;
    min-height: 32px;
    border: 0;
    border-radius: 8px;
    padding: 3px;
    background: transparent;
    color: var(--text);
    font-family: inherit;
    font-size: 12px;
    font-weight: 400;
    white-space: nowrap;
    cursor: default;
  }
  button:focus-visible {
    outline: 2px solid var(--accent);
    outline-offset: 2px;
  }
  svg {
    width: 18px;
    height: 18px;
    fill: none;
    stroke: currentColor;
    stroke-width: 1.7;
    stroke-linecap: round;
    stroke-linejoin: round;
    flex-shrink: 0;
  }
  .icon-button {
    width: 28px;
    flex-shrink: 0;
  }
  .approval,
  .effort,
  .chevron {
    color: var(--muted);
  }
  .approval svg {
    width: 16px;
    height: 16px;
  }
  .chevron {
    width: 14px;
    height: 14px;
  }
  .send {
    width: 34px;
    height: 34px;
    flex: 0 0 34px;
    border-radius: 50%;
    background: var(--text);
    color: var(--surface);
  }
  .send svg {
    width: 21px;
    height: 21px;
  }
  .preview-note {
    margin: 7px 0 0;
    text-align: center;
    color: var(--muted);
    font-size: 10px;
  }
  @container (max-width: 440px) {
    .approval span {
      display: none;
    }
    .controls {
      gap: 6px;
    }
    .right-controls {
      gap: 5px;
    }
  }
  @container (max-width: 320px) {
    .effort {
      display: none;
    }
    .left-controls {
      gap: 2px;
    }
  }
</style>
