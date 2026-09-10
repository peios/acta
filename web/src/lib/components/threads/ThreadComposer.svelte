<script lang="ts">
  import ThreadImagePicker from "./ThreadImagePicker.svelte";
  import type { DraftImage } from "$lib/thread-image-input.js";
  import ThreadPermissionControls from "./ThreadPermissionControls.svelte";
  import type {
    PermissionState,
    ApprovalItem,
  } from "$lib/thread-permissions.js";
  import ThreadModelPicker from "./ThreadModelPicker.svelte";
  let {
    permissionState,
    approvals,
    answerApproval,
    editQuestion = () => {},
    changePermission,
    configuration,
    configurationSequence,
    threadId,
    runId,
    available,
    settingsEditable,
    permissionsEditable,
    onsettingsbusy,
    images = [],
    onimages = () => {},
    draft = $bindable(""),
    enabled = false,
    locked = false,
    stopping = false,
    canStop = false,
    stopAvailable = available,
    onstop,
    onsend,
    onchange,
  }: {
    permissionState: PermissionState;
    approvals: ApprovalItem[];
    editQuestion?: (
      item: ApprovalItem,
      id: string,
      value: { selected: string[]; text: string },
    ) => void;
    answerApproval: (
      item: ApprovalItem,
      decision: "approve" | "deny" | "answer",
      values?: Record<string, string[]>,
    ) => void;
    changePermission: (mode: string) => void;
    configuration: Record<string, unknown>;
    configurationSequence: number;
    threadId: string;
    runId: string;
    available: boolean;
    settingsEditable: boolean;
    permissionsEditable: boolean;
    onsettingsbusy: (busy: boolean) => void;
    images?: DraftImage[];
    onimages?: (images: DraftImage[]) => void;
    draft?: string;
    enabled?: boolean;
    locked?: boolean;
    stopping?: boolean;
    canStop?: boolean;
    stopAvailable?: boolean;
    onstop: () => void;
    onsend: () => void;
    onchange: (text: string) => void;
  } = $props();
  let input: HTMLTextAreaElement;
  let imagesBusy = $state(false);
  let imagePicker: ThreadImagePicker;
  function send() {
    if (!enabled || imagesBusy || (!draft.trim() && !images.length)) return;
    onsend();
    input.focus({ preventScroll: true });
  }
  function resize(event: Event) {
    const input = event.currentTarget as HTMLTextAreaElement;
    input.style.height = "auto";
    input.style.height = `${Math.min(input.scrollHeight, 180)}px`;
  }
</script>

<div
  class="composer"
  role="group"
  aria-label="Message composer"
  ondragover={(event) => {
    if (event.dataTransfer?.types.includes("Files")) event.preventDefault();
  }}
  ondrop={(event) => {
    if (event.dataTransfer?.files.length) {
      event.preventDefault();
      void imagePicker?.add(event.dataTransfer.files);
    }
  }}
>
  <ThreadImagePicker
    bind:this={imagePicker}
    {images}
    disabled={locked}
    onchange={onimages}
    onbusy={(busy) => (imagesBusy = busy)}
  />
  <!-- svelte-ignore a11y_autofocus (Opening a conversation intentionally focuses its composer.) -->
  <textarea
    autofocus
    bind:this={input}
    aria-label="Message"
    placeholder="Message…"
    rows="1"
    bind:value={draft}
    readonly={locked}
    oninput={(event) => {
      resize(event);
      onchange(draft);
    }}
    onpaste={(event) => {
      const files = event.clipboardData?.files;
      if (files?.length) {
        event.preventDefault();
        void imagePicker?.add(files);
      }
    }}
    onkeydown={(event) => {
      if (event.key === "Enter" && !event.shiftKey && !event.isComposing) {
        event.preventDefault();
        send();
      }
    }}></textarea>
  <div class="composer-actions">
    <div class="settings-controls">
      <button
        type="button"
        class="attach"
        aria-label="Attach images"
        title="Attach images"
        disabled={locked || imagesBusy}
        onclick={() => imagePicker?.pick()}
        ><svg
          width="18"
          height="18"
          viewBox="0 0 24 24"
          fill="none"
          stroke="currentColor"
          stroke-width="1.7"
          stroke-linecap="round"
          stroke-linejoin="round"
          aria-hidden="true"
          ><path d="m8 12 6-6a3 3 0 0 1 4 4l-8 8a5 5 0 0 1-7-7l8-8" /></svg
        ></button
      >
      <ThreadModelPicker
        {configuration}
        sequence={configurationSequence}
        {threadId}
        {runId}
        {available}
        editable={settingsEditable && !permissionState.modePending}
        onbusy={onsettingsbusy}
      /><ThreadPermissionControls
        mode={String(
          (configuration.permissions as Record<string, unknown>)?.mode ??
            "unknown",
        )}
        {permissionState}
        items={approvals}
        answer={answerApproval}
        {editQuestion}
        change={changePermission}
        editable={permissionsEditable}
      />
    </div>
    {#if canStop || stopping}
      <button
        type="button"
        class="primary"
        disabled={stopping || !stopAvailable}
        onclick={onstop}
        aria-label={stopping ? "Stopping turn" : "Stop turn"}
        title={stopping ? "Stopping…" : "Stop turn"}
      >
        <svg
          viewBox="0 0 24 24"
          width="19"
          height="19"
          fill="currentColor"
          aria-hidden="true"
          ><rect x="6" y="6" width="12" height="12" rx="2" /></svg
        >
      </button>
    {/if}
    {#if !canStop || draft.trim() || images.length}
      <button
        type="button"
        class="primary"
        disabled={!enabled || imagesBusy || (!draft.trim() && !images.length)}
        onclick={send}
        aria-label="Send message"
        title={enabled
          ? "Send message"
          : "Connect the thread to send a message"}
      >
        <svg
          viewBox="0 0 24 24"
          width="19"
          height="19"
          fill="none"
          stroke="currentColor"
          stroke-width="1.8"
          stroke-linecap="round"
          stroke-linejoin="round"
          aria-hidden="true"><path d="M12 19V5m-6 6 6-6 6 6" /></svg
        >
      </button>
    {/if}
  </div>
</div>

<style>
  .attach {
    border: 0;
    background: transparent;
    color: var(--muted);
  }
  .attach:hover {
    background: var(--hover-surface);
    color: var(--text);
  }
  .composer {
    flex-shrink: 0;
    display: grid;
    grid-template-columns: minmax(0, 1fr);
    align-items: end;
    gap: 12px;
    border: 1px solid var(--panel-border);
    border-radius: 16px;
    background: var(--hover-surface);
    padding: 12px;
    box-shadow: 0 4px 18px #0000000a;
    transition: border-color 140ms;
  }
  .composer:focus-within {
    border-color: var(--muted);
  }
  textarea {
    display: block;
    width: 100%;
    min-height: 34px;
    max-height: 180px;
    padding: 6px 2px;
    border: 0;
    border-radius: 0;
    outline: none;
    box-shadow: none;
    background: transparent;
    color: var(--text);
    font: inherit;
    font-size: 14px;
    line-height: 1.6;
    resize: none;
  }
  textarea::placeholder {
    color: var(--muted);
  }
  .settings-controls {
    display: flex;
    align-items: center;
    flex-wrap: wrap;
    gap: 4px;
    min-width: 0;
  }
  .composer-actions {
    display: flex;
    justify-content: space-between;
    align-items: center;
    gap: 12px;
  }
  button {
    display: inline-flex;
    align-items: center;
    justify-content: center;
    width: 34px;
    min-height: 34px;
    padding: 0;
    border-radius: 10px;
  }
</style>
