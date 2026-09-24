<script lang="ts">
  import { tick } from "svelte";
  import { errorMessage } from "$lib/api";
  import { useCodebases } from "$lib/codebases.svelte";
  import { useCodeHosts } from "$lib/code-hosts.svelte";
  import "$lib/components/management/management.css";
  const codebases = useCodebases(),
    hosts = useCodeHosts();
  let { onSaved = () => {} }: { onSaved?: () => void } = $props();
  const titleID = $props.id();
  let dialog: HTMLDialogElement;
  let name = $state(""),
    paths = $state(""),
    error = $state(""),
    busy = $state(false);
  let host = $state(""),
    hostName = $state(""),
    requestID = "";
  export async function open() {
    host = hosts.selected;
    hostName = hosts.hosts.find((item) => item.id === host)?.name ?? "";
    name = "";
    paths = "";
    error = "";
    requestID = crypto.randomUUID();
    dialog.showModal();
    await tick();
    dialog.querySelector("input")?.focus();
  }
  async function submit(event: SubmitEvent) {
    event.preventDefault();
    if (busy) return;
    busy = true;
    error = "";
    try {
      await codebases.add(host, {
        id: requestID,
        name,
        paths: paths
          .split(/\r?\n/)
          .map((path) => path.trim())
          .filter(Boolean),
      });
      dialog.close();
      onSaved();
    } catch (e) {
      error = errorMessage(e);
    } finally {
      busy = false;
    }
  }
</script>

<dialog
  class="management-dialog"
  bind:this={dialog}
  aria-labelledby={titleID}
  oncancel={(event) => {
    if (busy) event.preventDefault();
  }}
>
  <h2 id={titleID}>Add codebase</h2>
  <p class="management-note">
    Use existing folders on <strong>{hostName}</strong>.
  </p>
  <form onsubmit={submit}>
    <label class="field"
      >Name<input
        bind:value={name}
        required
        maxlength="100"
        placeholder="peios"
        disabled={busy}
      /></label
    >
    <label class="field"
      >Folder paths<textarea
        bind:value={paths}
        required
        rows="4"
        placeholder="/home/jack/projects/peios"
        disabled={busy}></textarea></label
    >
    <p class="management-note">
      One absolute folder path per line. Files stay where they are.
    </p>
    {#if error}<p class="form-error" role="alert">{error}</p>{/if}
    <div class="management-actions">
      <button
        class="primary"
        type="submit"
        disabled={busy || !codebases.ready || host !== hosts.selected}
        >{busy ? "Adding…" : "Add codebase"}</button
      >
      <button
        class="secondary"
        type="button"
        disabled={busy}
        onclick={() => dialog.close()}>Cancel</button
      >
    </div>
  </form>
</dialog>

<style>
  .field {
    display: grid;
    gap: 8px;
    margin-top: 18px;
    font-size: 13px;
  }
  textarea {
    min-height: 105px;
    resize: vertical;
    font:
      12px ui-monospace,
      monospace;
  }
  .form-error {
    color: var(--danger);
    font-size: 13px;
  }
</style>
