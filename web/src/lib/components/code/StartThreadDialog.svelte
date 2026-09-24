<script lang="ts">
  import { tick } from "svelte";
  import { errorMessage } from "$lib/api";
  import { useCodebases, type CodebaseRoot } from "$lib/codebases.svelte";
  import {
    useCodeThreads,
    codeProviderName,
    type CodeProvider,
  } from "$lib/code-threads.svelte";
  import "$lib/components/management/management.css";
  const codebases = useCodebases(),
    threads = useCodeThreads();
  let { onStarted = () => {} }: { onStarted?: () => void } = $props();
  let dialog: HTMLDialogElement;
  const titleID = $props.id();
  let root = $state(""),
    provider = $state<CodeProvider>("claude"),
    name = $state(""),
    roots = $state<CodebaseRoot[]>([]),
    busy = $state(false),
    error = $state("");
  let host = $state(""),
    base = $state(""),
    requestID = "",
    requestKey = "";
  export async function open() {
    const selected = codebases.state.items.find(
      (b) => b.id === codebases.state.selected,
    );
    if (!selected || !codebases.ready) return;
    host = codebases.state.host;
    base = selected.id;
    name = selected.name;
    roots = selected.roots;
    root = roots[0]?.id ?? "";
    error = "";
    requestID = crypto.randomUUID();
    requestKey = "";
    dialog.showModal();
    await tick();
    dialog.querySelector<HTMLElement>("select, button")?.focus();
  }
  async function submit(event: SubmitEvent) {
    event.preventDefault();
    if (
      busy ||
      host !== codebases.state.host ||
      base !== codebases.state.selected
    )
      return;
    busy = true;
    error = "";
    try {
      const key = `${provider}:${root}`;
      if (requestKey && requestKey !== key) requestID = crypto.randomUUID();
      requestKey = key;
      await threads.start(requestID, root, provider);
      dialog.close();
      onStarted();
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
  <h2 id={titleID}>Start agent thread</h2>
  <p class="management-note">Codebase: <strong>{name}</strong></p>
  <form onsubmit={submit}>
    <label
      >Provider<select bind:value={provider} disabled={busy}>
        <option value="claude">Claude Code</option>
        <option value="codex">Codex</option>
      </select></label
    >
    {#if roots.length > 1}
      <label
        >Working folder<select bind:value={root} disabled={busy}
          >{#each roots as folder (folder.id)}<option value={folder.id}
              >{folder.path}</option
            >{/each}</select
        ></label
      >
    {:else}<p class="folder">{roots[0]?.path}</p>{/if}
    <p class="management-note">
      Starts {codeProviderName(provider)} and displays its output. No prompt is sent.
    </p>
    {#if error}<p class="form-error" role="alert">{error}</p>{/if}
    <div class="management-actions">
      <button
        class="primary"
        type="submit"
        disabled={busy ||
          !codebases.ready ||
          host !== codebases.state.host ||
          base !== codebases.state.selected}
        >{busy ? "Starting…" : "Start thread"}</button
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
  label {
    display: grid;
    gap: 8px;
    font-size: 13px;
  }
  .folder {
    overflow-wrap: anywhere;
    font:
      12px/1.6 ui-monospace,
      monospace;
    color: var(--muted);
  }
  .form-error {
    color: var(--danger);
    font-size: 13px;
  }
</style>
